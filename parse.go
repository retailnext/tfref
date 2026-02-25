// Copyright (c) 2026 RetailNext, Inc. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

package tfref

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

// modulesManifest mirrors the structure of .terraform/modules/modules.json,
// which is written by `terraform init` / `tofu init`.
type modulesManifest struct {
	Modules []struct {
		Key    string `json:"Key"`
		Source string `json:"Source"`
		Dir    string `json:"Dir"`
	} `json:"Modules"`
}

// moduleResolver maps (callerDir, moduleName, source) → filesystem path.
type moduleResolver struct {
	rootDir  string
	manifest *modulesManifest
}

// Resolve returns the absolute filesystem path for a module source and
// whether it could be resolved.
//
//   - Relative paths ("./foo", "../foo") are resolved relative to callerDir.
//   - Everything else is looked up in the .terraform/modules manifest using
//     the module's manifest key (e.g. "foo", "foo.bar").
func (r *moduleResolver) Resolve(callerDir, manifestKey, source string) (string, bool) {
	if strings.HasPrefix(source, "./") || strings.HasPrefix(source, "../") {
		resolved := filepath.Join(callerDir, source)
		if _, err := os.Stat(resolved); err != nil {
			return "", false // directory does not exist on disk
		}
		return resolved, true
	}
	for _, m := range r.manifest.Modules {
		if m.Key == manifestKey {
			return filepath.Join(r.rootDir, m.Dir), true
		}
	}
	return "", false
}

// ParseWorkspace parses an entire Terraform / OpenTofu workspace rooted at
// rootDir and returns a fully stitched cross-module reference Graph.
//
// Child modules are resolved using relative source paths and the
// .terraform/modules/modules.json cache when available.
func ParseWorkspace(rootDir string) (*Graph, error) {
	graph := NewGraph()
	manifest, err := loadModulesManifest(rootDir)
	if err != nil {
		// Not fatal — the manifest may not exist if init has not been run.
		manifest = &modulesManifest{}
	}
	resolver := &moduleResolver{rootDir: rootDir, manifest: manifest}
	return graph, parseModule(rootDir, "", resolver, graph, map[string]bool{})
}

func loadModulesManifest(rootDir string) (*modulesManifest, error) {
	p := filepath.Join(rootDir, ".terraform", "modules", "modules.json")
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var m modulesManifest
	return &m, json.Unmarshal(data, &m)
}

// pendingCall holds everything we need to stitch a module call boundary after
// both the parent and child have been parsed.
type pendingCall struct {
	label           string           // e.g. "foo" from module "foo" { ... }
	childModulePath string           // e.g. "module.foo"
	childDir        string           // absolute filesystem path to child module
	inputBindings   map[string][]Ref // varName → caller-side Refs
}

// parseModule parses a single module directory and recurses into any child
// modules it declares.  modulePath is the canonical module address for this
// module (empty for the root module).  visited prevents infinite recursion.
func parseModule(
	dir string,
	modulePath string,
	resolver *moduleResolver,
	graph *Graph,
	visited map[string]bool,
) error {
	if visited[modulePath] {
		return nil
	}
	visited[modulePath] = true

	// Collect both .tf and .tofu files. Mixed workspaces (some files with each
	// extension) are valid; process all of them together.
	tfFiles, err := filepath.Glob(filepath.Join(dir, "*.tf"))
	if err != nil {
		return err
	}
	tofuFiles, err := filepath.Glob(filepath.Join(dir, "*.tofu"))
	if err != nil {
		return err
	}
	files := append(tfFiles, tofuFiles...)

	var pending []pendingCall

	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		f, diags := hclsyntax.ParseConfig(src, file, hcl.Pos{Line: 1, Column: 1})
		if diags.HasErrors() {
			return fmt.Errorf("parse %s: %s", file, diags.Error())
		}
		body, ok := f.Body.(*hclsyntax.Body)
		if !ok {
			continue
		}

		for _, block := range body.Blocks {
			switch block.Type {

			case "locals":
				// Each attribute in a locals block is its own independent node.
				for attrName, attr := range block.Body.Attributes {
					owner := NodeID{modulePath, "local." + attrName}
					graph.Defined[owner] = true
					walkExprRefs(attr.Expr, owner, modulePath, graph)
				}

			case "module":
				if len(block.Labels) != 1 {
					continue
				}
				label := block.Labels[0]
				childModulePath := joinModulePath(modulePath, "module."+label)
				call := pendingCall{
					label:           label,
					childModulePath: childModulePath,
					inputBindings:   make(map[string][]Ref),
				}
				graph.Defined[NodeID{modulePath, "module." + label}] = true

				var source string
				for attrName, attr := range block.Body.Attributes {
					switch attrName {
					case "source":
						if v, diags := attr.Expr.Value(nil); !diags.HasErrors() && v.Type() == ctyString {
							source = v.AsString()
						}
						continue
					case "version", "providers":
						continue
					case "depends_on", "for_each", "count":
						// Meta-arguments: add direct dependency edges from the module
						// call node rather than treating them as input variable bindings.
						owner := NodeID{modulePath, "module." + label}
						for _, trav := range attr.Expr.Variables() {
							to := traversalToNodeID(trav, modulePath)
							if to.Addr == "" {
								continue
							}
							graph.Add(Ref{From: owner, To: to, Subject: trav.SourceRange()})
						}
						continue
					}
					// Input variable binding: collect what the caller passes in.
					for _, trav := range attr.Expr.Variables() {
						to := traversalToNodeID(trav, modulePath)
						if to.Addr == "" {
							continue
						}
						ref := Ref{
							From:    NodeID{modulePath, "module." + label},
							To:      to,
							Subject: trav.SourceRange(),
						}
						call.inputBindings[attrName] = append(call.inputBindings[attrName], ref)
						graph.Add(ref)
					}
				}

				manifestKey := moduleManifestKey(modulePath, label)
				if source != "" {
					if childDir, ok := resolver.Resolve(dir, manifestKey, source); ok {
						call.childDir = childDir
					} else {
						// Source could not be resolved: not a relative path and not present
						// in the .terraform/modules manifest. Return an explicit error so
						// the caller knows the result would be incomplete.
						hint := ".terraform/modules/modules.json (run 'terraform init' or 'tofu init' to populate it)"
						if strings.HasPrefix(source, "./") || strings.HasPrefix(source, "../") {
							hint = "the directory does not exist on disk"
						}
						return fmt.Errorf(
							"module.%s (source %q) cannot be resolved: %s",
							label, source, hint,
						)
					}
				}
				pending = append(pending, call)

			case "import", "moved", "removed":
				// Administrative blocks referencing resources by Terraform address literal.
				// Use basename+line for a unique, human-readable node ID, e.g. import[main.tf:42].
				filename := filepath.Base(block.OpenBraceRange.Filename)
				line := block.OpenBraceRange.Start.Line
				addr := fmt.Sprintf("%s[%s:%d]", block.Type, filename, line)
				owner := NodeID{modulePath, addr}
				// to= and from= are resource address literals; resolve them with
				// addressLiteralToNodeID so that module.cloud.google_project.this
				// becomes NodeID{"module.cloud", "google_project.this"} rather than
				// the misleading opaque NodeID{"module.cloud", "output.google_project"}.
				// Other attributes (e.g. id= in import blocks) are plain values.
				// The resolved addresses are also added to Defined so that a target
				// that has only a removed/import/moved block (no defining resource block)
				// is still considered to exist.
				for attrName, attr := range block.Body.Attributes {
					switch attrName {
					case "to", "from":
						for _, trav := range attr.Expr.Variables() {
							to, ok := addressLiteralToNodeID(trav, modulePath)
							if !ok || to.Addr == "" {
								continue
							}
							graph.Defined[to] = true
							graph.Add(Ref{From: owner, To: to, Subject: trav.SourceRange()})
						}
					default:
						walkExprRefs(attr.Expr, owner, modulePath, graph)
					}
				}

			default:
				addr := blockToAddr(block)
				if addr == "" {
					continue
				}
				owner := NodeID{modulePath, addr}
				graph.Defined[owner] = true
				walkBodyRefs(block.Body, owner, modulePath, graph)
			}
		}
	}

	// Phase 1: Recurse into all child modules before any stitching.
	for _, call := range pending {
		if call.childDir != "" {
			if err := parseModule(call.childDir, call.childModulePath, resolver, graph, visited); err != nil {
				return fmt.Errorf("module %s: %w", call.childModulePath, err)
			}
		}
	}

	// Phase 2: Stitch all input variables.
	// This may add cross-module edges (e.g. child::resource → opaque caller ref)
	// via propagateVarStitch, which phase 3 will then fully stitch.
	for _, call := range pending {
		for varName, callerRefs := range call.inputBindings {
			childVar := NodeID{call.childModulePath, "var." + varName}
			for _, cr := range callerRefs {
				graph.Add(Ref{From: childVar, To: cr.To, Subject: cr.Subject})
				propagateVarStitch(graph, childVar, cr.To, cr.Subject)
			}
		}
	}

	// Phase 3: Stitch all module output references.  Running after phase 2
	// ensures that cross-module edges added by propagateVarStitch pointing to
	// opaque "module.X.outputName" nodes are also resolved here.
	// Also stitch bare "module.X" refs (e.g. from for-expressions / depends_on)
	// to all outputs of the child module.
	for _, call := range pending {
		stitchModuleOutputs(graph, call.childModulePath, call.label, modulePath)
		stitchBareModuleRefs(graph, call.childModulePath, call.label, modulePath)
	}

	return nil
}

// propagateVarStitch ensures that nodes which already depend on childVar also
// get a direct edge to callerNode, preserving the source position.
func propagateVarStitch(graph *Graph, childVar, callerNode NodeID, rng hcl.Range) {
	for _, existing := range graph.Backward[childVar] {
		graph.Add(Ref{From: existing.From, To: callerNode, Subject: rng})
	}
}

// stitchModuleOutputs wires "module.X.outputName" references that point into
// callerModulePath to the actual "output.outputName" node in the child module.
// The from-node is intentionally unrestricted so that cross-module edges added
// by propagateVarStitch (e.g. child::resource → opaque callerModule ref) are
// also stitched after the three-phase processing order.
func stitchModuleOutputs(graph *Graph, childModulePath, label, callerModulePath string) {
	prefix := "module." + label + "."
	for _, refs := range graph.Forward {
		for _, ref := range refs {
			if ref.To.ModulePath != callerModulePath {
				continue
			}
			if strings.HasPrefix(ref.To.Addr, prefix) {
				outputName := strings.TrimPrefix(ref.To.Addr, prefix)
				childOutput := NodeID{childModulePath, "output." + outputName}
				graph.Add(Ref{From: ref.From, To: childOutput, Subject: ref.Subject})
			}
		}
	}
}

// stitchBareModuleRefs handles bare "module.X" references in the caller —
// these arise from for-expressions iterating over module instances and from
// depends_on = [module.X].  A bare ref is treated conservatively as a
// dependency on all outputs of the child module.
func stitchBareModuleRefs(graph *Graph, childModulePath, label, callerModulePath string) {
	bareAddr := "module." + label
	for _, refs := range graph.Forward {
		for _, ref := range refs {
			if ref.To != (NodeID{callerModulePath, bareAddr}) {
				continue
			}
			for outputID := range graph.Forward {
				if outputID.ModulePath == childModulePath && strings.HasPrefix(outputID.Addr, "output.") {
					graph.Add(Ref{From: ref.From, To: outputID, Subject: ref.Subject})
				}
			}
		}
	}
}

// walkBodyRefs recursively walks a block body, adding a reference edge for
// every expression traversal found.
func walkBodyRefs(body *hclsyntax.Body, owner NodeID, modulePath string, graph *Graph) {
	for _, attr := range body.Attributes {
		walkExprRefs(attr.Expr, owner, modulePath, graph)
	}
	for _, block := range body.Blocks {
		walkBodyRefs(block.Body, owner, modulePath, graph)
	}
}

// walkExprRefs extracts all variable traversals from expr and adds a Ref edge
// for each one that resolves to a known node type.
func walkExprRefs(expr hclsyntax.Expression, owner NodeID, modulePath string, graph *Graph) {
	for _, trav := range expr.Variables() {
		to := traversalToNodeID(trav, modulePath)
		if to.Addr == "" || to == owner {
			continue
		}
		graph.Add(Ref{From: owner, To: to, Subject: trav.SourceRange()})
	}
}
