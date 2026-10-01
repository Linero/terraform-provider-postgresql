---
page_title: "postgresql_session_role Resource - postgresql"
description: |-
  Makes a login role's sessions automatically run as another role.
---

# postgresql_session_role (Resource)

Makes a login role's sessions automatically switch to another role on connect (`ALTER ROLE role [IN DATABASE database] SET role = session_role`). The effect is the same as if every session ran `SET ROLE session_role` first:

- `session_user` stays `role`, but `current_user` is `session_role`.
- Privilege checks use `session_role`, and every object the session creates is **owned by `session_role`**.

The typical use is a service that runs its own migrations while acting as a database's owner. Anything it creates is owned by the owner, so the owner can manage it. Default privileges set up `FOR ROLE <owner>` cover it too, with no `SET ROLE` needed in the migrations.

`role` must be a member of `session_role` (see `postgresql_grant_role`). On PostgreSQL 16 and later, that membership needs the `SET` option, which a plain `GRANT` includes by default. Destroying this resource runs `ALTER ROLE ... RESET role`, and skips it if the role or database no longer exists.

## Example Usage

```terraform
# Every session app_migrator opens in app_db runs as app_owner - no SET ROLE needed,
# and everything it creates is owned by app_owner.
resource "postgresql_session_role" "migrator_as_owner" {
  role         = postgresql_role.migrator.name
  database     = postgresql_database.app.name
  session_role = postgresql_role.owner.name

  depends_on = [postgresql_grant_role.migrator_is_owner]
}
```

## Schema

### Required

- `role` (String) Login role whose sessions switch role. Changing this forces a new resource.
- `session_role` (String) Role the sessions switch to.

### Optional

- `database` (String) Only switch role in sessions connected to this database (`IN DATABASE`); the database must exist. If unset, applies in every database. Changing this forces a new resource.

### Read-Only

- `id` (String) `<role>|<database>`, or just `<role>` when `database` is unset.

## Import

```shell
terraform import postgresql_session_role.migrator_as_owner 'app_migrator|app_db'
```
