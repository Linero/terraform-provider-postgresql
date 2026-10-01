resource "postgresql_grant_role" "migrator_is_owner" {
  role       = postgresql_role.migrator.name
  grant_role = postgresql_role.owner.name
}
