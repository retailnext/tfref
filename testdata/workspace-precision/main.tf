module "foo" {
  source = "./modules/foo"
}

module "bar" {
  source     = "./modules/bar"
  foo_result = module.foo.result
}

module "baz" {
  source           = "./modules/baz"
  independent_data = module.bar.independent_output
}

module "zee" {
  source       = "./modules/zee"
  derived_data = module.bar.derived_output
}

module "buckets" {
  source   = "./modules/foo"
  for_each = { prod = "prod", dev = "dev" }
}

locals {
  prod_result = module.buckets["prod"].result
  zee_derived = module.zee.final_output
}
