resource "postgresql_database" "app" {
  name = "app_db"
  # owner intentionally left unset - assigned separately below, e.g. because the
  # connecting role can create databases but isn't a member of postgresql_role.app.
}

resource "postgresql_database_owner" "app" {
  database = postgresql_database.app.name
  owner    = postgresql_role.app.name
}
