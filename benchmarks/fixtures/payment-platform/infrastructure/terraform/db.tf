variable "payment_db_password" {
  type      = string
  sensitive = true
}

resource "postgresql_database" "payments" {
  name = "payments"
}
