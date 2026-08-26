resource "postgresql_role" "app" {
  name  = "app_user"
  login = true
  roles = ["app_readers"]

  password_wo         = var.app_user_password
  password_wo_version = 1
}
