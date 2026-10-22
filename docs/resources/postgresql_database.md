---
page_title: "postgresql_database Resource - postgresql"
description: |-
  A PostgreSQL database (CREATE DATABASE).
---

# postgresql_database (Resource)

A PostgreSQL database (`CREATE DATABASE`).

## Example Usage

```terraform
resource "postgresql_database" "app" {
  name  = "app_db"
  owner = postgresql_role.app.name
}
```

## Schema

### Required

- `name` (String) Database name. Changing this forces a new resource.

### Optional

- `owner` (String) Owning role. When set, applied via `ALTER DATABASE ... OWNER TO` right after creation and kept in sync on every update. When left unset, ownership defaults to whichever role this provider connects as, and this attribute simply tracks the database's actual current owner (e.g. as changed by a separate `postgresql_database_owner` resource) without trying to manage it. See `postgresql_database_owner` for that decoupled pattern.
- `encoding` (String) Defaults to `"UTF8"`. Changing this forces a new resource.
- `lc_collate` (String) Defaults to `"en_US.UTF-8"`. Changing this forces a new resource.
- `lc_ctype` (String) Defaults to `"en_US.UTF-8"`. Changing this forces a new resource.
- `template` (String) Defaults to `"template0"`. Changing this forces a new resource.
- `connection_limit` (Number) `-1` (default) means no limit.
- `reassign_owned` (Boolean) Defaults to `false`. When the owner changes, also run `REASSIGN OWNED BY <previous owner> TO <new owner>` inside the database, so every object the previous owner had there (schemas, tables, sequences, functions, types, ...) moves to the new owner. `REASSIGN OWNED` also transfers shared objects (other databases, tablespaces); those are handed back to the previous owner in the same transaction, so only this database is affected. Skipped (with a warning) when the previous owner is a superuser. Requires the connecting role to have the privileges of both the previous and the new owner. Applies when `owner` changes on update, or when an already existing database is adopted on create (a freshly created database has nothing to reassign).

## Import

```shell
terraform import postgresql_database.app app_db
```
