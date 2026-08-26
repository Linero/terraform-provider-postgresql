resource "postgresql_database" "app" {
  name  = "app_db"
  owner = postgresql_role.app.name
}
