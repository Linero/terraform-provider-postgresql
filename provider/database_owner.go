package provider

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

const reassignOwnedDescription = "When the owner changes, also run REASSIGN OWNED BY <previous owner> TO <new owner> " +
	"inside the database, so every object the previous owner had there (schemas, tables, sequences, functions, " +
	"types, ...) moves to the new owner. REASSIGN OWNED also transfers shared objects (other databases, " +
	"tablespaces); those are handed back to the previous owner in the same transaction, so only this database " +
	"is affected. Skipped (with a warning) when the previous owner is a superuser. Requires the connecting " +
	"role to have the privileges of both the previous and the new owner."

// setDatabaseOwner sets database's owner to newOwner. With reassign, it must run in a transaction
// connected to database itself, since REASSIGN OWNED only reaches objects of the current database.
// Returns a non-empty warning when the reassign step was skipped.
func setDatabaseOwner(ctx context.Context, tx pgx.Tx, database, newOwner string, reassign bool) (string, error) {
	var oldOwner string
	if err := tx.QueryRow(ctx,
		`SELECT pg_catalog.pg_get_userbyid(datdba) FROM pg_catalog.pg_database WHERE datname = $1`,
		database).Scan(&oldOwner); err != nil {
		return "", fmt.Errorf("could not read current database owner: %w", err)
	}

	if _, err := tx.Exec(ctx, fmt.Sprintf("ALTER DATABASE %s OWNER TO %s", quoteIdent(database), quoteIdent(newOwner))); err != nil {
		return "", fmt.Errorf("could not set database owner: %w", err)
	}

	if !reassign || oldOwner == newOwner {
		return "", nil
	}

	var oldIsSuperuser bool
	if err := tx.QueryRow(ctx, `SELECT rolsuper FROM pg_catalog.pg_roles WHERE rolname = $1`, oldOwner).Scan(&oldIsSuperuser); err != nil {
		return "", fmt.Errorf("could not read role %q: %w", oldOwner, err)
	}
	if oldIsSuperuser {
		return fmt.Sprintf("Previous owner %q of database %q is a superuser, so REASSIGN OWNED was skipped: "+
			"it would also move objects the superuser owns for the system. Objects inside the database are left "+
			"owned by %q.", oldOwner, database, oldOwner), nil
	}

	// REASSIGN OWNED also moves shared objects; remember the ones outside this database to hand them back.
	otherDatabases, err := queryNames(ctx, tx,
		`SELECT datname FROM pg_catalog.pg_database WHERE datdba = (SELECT oid FROM pg_catalog.pg_roles WHERE rolname = $1) AND datname <> $2`, oldOwner, database)
	if err != nil {
		return "", fmt.Errorf("could not list other databases owned by %q: %w", oldOwner, err)
	}
	tablespaces, err := queryNames(ctx, tx,
		`SELECT spcname FROM pg_catalog.pg_tablespace WHERE spcowner = (SELECT oid FROM pg_catalog.pg_roles WHERE rolname = $1)`, oldOwner)
	if err != nil {
		return "", fmt.Errorf("could not list tablespaces owned by %q: %w", oldOwner, err)
	}

	if _, err := tx.Exec(ctx, fmt.Sprintf("REASSIGN OWNED BY %s TO %s", quoteIdent(oldOwner), quoteIdent(newOwner))); err != nil {
		return "", fmt.Errorf("could not reassign objects owned by %q to %q: %w", oldOwner, newOwner, err)
	}

	for _, name := range otherDatabases {
		if _, err := tx.Exec(ctx, fmt.Sprintf("ALTER DATABASE %s OWNER TO %s", quoteIdent(name), quoteIdent(oldOwner))); err != nil {
			return "", fmt.Errorf("could not restore owner of database %q: %w", name, err)
		}
	}
	for _, name := range tablespaces {
		if _, err := tx.Exec(ctx, fmt.Sprintf("ALTER TABLESPACE %s OWNER TO %s", quoteIdent(name), quoteIdent(oldOwner))); err != nil {
			return "", fmt.Errorf("could not restore owner of tablespace %q: %w", name, err)
		}
	}
	return "", nil
}

func queryNames(ctx context.Context, tx pgx.Tx, sql string, args ...any) ([]string, error) {
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}
