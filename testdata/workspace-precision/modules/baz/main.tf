variable "independent_data" {}

resource "aws_ssm_parameter" "param" {
  name  = "/app/data"
  type  = "String"
  value = var.independent_data
}

output "param_arn" {
  value = aws_ssm_parameter.param.arn
}
