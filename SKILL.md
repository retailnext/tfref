---
name: tfref
description: Trace all direct and transitive references to any Terraform/OpenTofu symbol in a workspace — across module boundaries — with exact file:line:col citations.
invocation: tfref
---

# tfref — Terraform Reference Tracer

## What this skill does

Given a target Terraform address (e.g. `module.cloud`, `local.result`, `module.foo.aws_s3_bucket.mybucket`), `tfref` returns **every node in the workspace that directly or transitively references that target**, in order of traversal depth.

It works by parsing `.tf` and `.tofu` source files directly (no `terraform plan`, no credentials, no remote backend access needed). All expression types are handled: string interpolation, ternary, for-expressions, splats, bracket access, `depends_on`, and more.

## How to invoke

```bash
cd ~/.copilot/skills/tfref && go run . <workspace-dir> <target-addr> [flags]
```

**Flags:**
- `--format json` — machine-readable output (recommended for skill use)
- `--format text` — human-readable table (default)
- `--direction forward` — what does target depend on? (default: `backward` = who depends on target)
- `--show-internals` — include nodes inside the target module

## Address format

Addresses encode module nesting as leading `module.NAME` segments:

| Address | Meaning |
|---|---|
| `module.cloud` | The `module "cloud"` call in the root module |
| `local.result` | `local.result` in the root module |
| `module.foo.aws_s3_bucket.mybucket` | `aws_s3_bucket.mybucket` inside module `foo` |
| `module.foo.output.result` | `output "result"` inside module `foo` |
| `module.foo.module.bar.local.x` | `local.x` inside module `bar` inside module `foo` |

## Example: backward search (who depends on target)

```bash
cd ~/.copilot/skills/tfref && go run . /path/to/workspace module.cloud --format json
```

Output:
```json
{
  "target": "module.cloud",
  "direction": "backward",
  "workspace": "/path/to/workspace",
  "node_count": 4,
  "nodes": [
    { "full_addr": "local.cloud", "depth": 1, "file": "outputs.tf", "line": 17, "column": 11, "via": "module.cloud" },
    { "full_addr": "output.cloud", "depth": 2, "file": "outputs.tf", "line": 67, "column": 11, "via": "local.cloud" },
    { "full_addr": "module.write-outputs", "depth": 2, "file": "outputs.tf", "line": 156, "column": 51, "via": "local.cloud" }
  ]
}
```

**Interpreting the output:**
- `depth` = number of hops from the target (1 = direct reference)
- `file`, `line`, `column` = exact location of the reference expression in the source file
- `via` = the node this node references that connects it to the target chain
- `full_addr` = full Terraform address of the referencing node

## Example: forward search (what does target depend on)

```bash
cd ~/.copilot/skills/tfref && go run . /path/to/workspace module.cloud --direction forward --format json
```

## Module resolution

- Relative module sources (`./modules/networking`) are resolved from the workspace
- Remote/registry modules are resolved via `.terraform/modules/modules.json`
- If a module cannot be resolved, the tool returns an error rather than silently returning incomplete results. The error message will tell you to run `terraform init` or `tofu init`.

## When to use this skill

- "What would change if I modify/delete X?"
- "Identify everything in this workspace that derives from module.cloud"
- "Find all places that reference var.region so I can update them"
- "Does module.baz depend on module.foo?"
- "What does module.cloud depend on?"

## Limitations

- `for_each` instances are normalized: `module.foo["prod"].output` and `module.foo["staging"].output` both trace to the same `module.foo` node
- `.tfvars` files are not parsed
- `moved {}` / `import {}` `to =` addresses are not data-flow edges
- Registry modules require a local `.terraform/` cache from `terraform init`
