terraform {
  required_version = ">= 1.5"
}

resource "null_resource" "deploy" {
  triggers = {
    version = var.app_version
  }
}

output "version" {
  value = var.app_version
}
