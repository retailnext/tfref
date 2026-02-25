output "vpc_id" {
  value = aws_vpc.main.id
}

output "child_a_result" {
  value = module.child_a.result
}
