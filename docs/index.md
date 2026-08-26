---
page_title: "postgresql Provider"
description: |-
  Manage PostgreSQL roles, databases, and grants.
---

# postgresql Provider

Manages PostgreSQL roles, databases, database ownership, and grants (including
default privileges for future objects).

Every catalog-mutating statement this provider issues is serialized behind an
in-process mutex plus a PostgreSQL advisory lock (`pg_advisory_xact_lock`/
`pg_advisory_lock`), so concurrent `terraform apply` operations - whether
parallel resources within one apply or separate applies against the same
cluster - never race on shared catalog rows (`pg_namespace.nspacl`,
`pg_default_acl`, `pg_database.datacl`, `pg_authid`), which is what causes
PostgreSQL's `tuple concurrently updated` error with naive concurrent
GRANT/REVOKE statements.

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
