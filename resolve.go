// Copyright (c) 2026 RetailNext, Inc. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package tfref

import (
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// ctyString is a package-level alias used when checking TraverseIndex key types.
var ctyString = cty.String

// stepName extracts the string identifier from a traversal step.
//
// It returns (name, true) for:
//   - hcl.TraverseAttr — always has a string name
//   - hcl.TraverseIndex with a known, non-null string key
//
// It returns ("", false) for numeric or unknown index keys (count / for_each
// instance accessors).  These steps do not change node identity and should be
// skipped.
func stepName(step hcl.Traverser) (string, bool) {
	switch s := step.(type) {
	case hcl.TraverseAttr:
		return s.Name, true
	case hcl.TraverseIndex:
		if s.Key.Type() == cty.String && s.Key.IsKnown() && !s.Key.IsNull() {
			return s.Key.AsString(), true
		}
		// Numeric / unknown index (count, for_each) — instance accessor, skip.
		return "", false
	}
	return "", false
}

// traversalToNodeID converts an hcl.Traversal into a NodeID scoped to
// modulePath.
//
// Both dot-access (foo.bar) and bracket-access (foo["bar"]) syntax are handled
// identically.  Numeric and unknown index steps (for count / for_each) are
// treated as instance accessors and stripped from the identity.
//
// Built-in Terraform symbols (path, terraform, each, count, self) are
// returned as a zero NodeID.
func traversalToNodeID(t hcl.Traversal, modulePath string) NodeID {
	if len(t) == 0 {
		return NodeID{}
	}
	root, ok := t[0].(hcl.TraverseRoot)
	if !ok {
		return NodeID{}
	}

	// Collect up to 2 named steps that determine node identity.
	// We stop at the first un-named (numeric/unknown) step so that
	// aws_instance.web[0].id and aws_instance.web[each.key].id both resolve
	// to "aws_instance.web".
	namedSteps := make([]string, 0, 2)
	for _, step := range t[1:] {
		name, ok := stepName(step)
		if !ok {
			break
		}
		namedSteps = append(namedSteps, name)
		if len(namedSteps) == 2 {
			break
		}
	}

	var addr string
	switch root.Name {
	case "var":
		// var.name  or  var["name"]
		if len(namedSteps) >= 1 {
			addr = "var." + namedSteps[0]
		}

	case "local":
		// local.name  or  local["name"]
		if len(namedSteps) >= 1 {
			addr = "local." + namedSteps[0]
		}

	case "module":
		// Modules require special handling because for_each instance keys are
		// TraverseIndex steps that should be skipped, unlike resources where a
		// string TraverseIndex is part of the node identity (bracket syntax).
		//
		// module.foo.output           → module.foo.output   (plain)
		// module.foo["prod"].output   → module.foo.output   (string for_each key)
		// module.foo[each.key].output → module.foo.output   (unknown for_each key)
		// module.foo[0].output        → module.foo.output   (count key)
		// module.foo                  → module.foo          (bare reference)
		steps := t[1:]
		if len(steps) == 0 {
			break
		}
		// Step 1: module label name (attr or bracket-access string).
		label, ok := stepName(steps[0])
		if !ok {
			break
		}
		addr = "module." + label
		// Step 2: skip any TraverseIndex (for_each / count instance accessor),
		// then pick up the output attribute if present.
		i := 1
		if i < len(steps) {
			if _, isIdx := steps[i].(hcl.TraverseIndex); isIdx {
				i++ // skip the instance key
			}
		}
		if i < len(steps) {
			if outName, ok := stepName(steps[i]); ok {
				addr = "module." + label + "." + outName
			}
		}

	case "data":
		// data.type.name  or  data["type"]["name"]
		if len(namedSteps) >= 2 {
			addr = "data." + namedSteps[0] + "." + namedSteps[1]
		}

	case "path", "terraform", "each", "count", "self":
		// Built-in symbols — not user-defined nodes.
		return NodeID{}

	default:
		// Resource reference: type.name  or  type["name"]
		if len(namedSteps) >= 1 {
			addr = root.Name + "." + namedSteps[0]
		}
	}

	if addr == "" {
		return NodeID{}
	}
	return NodeID{ModulePath: modulePath, Addr: addr}
}

// blockToAddr returns the Terraform address string for a top-level block,
// or an empty string if the block type is not one that creates a named node.
func blockToAddr(block *hclsyntax.Block) string {
	switch block.Type {
	case "resource":
		if len(block.Labels) == 2 {
			return block.Labels[0] + "." + block.Labels[1]
		}
	case "data":
		if len(block.Labels) == 2 {
			return "data." + block.Labels[0] + "." + block.Labels[1]
		}
	case "variable":
		if len(block.Labels) == 1 {
			return "var." + block.Labels[0]
		}
	case "output":
		if len(block.Labels) == 1 {
			return "output." + block.Labels[0]
		}
	case "module":
		if len(block.Labels) == 1 {
			return "module." + block.Labels[0]
		}
	}
	return ""
}

// addressLiteralToNodeID resolves a traversal from an import/moved/removed
// block's to= or from= attribute.  These attributes use Terraform's resource
// address syntax, not HCL output-reference syntax:
//
//	module.cloud.google_project.this           → NodeID{"module.cloud", "google_project.this"}
//	module.cloud.module.sub.aws_s3_bucket.b    → NodeID{"module.cloud/module.sub", "aws_s3_bucket.b"}
//	aws_s3_bucket.old                          → NodeID{callerModulePath, "aws_s3_bucket.old"}
//	data.google_project.main                   → NodeID{callerModulePath, "data.google_project.main"}
//
// Returns (NodeID{}, false) when the traversal cannot be mapped to a resource
// address (e.g. bare module reference with no resource suffix).
func addressLiteralToNodeID(trav hcl.Traversal, callerModulePath string) (NodeID, bool) {
	steps := trav
	modulePath := callerModulePath

	// Consume leading module.LABEL pairs, building up the absolute module path.
	for len(steps) >= 2 {
		var rootName string
		switch r := steps[0].(type) {
		case hcl.TraverseRoot:
			rootName = r.Name
		case hcl.TraverseAttr:
			rootName = r.Name
		}
		if rootName != "module" {
			break
		}
		label, ok := stepName(steps[1])
		if !ok {
			break
		}
		modulePath = joinModulePath(modulePath, "module."+label)
		steps = steps[2:]
	}

	if len(steps) == 0 {
		// Bare module reference with no resource suffix — not a resource address.
		return NodeID{}, false
	}

	// Extract the first name from the remaining steps.  After module pair
	// consumption the first remaining step is a TraverseAttr; at the start of
	// a non-module traversal it is a TraverseRoot, which stepName() does not
	// handle.  Extract it explicitly.
	var firstPart string
	rest := steps[1:]
	switch r := steps[0].(type) {
	case hcl.TraverseRoot:
		firstPart = r.Name
	case hcl.TraverseAttr:
		firstPart = r.Name
	default:
		return NodeID{}, false
	}

	// Collect the resource address parts from the remaining steps.
	// Expected shapes: TYPE.NAME  or  data.TYPE.NAME
	// Skip index steps (for_each instance keys like ["prod"] or [0]).
	addrParts := []string{firstPart}
	for _, step := range rest {
		name, ok := stepName(step)
		if !ok {
			continue // skip TraverseIndex (instance key)
		}
		addrParts = append(addrParts, name)
		if len(addrParts) == 2 && addrParts[0] != "data" {
			break // TYPE.NAME complete
		}
		if len(addrParts) == 3 {
			break // data.TYPE.NAME complete
		}
	}

	if len(addrParts) < 2 {
		return NodeID{}, false
	}
	return NodeID{modulePath, strings.Join(addrParts, ".")}, true
}

// joinModulePath appends a child call label to a module path.
//
//	("",           "module.foo") → "module.foo"
//	("module.foo", "module.bar") → "module.foo/module.bar"
func joinModulePath(base, child string) string {
	if base == "" {
		return child
	}
	return base + "/" + child
}

// moduleManifestKey returns the key used in .terraform/modules/modules.json
// for a module call.
//
// The manifest uses dot-separated labels without the "module." prefix:
//
//	root module calling "foo"         → "foo"
//	module.foo calling "bar"          → "foo.bar"
//	module.foo/module.bar calling "z" → "foo.bar.z"
func moduleManifestKey(modulePath, label string) string {
	if modulePath == "" {
		return label
	}
	parts := strings.Split(modulePath, "/")
	keys := make([]string, 0, len(parts)+1)
	for _, p := range parts {
		keys = append(keys, strings.TrimPrefix(p, "module."))
	}
	keys = append(keys, label)
	return strings.Join(keys, ".")
}
