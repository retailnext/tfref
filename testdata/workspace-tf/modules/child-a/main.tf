variable "env" {}
variable "vpc_id" {}

resource "aws_subnet" "main" {
  vpc_id            = var.vpc_id
  cidr_block        = "10.0.1.0/24"
  availability_zone = "us-east-1a"
  tags = {
    Env = var.env
  }
}

output "result" {
  value = aws_subnet.main.id
}
