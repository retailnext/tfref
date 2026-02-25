# Root workspace exercising all reference forms.

resource "aws_vpc" "main" {
  cidr_block = "10.0.0.0/16"
  tags = {
    Name = "main-${var.env}"  # string interpolation
  }
}

resource "aws_security_group" "web" {
  vpc_id = aws_vpc.main.id  # plain attribute reference
}

resource "aws_security_group" "db" {
  # depends_on explicit dependency
  depends_on = [aws_vpc.main]
  vpc_id     = aws_vpc.main.id
}

# Bracket-access syntax: resource["name"] is equivalent to resource.name
locals {
  vpc_via_bracket = aws_vpc["main"].id
  sg_ids          = [aws_security_group.web.id, aws_security_group.db.id]

  # Ternary conditional — both branches captured
  selected_cidr = var.env == "prod" ? aws_vpc.main.cidr_block : "10.1.0.0/16"

  # For-expression over a resource set
  sg_map = { for sg in [aws_security_group.web, aws_security_group.db] : sg.id => sg.name }

  # Splat expression
  all_sg_ids = [aws_security_group.web.id]

  # Reference through child-a output
  child_a_value = module.child_a.result

  # Reference through child-b output (depends on child-a inside child-b)
  child_b_derived = module.child_b.derived_output
}

# Module call: child_a uses for_each (string keys)
module "child_a" {
  source   = "./modules/child-a"
  for_each = { prod = "prod", dev = "dev" }
  env      = each.key
  vpc_id   = aws_vpc.main.id
}

# Module call: child_b with count
module "child_b" {
  source = "./modules/child-b"
  count  = 2
  env    = var.env
}

data "aws_availability_zones" "available" {}

# Reference to for_each module with string key
locals {
  specific_instance = module.child_a["prod"].result

  # Reference to count module with index
  first_child_b = module.child_b[0].derived_output

  # Reference to for_each module with each.key (in a for-expression context)
  all_results = [for k, v in module.child_a : v.result]
}
