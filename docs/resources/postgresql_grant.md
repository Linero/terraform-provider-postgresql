---
page_title: "postgresql_grant Resource - postgresql"
description: |-
  Grants privileges on a database, schema, or all tables/sequences in a schema, to a role.
---

# postgresql_grant (Resource)

Grants privileges on a database, schema, or all tables/sequences in a schema,
to a role.

## Example Usage

```terraform
resource "postgresql_grant" "app_tables" {
  object_type = "table"
  database    = postgresql_database.app.name
  schema      = "public"
  role        = postgresql_role.app.name
  privileges  = ["SELECT", "INSERT", "UPDATE", "DELETE"]
}
```

## Schema

### Required

- `object_type` (String) One of: `database`, `schema`, `table`, `sequence`. `table`/`sequence` apply to ALL tables/sequences currently in the schema. Changing this forces a new resource.
- `database` (String) Database the grant applies to. Changing this forces a new resource.
- `role` (String) Role the privileges are granted to. Changing this forces a new resource.
- `privileges` (Set of String) Privileges to grant, e.g. `["SELECT", "INSERT"]`.

### Optional

- `schema` (String) Schema the grant applies to. Required unless `object_type = "database"`, and must be omitted when it is. Changing this forces a new resource.
- `with_grant_option` (Boolean) Defaults to `false`.

### Read-Only

- `id` (String)
