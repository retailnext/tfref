resource "random_id" "data" {
  byte_length = 8
}

output "result" {
  value = random_id.data.hex
}
