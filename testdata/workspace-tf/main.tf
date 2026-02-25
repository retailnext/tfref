# Root workspace exercising all reference forms.

resource "random_string" "main" {
  length  = 8
  special = false
}

resource "random_string" "web" {
  length  = random_string.main.length  # plain attribute reference
  special = false
}

resource "random_string" "db" {
  depends_on = [random_string.main]   # explicit dependency
  length     = random_string.main.length
  special    = false
}

resource "random_integer" "items" {
  count = 3
  min   = 1
  max   = 100
}

locals {
  ids            = [random_string.web.result, random_string.db.result]
  selected       = var.env == "prod" ? random_string.main.result : "fallback"  # ternary
  all_results_fe = [for r in [random_string.web, random_string.db] : r.result]  # for-expr
  all_item_ids   = random_integer.items[*].result  # splat
  child_a_result = module.child_a["prod"].result
  child_b_result = module.child_b[0].derived_output
  all_child_a    = [for k, v in module.child_a : v.result]
}

module "child_a" {
  source   = "./modules/child-a"
  for_each = { prod = "prod", dev = "dev" }
  env      = each.key
  input    = random_string.main.result
}

module "child_b" {
  source = "./modules/child-b"
  count  = 2
  env    = var.env
}
