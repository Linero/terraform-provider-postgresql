resource "postgresql_grant" "app_tables" {
  object_type = "table"
  database    = postgresql_database.app.name
  schema      = "public"
  role        = postgresql_role.app.name
  privileges  = ["SELECT", "INSERT", "UPDATE", "DELETE"]
}
