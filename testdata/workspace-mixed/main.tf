# Mixed workspace: root uses .tf, one child uses .tf, another uses .tofu.

resource "aws_vpc" "main" {
  cidr_block = "10.0.0.0/16"
}

module "tf_child" {
  source = "./child-tf"
  vpc_id = aws_vpc.main.id
}

module "tofu_child" {
  source = "./child-tofu"
  vpc_id = aws_vpc.main.id
}

locals {
  combined = "${module.tf_child.result}-${module.tofu_child.result}"
}
