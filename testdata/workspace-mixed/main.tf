# Mixed workspace: root uses .tf, one child uses .tf, another uses .tofu.

resource "random_string" "main" {
  length  = 8
  special = false
}

module "tf_child" {
  source = "./child-tf"
  input  = random_string.main.result
}

module "tofu_child" {
  source = "./child-tofu"
  input  = random_string.main.result
}

locals {
  combined = "${module.tf_child.result}-${module.tofu_child.result}"
}
