module "foo" {
  source = "./modules/foo"
}

# bar gets foo's result as an input variable
module "bar" {
  source      = "./modules/bar"
  foo_result  = module.foo.result
}

# baz uses only bar.independent_output — must NOT appear in backward refs from foo
module "baz" {
  source           = "./modules/baz"
  independent_data = module.bar.independent_output
}

# zee uses bar.derived_output (which depends on bar's input from foo) — MUST appear
module "zee" {
  source       = "./modules/zee"
  derived_data = module.bar.derived_output
}

# for_each module scenario: buckets is parameterized with for_each
module "buckets" {
  source   = "./modules/foo"
  for_each = { prod = "prod", dev = "dev" }
}

locals {
  # Reference a specific for_each instance by string key
  prod_result = module.buckets["prod"].result

  # Reference via count (hypothetical — using bar with count index)
  zee_derived = module.zee.final_output
}
