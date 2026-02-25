locals {
  # both module.foo and module.baz will appear as traversals here
  combined = [for x in module.foo.results : x if contains(module.baz.ids, x)]
}