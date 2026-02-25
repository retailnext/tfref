output "main_result" {
  value = random_string.main.result
}

output "child_a_result" {
  value = module.child_a["prod"].result
}
