# tfref — Copilot Instructions

This repo implements `tfref`, a Go library and CLI tool for tracing transitive Terraform/OpenTofu references across module boundaries. It is designed as a GitHub Copilot skill.

---

## Architecture Overview

```
tfref/
├── model.go        — NodeID, Ref, Graph, BackwardResult types
├── parse.go        — ParseWorkspace, parseModule, HCL traversal extraction, module stitching
├── resolve.go      — traversalToNodeID, blockToAddr, stepName, module for_each handling
├── query.go        — DeepBackwardRefs, DeepForwardRefs, ParseFullAddr, print helpers
├── tfref_test.go   — unit + integration tests (22 tests, all patterns)
├── testdata/       — fixture workspaces for testing
│   ├── workspace-tf/       — pure .tf workspace
│   ├── workspace-tofu/     — pure .tofu workspace
│   ├── workspace-mixed/    — mixed .tf and .tofu files
│   └── workspace-precision/ — baz/zee module boundary precision scenario
└── cmd/tfref/
    └── main.go     — CLI: flag parsing, text/JSON output, full address parsing
```

---

## Key Design Decisions

### Why HCL parsing (not LSP / terraform graph)?

- `terraform graph` requires `terraform init` and can't read remote state
- `tofu-ls`/`terraform-ls` `textDocument/references` returns empty or only direct refs
- `hashicorp/hcl/v2` is the official parser; `Expression.Variables()` returns ALL traversals from any expression type (interpolation, for-expr, splat, etc.) without manual AST walking

### NodeID and module paths

- `NodeID{ModulePath, Addr}` uniquely identifies any node in the workspace
- `ModulePath` is slash-separated: `""` = root, `"module.foo"` = child, `"module.foo/module.bar"` = nested
- `Addr` is the local address: `"aws_instance.web"`, `"var.region"`, `"local.bar"`, `"output.result"`, `"module.child"`

### Three-phase stitching (parse.go)

Module boundary stitching happens in three phases after all files in a module dir are parsed:

1. **Phase 1**: Recurse into all child module directories (collect their nodes/edges)
2. **Phase 2**: Stitch input variables (`var.X` in child → caller's expression nodes). Uses `propagateVarStitch` to also wire existing consumers of `var.X` to the caller nodes.
3. **Phase 3**: Stitch output references — replace opaque `module.X.outputName` refs in the caller with the real `output.outputName` node in the child. This phase runs AFTER var-stitching so cross-module edges added by `propagateVarStitch` are also stitched.

This ordering is critical: if output-stitching ran per-module before all var-stitching, cross-module edges would miss stitching.

### stitchBareModuleRefs

When a node references `module.X` as a bare ref (e.g. `[for k, v in module.child : v.result]` or `depends_on = [module.foo]`), we cannot know which specific outputs are accessed. `stitchBareModuleRefs` conservatively adds edges from the referencing node to ALL outputs of the child module.

### module for_each handling (resolve.go)

`module.foo["prod"].output_x`, `module.foo[each.key].output_x`, and `module.foo[0].output_x` all normalize to `module.foo.output_x`. The `case "module":` branch in `traversalToNodeID` does its own traversal walk:
1. Get module label from step 1
2. Skip any `TraverseIndex` step (all key types) as an instance accessor
3. Remaining GetAttr is the output name

### visited map key = modulePath (not absDir)

The recursion guard uses `modulePath` (the unique call chain) as the key, NOT the filesystem path. This allows two module calls with the same source directory (e.g. `module.foo` and `module.buckets` both sourcing `./modules/foo`) to each get their own graph nodes and stitching.

---

## How to Extend

### Adding a new HCL block type

In `parse.go` `parseModule`, the `switch block.Type` handles `"module"`, `"variable"`, `"output"`, `"locals"`, and `default` (resources, data). To add a new block type:
1. Add a `case "yourtype":` in the switch
2. Compute an `addr` string for the node
3. Call `walkBodyRefs(block.Body, owner, modulePath, graph)` to collect all expression references

### Adding a new expression reference type

`walkExprRefs` already handles all expression types via `expr.Variables()`. No changes needed for new expression patterns — the HCL library handles them.

### Extending the CLI

The CLI is in `cmd/tfref/main.go`. Add new flags with `flag.Bool` / `flag.String` and register any new value-taking flags in the `valueTakers` map in `reorderArgs` so they work when placed after positional args.

---

## Test Patterns

Tests in `tfref_test.go` follow two patterns:

1. **Inline TF** (simple cases): `t.TempDir()` + `writeFile` with HCL content strings. Use newlines as attribute separators (HCL does not allow `;`).

2. **Testdata fixtures** (complex cases): pre-built workspaces in `testdata/`. Used for cross-module scenarios.

The `hasRef(results, modulePath, addr)` helper checks if a NodeID appears as a `From` in any result.

To add a new test:
- For single-file patterns: inline TF in `t.TempDir()`
- For module boundary scenarios: add to `testdata/workspace-precision/` or create a new `testdata/workspace-X/` dir

---

## Module Resolution Edge Cases

- Relative paths that don't exist on disk → error (not silent skip)
- Registry modules without a `.terraform/` cache → error with `terraform init` hint
- Two module calls to same source dir → both parsed independently (keyed by modulePath)
- Mixed `.tf` + `.tofu` extensions in same dir → both globbed and parsed together
