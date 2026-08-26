---
page_title: "postgresql_role Resource - postgresql"
description: |-
  A PostgreSQL role (CREATE ROLE).
---

# postgresql_role (Resource)

A PostgreSQL role (`CREATE ROLE`).

## Example Usage

```terraform
resource "postgresql_role" "app" {
  name  = "app_user"
  login = true
  roles = ["app_readers"]

  password_wo         = var.app_user_password
  password_wo_version = 1
}
```

## Schema

### Required

- `name` (String) Role name.
- `password_wo` (String, Sensitive, Write-Only) Write-only password for the role. Never stored in state.
- `password_wo_version` (Number) Version of `password_wo`. Changing this value rotates the password, even if `password_wo`'s value is unchanged - Terraform can never diff a write-only value, so this is the only usable rotation signal.

### Optional

- `login` (Boolean) Whether the role can log in. Defaults to `false` (matches PostgreSQL's own `CREATE ROLE` default).
- `superuser` (Boolean) Whether the role is a superuser. Defaults to `false`.
- `create_role` (Boolean) Whether the role can create other roles. Defaults to `false`.
- `create_database` (Boolean) Whether the role can create databases. Defaults to `false`.
- `connection_limit` (Number) Maximum concurrent connections for the role. `-1` (default) means no limit.
- `valid_until` (String) Timestamp after which the role's password is no longer valid. Defaults to `"infinity"` when unset.
- `roles` (List of String) Roles this role is a member of (native PostgreSQL role membership, `GRANT role TO role`).

## Import

```shell
terraform import postgresql_role.app app_user
```

Note: `password_wo`/`password_wo_version` are write-only and can never be imported - set them in configuration after import.
