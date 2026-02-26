---
name: tfref
description: Trace all direct and transitive references to any Terraform/OpenTofu symbol in a workspace — across module boundaries — with exact file:line positions.
invocation: tfref
---

# tfref — Terraform Reference Tracer

## What this skill does

Given a target Terraform address (e.g. `module.cloud`, `local.result`, `module.foo.aws_s3_bucket.mybucket`), `tfref` returns **every node in the workspace that directly or transitively references that target**, across module boundaries, with exact file:line positions for every reference edge.

It works by parsing `.tf` and `.tofu` source files directly — no `terraform plan`, no credentials, no remote state access required. All expression types are handled: string interpolation (`"${module.foo.result}"`), ternary, for-expressions, splats, bracket access, and `depends_on`.

**Important behaviors:**
- If the target address does not exist in the workspace, the tool exits with an error. A target with no references is fine; a mistyped address is an error.
- `import`, `moved`, and `removed` blocks are always reported as references. When planning to remove or move a module/resource, these blocks **must** be dealt with — the tool ensures they are never silently omitted.
- A resource is considered to exist if it has a defining block (`resource`, `module`, `data`, etc.) **or** if it is the target of an `import`, `moved`, or `removed` block.

## How to invoke

The binary is under `cmd/tfref`. When running from the cloned skill directory:

```bash
cd ~/.copilot/skills/tfref && go run ./cmd/tfref/... <workspace-dir> <target-addr> [flags]
```

Or if installed via `go install github.com/retailnext/tfref/cmd/tfref@latest`:

```bash
tfref <workspace-dir> <target-addr> [flags]
```

`WORKSPACE` defaults to the current directory if omitted:

```bash
cd /path/to/workspace && go run ./cmd/tfref/... module.cloud
```

**Flags:**
- `--direction backward` — who depends on target? (default)
- `--direction forward` — what does target depend on?

## Address format

Addresses encode module nesting as leading `module.NAME` segments:

| Address | Meaning |
|---|---|
| `module.cloud` | The `module "cloud"` call in the root module |
| `local.result` | `local.result` in the root module |
| `aws_instance.web` | Resource in the root module |
| `data.google_project.main` | Data source in the root module |
| `module.foo.aws_s3_bucket.mybucket` | `aws_s3_bucket.mybucket` inside module `foo` |
| `module.foo.output.result` | `output "result"` inside module `foo` |
| `module.foo.module.bar.local.x` | `local.x` inside module `bar` inside module `foo` |

## Example: DOT output

```bash
cd ~/.copilot/skills/tfref && go run ./cmd/tfref/... /path/to/workspace module.cloud
```

```dot
// tfref backward: module.cloud
// workspace: /path/to/workspace
// 354 node(s) found

digraph tfref {
  rankdir=BT;  // bottom-to-top: dependents above, dependencies below
  node [fontname="Helvetica", fontsize=10];
  edge [fontname="Helvetica", fontsize=9, color="#555555"];

  "module.cloud"                           [shape=box, style="filled,bold", fillcolor="#d0e8ff", label="module.cloud", tooltip="main.tf:5-8"];
  "data.google_iam_policy.production_iam_policy" [tooltip="folder_production.tf:1-9"];
  "google_folder_iam_policy.production" [tooltip="folder_production.tf:35-41"];
  "import[cloud_migration.tf:6]" [tooltip="cloud_migration.tf:6-9"];
  "local.cloud" [tooltip="outputs.tf:17"];
  "module.prod-app-project" [tooltip="production_ui_app_project.tf:39-55"];

  "data.google_iam_policy.production_iam_policy" -> "module.cloud.output.vpc_terraform_service_account_email_by_id" [label="folder_production.tf:10"];
  "google_folder_iam_policy.production"    -> "data.google_iam_policy.production_iam_policy" [label="folder_production.tf:39"];
  "import[cloud_migration.tf:6]"           -> "module.cloud.module.external-project.google_project_iam_policy.this" [label="cloud_migration.tf:8"];
  "local.cloud"                            -> "module.cloud"                           [label="outputs.tf:17"];
  "module.prod-app-project"                -> "module.cloud.output.member_service_account_email_by_id_by_project" [label="production_ui_app_project.tf:42"];
}
```

**Reading the DOT graph:**
- Arrows point **from dependent → dependency** (bottom-to-top in rendered layout)
- The target node is highlighted in blue
- Node `tooltip` attributes show the source file and line range where the node is defined (visible on hover in SVG output)
- Edge labels show `file:line` of the reference expression
- `import[file.tf:N]`, `moved[file.tf:N]`, `removed[file.tf:N]` nodes are administrative blocks at source line N — they **must** be updated or removed when restructuring the target
- Edge targets show the exact node inside the module being referenced (e.g. `module.cloud.output.vpc_sa_email`), not just `module.cloud`

Render locally: `go run ./cmd/tfref/... . module.cloud | dot -Tsvg -o refs.svg && open refs.svg`

## Example: forward search

```bash
cd ~/.copilot/skills/tfref && go run ./cmd/tfref/... /path/to/workspace module.cloud --direction forward
```

Returns everything `module.cloud` itself depends on.

## Module resolution

- Relative module sources (`./modules/networking`) are resolved from the workspace directory
- Remote/registry modules are resolved via `.terraform/modules/modules.json` (populated by `terraform init` / `tofu init`)
- If a module cannot be resolved, the tool returns an **error** rather than silently returning incomplete results — the error message will tell you to run `terraform init` or `tofu init`

## When to use this skill

- *"What would change if I remove or move module.cloud?"* — use backward search; pay special attention to any `import[...]`, `moved[...]`, `removed[...]` nodes in the result
- *"Identify everything in this workspace that derives from module.cloud"*
- *"Find all places that reference var.region so I can update them"*
- *"Does module.baz depend on module.foo?"* — use forward search on module.baz, look for module.foo in results
- *"What does module.cloud depend on?"* — use forward search

## Limitations

- `for_each` instances are normalized: `module.foo["prod"].output` and `module.foo["staging"].output` both trace to the same `module.foo` node — instance-level distinction is not tracked
- `.tfvars` files are not parsed
- Registry modules require a local `.terraform/` cache from `terraform init` / `tofu init`
