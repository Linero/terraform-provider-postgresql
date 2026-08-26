# Postgresql Provider

The Postgresql provider provides resources to interact with a PostgreSQL server, specifically for managing roles, databases, database ownership, and grants (including default privileges for future objects).

Every catalog-mutating statement this provider issues is serialized behind an in-process mutex plus a PostgreSQL advisory lock (`pg_advisory_xact_lock`/`pg_advisory_lock`), so concurrent `terraform apply` operations - whether parallel resources within one apply or separate applies against the same cluster - never race on shared catalog rows (`pg_namespace.nspacl`, `pg_default_acl`, `pg_database.datacl`, `pg_authid`), which is what causes PostgreSQL's `tuple concurrently updated` error with naive concurrent GRANT/REVOKE statements.

## Example Usage

```terraform
provider "postgresql" {
  host     = "localhost"
  port     = 5432
  username = "postgres"
  password = var.postgres_admin_password
  database = "postgres"
  sslmode  = "prefer"
}

resource "postgresql_role" "app" {
  name  = "app_user"
  login = true
  roles = ["app_readers"]

  password_wo         = var.app_user_password
  password_wo_version = 1
}

resource "postgresql_database" "app" {
  name  = "app_db"
  owner = postgresql_role.app.name
}

resource "postgresql_grant" "app_tables" {
  object_type = "table"
  database    = postgresql_database.app.name
  schema      = "public"
  role        = postgresql_role.app.name
  privileges  = ["SELECT", "INSERT", "UPDATE", "DELETE"]
}

resource "postgresql_default_privileges" "app_tables" {
  object_type = "table"
  database    = postgresql_database.app.name
  schema      = "public"
  owner       = postgresql_role.app.name
  role        = postgresql_role.app.name
  privileges  = ["SELECT", "INSERT", "UPDATE", "DELETE"]
}
```

## Schema

### Required

- `host` (String) PostgreSQL server host.
- `username` (String) Username used to authenticate the admin connection.
- `password` (String, Sensitive) Password used to authenticate the admin connection.

### Optional

- `port` (Number) PostgreSQL server port. Defaults to 5432.
- `database` (String) Database the provider connects to in order to issue role/database DDL. Defaults to `"postgres"`. `postgresql_grant` and `postgresql_default_privileges` connect to their own `database` attribute instead, since schema/table/sequence catalogs are per-database.
- `sslmode` (String) SSL mode for the connection (`disable`, `allow`, `prefer`, `require`, `verify-ca`, `verify-full`). Defaults to `"prefer"`.
- `connect_timeout` (Number) Maximum wait, in seconds, for a connection to be established. Defaults to 15.

## Resources

- `postgresql_role` - a PostgreSQL role, with a write-only `password_wo`/`password_wo_version` pair (see `docs/resources/postgresql_role.md`).
- `postgresql_database` - a PostgreSQL database, with an optional `owner`.
- `postgresql_database_owner` - manages only a database's owner (`ALTER DATABASE ... OWNER TO`), independent of `postgresql_database`'s own `owner` attribute - useful when the connecting role can create databases but isn't a member of the intended owner role.
- `postgresql_grant` - grants privileges on a database, schema, or all tables/sequences in a schema, to a role.
- `postgresql_default_privileges` - grants privileges on objects an owner creates in a schema in the future.

Creating a `postgresql_role` or `postgresql_database` that already exists outside of Terraform's state adopts it (via `ALTER`) instead of failing, so this provider can take over management without a manual `terraform import`. Grants and default privileges are idempotent in PostgreSQL itself, so they never hit this conflict in the first place.

See `docs/` for the full schema of every resource, and `examples/` for one `.tf` snippet per resource.
