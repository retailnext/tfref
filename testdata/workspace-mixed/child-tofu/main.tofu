variable "input" {}

resource "random_string" "b" {
  length  = 8
  special = false
  keepers = { src = var.input }
}

output "result" {
  value = random_string.b.result
}
