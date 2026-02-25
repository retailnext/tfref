variable "vpc_id" {}

resource "aws_subnet" "a" {
  vpc_id     = var.vpc_id
  cidr_block = "10.0.1.0/24"
}

output "result" {
  value = aws_subnet.a.id
}
