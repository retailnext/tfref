variable "independent_data" {}

resource "random_string" "param" {
  length  = 8
  special = false
  keepers = { data = var.independent_data }
}

output "param_output" {
  value = random_string.param.result
}
