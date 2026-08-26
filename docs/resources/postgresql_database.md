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

## Import

```shell
terraform import postgresql_database.app app_db
```
