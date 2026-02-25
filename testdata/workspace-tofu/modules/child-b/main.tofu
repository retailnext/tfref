variable "env" {}

resource "aws_instance" "app" {
  ami           = "ami-12345678"
  instance_type = "t3.micro"
  tags = {
    Env = var.env
  }
}

resource "aws_eip" "app" {
  instance = aws_instance.app.id
}

# derived_output depends on aws_instance.app (via aws_eip.app)
output "derived_output" {
  value = aws_eip.app.public_ip
}

# independent_output does NOT depend on aws_instance.app
output "independent_output" {
  value = var.env
}
