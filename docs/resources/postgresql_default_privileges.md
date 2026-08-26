---
page_title: "postgresql_default_privileges Resource - postgresql"
description: |-
  Grants privileges on objects owner creates in schema in the future.
---

# postgresql_default_privileges (Resource)

Grants privileges on objects `owner` creates in `schema` in the future
(`ALTER DEFAULT PRIVILEGES ... GRANT ...`). Existing objects are unaffected -
pair this with `postgresql_grant` to also cover objects that already exist.

## Example Usage

```terraform
resource "postgresql_default_privileges" "app_tables" {
  object_type = "table"
  database    = postgresql_database.app.name
  schema      = "public"
  owner       = postgresql_role.owner.name
  role        = postgresql_role.app.name
  privileges  = ["SELECT", "INSERT", "UPDATE", "DELETE"]
}
```

## Schema

### Required

- `object_type` (String) One of: `table`, `sequence`. Changing this forces a new resource.
- `database` (String) Changing this forces a new resource.
- `schema` (String) Changing this forces a new resource.
- `role` (String) Role the default privileges are granted to. Changing this forces a new resource.
- `owner` (String) Role whose future objects (created in `schema`) get these privileges applied automatically. Changing this forces a new resource.
- `privileges` (Set of String)

### Optional

- `with_grant_option` (Boolean) Defaults to `false`.

### Read-Only

- `id` (String)
