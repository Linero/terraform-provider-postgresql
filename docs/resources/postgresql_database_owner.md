---
page_title: "postgresql_database_owner Resource - postgresql"
description: |-
  Manages only the owner of a PostgreSQL database.
---

# postgresql_database_owner (Resource)

Manages only the owner of a PostgreSQL database (`ALTER DATABASE ... OWNER TO`),
independent of `postgresql_database`'s own (optional) `owner` attribute. This
decouples "who runs `CREATE DATABASE`" from "who owns the result" - useful when
the connecting role can create databases but isn't a member of the intended
owner role, or when ownership assignment should be an explicit, separately
sequenced step.

~> **Destroying this resource does not revert ownership.** There's no
well-defined previous owner to revert to; destroying it only stops Terraform
from managing the database's owner going forward.

## Example Usage

```terraform
resource "postgresql_database" "app" {
  name = "app_db"
  # owner intentionally left unset - assigned separately below, e.g. because the
  # connecting role can create databases but isn't a member of postgresql_role.app.
}

resource "postgresql_database_owner" "app" {
  database = postgresql_database.app.name
  owner    = postgresql_role.app.name
}
```

## Schema

### Required

- `database` (String) Name of the database whose owner is managed. Changing this forces a new resource.
- `owner` (String) Role to assign as the database's owner.

### Optional

- `reassign_owned` (Boolean) Defaults to `false`. When the owner changes, also run `REASSIGN OWNED BY <previous owner> TO <new owner>` inside the database, so every object the previous owner had there (schemas, tables, sequences, functions, types, ...) moves to the new owner. `REASSIGN OWNED` also transfers shared objects (other databases, tablespaces); those are handed back to the previous owner in the same transaction, so only this database is affected. Skipped (with a warning) when the previous owner is a superuser. Requires the connecting role to have the privileges of both the previous and the new owner.
