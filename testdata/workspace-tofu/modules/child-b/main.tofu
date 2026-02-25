variable "env" {}

resource "random_string" "app" {
  length  = 8
  special = false
  keepers = { env = var.env }
}

resource "random_string" "derived" {
  length  = 16
  special = false
  keepers = { source = random_string.app.result }
}

output "derived_output" {
  value = random_string.derived.result
}

output "independent_output" {
  value = var.env
}
