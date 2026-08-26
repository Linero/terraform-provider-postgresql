resource "postgresql_default_privileges" "app_tables" {
  object_type = "table"
  database    = postgresql_database.app.name
  schema      = "public"
  owner       = postgresql_role.owner.name
  role        = postgresql_role.app.name
  privileges  = ["SELECT", "INSERT", "UPDATE", "DELETE"]
}
