variable "foo_result" {}

resource "random_string" "derived" {
  length  = 16
  special = false
  keepers = { foo = var.foo_result }
}

output "derived_output" {
  value = random_string.derived.result
}

output "independent_output" {
  value = "static-value"
}
