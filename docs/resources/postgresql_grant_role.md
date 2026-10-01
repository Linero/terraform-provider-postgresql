---
page_title: "postgresql_grant_role Resource - postgresql"
description: |-
  Grants membership in one role to another.
---

# postgresql_grant_role (Resource)

Grants membership in one role to another (`GRANT grant_role TO role`). `role` inherits `grant_role`'s privileges.

`postgresql_role`'s own `roles` attribute does the same thing, but runs inside that role's `CREATE ROLE`. That fails when the parent role is created in the same apply, e.g. when both roles come from one `for_each`, because Terraform gives instances of the same resource no order. This resource is a separate step you can sequence after both roles exist.

Creating it for a membership that already exists (e.g. one granted earlier through `postgresql_role.roles`) only raises a PostgreSQL `NOTICE`, so it adopts the membership without failing. Destroying it runs `REVOKE`, and skips it if either role no longer exists.

~> **Don't manage the same membership here and in `postgresql_role.roles`.** Leave `roles` unset (it's computed) on roles whose memberships this resource manages. Otherwise each resource would try to undo the other.

## Example Usage

```terraform
resource "postgresql_grant_role" "migrator_is_owner" {
  role       = postgresql_role.migrator.name
  grant_role = postgresql_role.owner.name
}
```

## Schema

### Required

- `role` (String) Role that is granted membership (the member). Changing this forces a new resource.
- `grant_role` (String) Role that `role` becomes a member of (the parent). Changing this forces a new resource.

### Read-Only

- `id` (String) `<role>|<grant_role>`.

## Import

```shell
terraform import postgresql_grant_role.migrator_is_owner 'app_migrator|app_owner'
```
