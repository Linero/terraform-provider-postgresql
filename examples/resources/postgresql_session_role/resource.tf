# Every session app_migrator opens in app_db runs as app_owner - no SET ROLE needed,
# and everything it creates is owned by app_owner.
resource "postgresql_session_role" "migrator_as_owner" {
  role         = postgresql_role.migrator.name
  database     = postgresql_database.app.name
  session_role = postgresql_role.owner.name

  depends_on = [postgresql_grant_role.migrator_is_owner]
}
