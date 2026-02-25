variable "vpc_id" {}

resource "aws_subnet" "b" {
  vpc_id     = var.vpc_id
  cidr_block = "10.0.2.0/24"
}

output "result" {
  value = aws_subnet.b.id
}
