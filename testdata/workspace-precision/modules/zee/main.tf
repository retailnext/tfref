variable "derived_data" {}

resource "random_string" "alarm" {
  length  = 8
  special = false
  keepers = { data = var.derived_data }
}

output "final_output" {
  value = random_string.alarm.result
}
