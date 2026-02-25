# Standard dot access — TraverseAttr
local.bar
module.foo.output_name
aws_instance.web.id

# Bracket with string literal — TraverseIndex(cty.StringVal)
local["bar"]                    # uncommon but valid HCL
module["foo"].output_name       # valid, unusual
aws_instance["web"].id          # valid, unusual

# for_each / resource with count — TraverseIndex(cty.NumberVal or unknown)
aws_instance.web[0].id          # count-based: Index(NumberIntVal(0))
aws_instance.web[each.key].id   # for_each: Index(unknown/relative traversal)
module.foo[each.key].output     # for_each module

# Nested access — index after identity is fine to ignore for node ID purposes
var.settings["region"]          # var.settings is the node, ["region"] is a sub-access
local.tags["env"]               # local.tags is the node