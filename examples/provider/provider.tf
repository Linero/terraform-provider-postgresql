provider "postgresql" {
  host     = "localhost"
  port     = 5432
  username = "postgres"
  password = var.postgres_admin_password
  database = "postgres"
  sslmode  = "prefer"
}
