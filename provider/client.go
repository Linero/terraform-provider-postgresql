package provider

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const advisoryLockKeyName = "terraform-provider-postgresql/catalog-lock"

var advisoryLockKey = fnv1a64(advisoryLockKeyName)

func fnv1a64(s string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return int64(h.Sum64())
}

type connConfig struct {
	Host           string
	Port           int64
	Username       string
	Password       string
	Sslmode        string
	ConnectTimeout int64
}

func (cfg connConfig) dsn(database string) string {
	pair := func(key, value string) string {
		v := strings.ReplaceAll(value, `\`, `\\`)
		v = strings.ReplaceAll(v, `'`, `\'`)
		return fmt.Sprintf("%s='%s'", key, v)
	}
	kv := []string{
		pair("host", cfg.Host),
		pair("port", fmt.Sprintf("%d", cfg.Port)),
		pair("user", cfg.Username),
		pair("password", cfg.Password),
		pair("dbname", database),
		pair("sslmode", cfg.Sslmode),
		pair("connect_timeout", fmt.Sprintf("%d", cfg.ConnectTimeout)),
	}
	return strings.Join(kv, " ")
}

type Client struct {
	cfg     connConfig
	adminDB string

	mu sync.Mutex // serializes ALL catalog-mutating DDL across every database

	poolsMu sync.Mutex
	pools   map[string]*pgxpool.Pool
}

func NewClient(ctx context.Context, cfg connConfig, adminDatabase string) (*Client, error) {
	c := &Client{
		cfg:     cfg,
		adminDB: adminDatabase,
		pools:   map[string]*pgxpool.Pool{},
	}
	if _, err := c.poolFor(ctx, adminDatabase); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Client) Close() {
	c.poolsMu.Lock()
	defer c.poolsMu.Unlock()
	for _, p := range c.pools {
		p.Close()
	}
}

func (c *Client) ClosePool(database string) {
	c.poolsMu.Lock()
	defer c.poolsMu.Unlock()
	if p, ok := c.pools[database]; ok {
		p.Close()
		delete(c.pools, database)
	}
}

func (c *Client) poolFor(ctx context.Context, database string) (*pgxpool.Pool, error) {
	c.poolsMu.Lock()
	defer c.poolsMu.Unlock()

	if p, ok := c.pools[database]; ok {
		return p, nil
	}

	pgxCfg, err := pgxpool.ParseConfig(c.cfg.dsn(database))
	if err != nil {
		return nil, fmt.Errorf("invalid connection configuration for database %q: %w", database, err)
	}
	if pgxCfg.MaxConns < 5 {
		pgxCfg.MaxConns = 5
	}

	pool, err := pgxpool.NewWithConfig(ctx, pgxCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool for database %q: %w", database, err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to connect to database %q: %w", database, err)
	}

	c.pools[database] = pool
	return pool, nil
}

func (c *Client) WithCatalogLock(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return c.WithCatalogLockOnDatabase(ctx, c.adminDB, fn)
}

func (c *Client) WithCatalogLockOnDatabase(ctx context.Context, database string, fn func(ctx context.Context, tx pgx.Tx) error) error {
	pool, err := c.poolFor(ctx, database)
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", advisoryLockKey); err != nil {
		return fmt.Errorf("failed to acquire catalog advisory lock: %w", err)
	}

	if err := fn(ctx, tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

func (c *Client) WithCatalogLockNoTx(ctx context.Context, fn func(ctx context.Context, conn *pgxpool.Conn) error) error {
	pool, err := c.poolFor(ctx, c.adminDB)
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", advisoryLockKey); err != nil {
		return fmt.Errorf("failed to acquire catalog advisory lock: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", advisoryLockKey)
	}()

	return fn(ctx, conn)
}

func (c *Client) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	pool, err := c.poolFor(ctx, c.adminDB)
	if err != nil {
		return nil, err
	}
	return pool.Query(ctx, sql, args...)
}

func (c *Client) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	pool, err := c.poolFor(ctx, c.adminDB)
	if err != nil {
		return errRow{err}
	}
	return pool.QueryRow(ctx, sql, args...)
}

func (c *Client) QueryOnDatabase(ctx context.Context, database, sql string, args ...any) (pgx.Rows, error) {
	pool, err := c.poolFor(ctx, database)
	if err != nil {
		return nil, err
	}
	return pool.Query(ctx, sql, args...)
}

func (c *Client) QueryRowOnDatabase(ctx context.Context, database, sql string, args ...any) pgx.Row {
	pool, err := c.poolFor(ctx, database)
	if err != nil {
		return errRow{err}
	}
	return pool.QueryRow(ctx, sql, args...)
}

type errRow struct{ err error }

func (r errRow) Scan(...any) error { return r.err }

// ownerChangeDatabase picks the database an owner change of database runs in: REASSIGN OWNED only
// reaches objects of the database it's executed in, a plain ALTER DATABASE works from the admin one.
func (c *Client) ownerChangeDatabase(database string, reassign bool) string {
	if reassign {
		return database
	}
	return c.adminDB
}
