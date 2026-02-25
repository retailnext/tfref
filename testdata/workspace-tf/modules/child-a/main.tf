variable "env" {}
variable "input" {}

resource "random_string" "resource" {
  length  = 8
  special = false
  keepers = {
    env   = var.env
    input = var.input
  }
}

output "result" {
  value = random_string.resource.result
}
