package tfref_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/awalterschulze/gographviz"
	"github.com/retailnext/tfref"
)

// writeFile is a test helper that writes content to path, creating
// intermediate directories as needed.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// hasRef reports whether results contain a backward-ref entry whose From node
// matches the given module path and address.  Empty module path = root module.
func hasRef(results []tfref.BackwardResult, fromModule, fromAddr string) bool {
	for _, r := range results {
		if r.Ref.From.ModulePath == fromModule && r.Ref.From.Addr == fromAddr {
			return true
		}
	}
	return false
}

// ── Simple unit tests (inline workspaces) ────────────────────────────────────

// TestDirectRef verifies that a simple same-file resource reference is recorded.
func TestDirectRef(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.tf"), `
resource "terraform_data" "main" {}
resource "terraform_data" "public" {
  input = terraform_data.main.id
}
`)
	graph, err := tfref.ParseWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	results := tfref.DeepBackwardRefs(graph, tfref.NodeID{Addr: "terraform_data.main"})
	if !hasRef(results, "", "terraform_data.public") {
		t.Errorf("expected terraform_data.public to reference terraform_data.main; got: %v", results)
	}
}

// TestLocalGranularity verifies that individual locals are separate nodes.
// local.instance_id references web; local.unrelated_val does not.
// output.out must appear (via instance_id) but NOT via unrelated_val.
func TestLocalGranularity(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.tf"), `
resource "terraform_data" "web" {}
locals {
  instance_id   = terraform_data.web.id
  unrelated_val = "hello"
}
output "out" {
  value = local.instance_id
}
`)
	graph, err := tfref.ParseWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	results := tfref.DeepBackwardRefs(graph, tfref.NodeID{Addr: "terraform_data.web"})
	if !hasRef(results, "", "local.instance_id") {
		t.Error("expected local.instance_id -> aws_instance.web")
	}
	if !hasRef(results, "", "output.out") {
		t.Error("expected output.out to transitively reference aws_instance.web via local.instance_id")
	}
	if hasRef(results, "", "local.unrelated_val") {
		t.Error("local.unrelated_val must NOT appear — it does not reference aws_instance.web")
	}
}

// TestStringInterpolation verifies references inside "${...}" are captured.
func TestStringInterpolation(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.tf"), `
resource "terraform_data" "main" {}
locals {
  tag = "pfx-${terraform_data.main.id}-suffix"
}
`)
	graph, err := tfref.ParseWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	results := tfref.DeepBackwardRefs(graph, tfref.NodeID{Addr: "terraform_data.main"})
	if !hasRef(results, "", "local.tag") {
		t.Errorf("expected local.tag via string interpolation; got: %v", results)
	}
}

// TestTernaryRef verifies references in both branches of a ternary are captured.
func TestTernaryRef(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.tf"), `
variable "flag" {}
resource "terraform_data" "main" {}
locals {
  chosen = var.flag ? terraform_data.main.id : "fallback"
}
`)
	graph, err := tfref.ParseWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	results := tfref.DeepBackwardRefs(graph, tfref.NodeID{Addr: "terraform_data.main"})
	if !hasRef(results, "", "local.chosen") {
		t.Errorf("expected local.chosen via ternary; got: %v", results)
	}
}

// TestForExpressionRef verifies references inside for-expressions are captured.
func TestForExpressionRef(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.tf"), `
resource "terraform_data" "pub"  {}
resource "terraform_data" "priv" {}
locals {
  subnet_ids = [for s in [terraform_data.pub, terraform_data.priv] : s.id]
}
`)
	graph, err := tfref.ParseWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	results := tfref.DeepBackwardRefs(graph, tfref.NodeID{Addr: "terraform_data.pub"})
	if !hasRef(results, "", "local.subnet_ids") {
		t.Errorf("expected local.subnet_ids via for-expression; got: %v", results)
	}
}

// TestSplatRef verifies [*] splat expressions capture the referenced resource.
func TestSplatRef(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.tf"), `
resource "terraform_data" "web" {
  count = 2
  input = count.index
}
locals {
  all_ids = terraform_data.web[*].output
}
`)
	graph, err := tfref.ParseWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	results := tfref.DeepBackwardRefs(graph, tfref.NodeID{Addr: "terraform_data.web"})
	if !hasRef(results, "", "local.all_ids") {
		t.Errorf("expected local.all_ids via splat; got: %v", results)
	}
}

// TestBracketAccess verifies resource["name"] resolves identically to resource.name.
func TestBracketAccess(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.tf"), `
resource "terraform_data" "vpc" {
  for_each = { main = "10.0.0.0/16" }
  input    = each.value
}
locals {
  vpc_id = terraform_data.vpc["main"].output
}
`)
	graph, err := tfref.ParseWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	results := tfref.DeepBackwardRefs(graph, tfref.NodeID{Addr: "terraform_data.vpc"})
	if !hasRef(results, "", "local.vpc_id") {
		t.Errorf("expected local.vpc_id via bracket syntax; got: %v", results)
	}
}

// TestDependsOn verifies explicit depends_on lists create dependency edges.
func TestDependsOn(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.tf"), `
resource "terraform_data" "main" {}
resource "terraform_data" "gw" {
  depends_on = [terraform_data.main]
}
`)
	graph, err := tfref.ParseWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	results := tfref.DeepBackwardRefs(graph, tfref.NodeID{Addr: "terraform_data.main"})
	if !hasRef(results, "", "terraform_data.gw") {
		t.Errorf("expected terraform_data.gw depends_on terraform_data.main; got: %v", results)
	}
}

// TestCountRef verifies resource.name[count.index] and resource.name[0] both
// resolve to resource.name.
func TestCountRef(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.tf"), `
resource "terraform_data" "sn" {
  count = 3
  input = count.index
}
resource "terraform_data" "via_count" {
  count = 3
  input = terraform_data.sn[count.index].output
}
locals {
  first = terraform_data.sn[0].output
}
`)
	graph, err := tfref.ParseWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	results := tfref.DeepBackwardRefs(graph, tfref.NodeID{Addr: "terraform_data.sn"})
	if !hasRef(results, "", "local.first") {
		t.Error("expected local.first -> terraform_data.sn[0]")
	}
	if !hasRef(results, "", "terraform_data.via_count") {
		t.Error("expected terraform_data.via_count -> terraform_data.sn[count.index]")
	}
}

// TestPositionInfo verifies Ref.Subject carries a non-zero source range.
func TestPositionInfo(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.tf"), `
resource "terraform_data" "main" {}
resource "terraform_data" "public" {
  input = terraform_data.main.id
}
`)
	graph, err := tfref.ParseWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	results := tfref.DeepBackwardRefs(graph, tfref.NodeID{Addr: "terraform_data.main"})
	for _, r := range results {
		if r.Ref.From.Addr == "terraform_data.public" {
			if r.Ref.Subject.Start.Line == 0 {
				t.Error("expected non-zero line in Ref.Subject")
			}
			if r.Ref.Subject.Filename == "" {
				t.Error("expected non-empty filename in Ref.Subject")
			}
			return
		}
	}
	t.Error("ref from terraform_data.public not found")
}

// ── Cross-module tests ────────────────────────────────────────────────────────

// TestCrossModuleRef verifies output stitching across a module boundary:
// root: local.bar = module.child.result
// child: output.result = aws_instance.web.id
// → backward refs from aws_instance.web must include root::local.bar
func TestCrossModuleRef(t *testing.T) {
	dir := t.TempDir()
	childDir := filepath.Join(dir, "modules", "child")
	writeFile(t, filepath.Join(dir, "main.tf"), `
module "child" { source = "./modules/child" }
locals { bar = module.child.result }
`)
	writeFile(t, filepath.Join(childDir, "main.tf"), `
resource "terraform_data" "web" {}
output "result" { value = terraform_data.web.id }
`)
	graph, err := tfref.ParseWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	target := tfref.NodeID{ModulePath: "module.child", Addr: "terraform_data.web"}
	results := tfref.DeepBackwardRefs(graph, target)
	if !hasRef(results, "", "local.bar") {
		t.Errorf("expected root::local.bar transitively refs module.child::aws_instance.web; got: %v", results)
	}
}

// TestCrossModuleVarStitch verifies input variable stitching: when a caller
// passes X into var.Y of a child, resources using var.Y transitively depend on X.
func TestCrossModuleVarStitch(t *testing.T) {
	dir := t.TempDir()
	childDir := filepath.Join(dir, "modules", "child")
	writeFile(t, filepath.Join(dir, "main.tf"), `
resource "terraform_data" "main" {}
module "child" {
  source = "./modules/child"
  input  = terraform_data.main.id
}
`)
	writeFile(t, filepath.Join(childDir, "main.tf"), `
variable "input" {}
resource "terraform_data" "pub" { input = var.input }
output "subnet_id" { value = terraform_data.pub.id }
`)
	graph, err := tfref.ParseWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	results := tfref.DeepBackwardRefs(graph, tfref.NodeID{Addr: "terraform_data.main"})
	if !hasRef(results, "module.child", "terraform_data.pub") {
		t.Errorf("expected module.child::terraform_data.pub transitively depends on root::terraform_data.main; got: %v", results)
	}
}

// ── for_each / count module tests ─────────────────────────────────────────────

// TestForEachModuleUnknownKey verifies module.foo[each.key].output resolves
// to module.foo.output (unknown key stripped as instance accessor).
func TestForEachModuleUnknownKey(t *testing.T) {
	dir := t.TempDir()
	childDir := filepath.Join(dir, "modules", "child")
	writeFile(t, filepath.Join(dir, "main.tf"), `
variable "envs" {}
module "child" {
  source   = "./modules/child"
  for_each = var.envs
  name     = each.key
}
locals {
  results = [for k, v in module.child : v.result]
}
`)
	writeFile(t, filepath.Join(childDir, "main.tf"), `
variable "name" {}
resource "terraform_data" "b" { input = var.name }
output "result" { value = terraform_data.b.id }
`)
	graph, err := tfref.ParseWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	target := tfref.NodeID{ModulePath: "module.child", Addr: "terraform_data.b"}
	results := tfref.DeepBackwardRefs(graph, target)
	if !hasRef(results, "", "local.results") {
		t.Errorf("expected root::local.results via for_each module (unknown key); got: %v", results)
	}
}

// TestForEachModuleStringKey guards against the bug where module.foo["prod"].output
// would incorrectly resolve to module.foo.prod instead of module.foo.output.
func TestForEachModuleStringKey(t *testing.T) {
	dir := t.TempDir()
	childDir := filepath.Join(dir, "modules", "child")
	writeFile(t, filepath.Join(dir, "main.tf"), `
module "child" {
  source   = "./modules/child"
  for_each = { prod = "prod", dev = "dev" }
  name     = each.key
}
locals {
  prod_result = module.child["prod"].result
}
`)
	writeFile(t, filepath.Join(childDir, "main.tf"), `
variable "name" {}
resource "terraform_data" "b" { input = var.name }
output "result" { value = terraform_data.b.id }
`)
	graph, err := tfref.ParseWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	target := tfref.NodeID{ModulePath: "module.child", Addr: "terraform_data.b"}
	results := tfref.DeepBackwardRefs(graph, target)
	if !hasRef(results, "", "local.prod_result") {
		t.Errorf("expected root::local.prod_result via string for_each key; got: %v", results)
	}
}

// TestCountModuleRef verifies module.foo[0].output (numeric count key)
// resolves to module.foo.output.
func TestCountModuleRef(t *testing.T) {
	dir := t.TempDir()
	childDir := filepath.Join(dir, "modules", "child")
	writeFile(t, filepath.Join(dir, "main.tf"), `
module "child" {
  source = "./modules/child"
  count  = 3
  index  = count.index
}
locals {
  first = module.child[0].result
}
`)
	writeFile(t, filepath.Join(childDir, "main.tf"), `
variable "index" {}
resource "terraform_data" "b" { input = "bucket-${var.index}" }
output "result" { value = terraform_data.b.id }
`)
	graph, err := tfref.ParseWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	target := tfref.NodeID{ModulePath: "module.child", Addr: "terraform_data.b"}
	results := tfref.DeepBackwardRefs(graph, target)
	if !hasRef(results, "", "local.first") {
		t.Errorf("expected root::local.first via count module index; got: %v", results)
	}
}

// ── Remote module cache tests ─────────────────────────────────────────────────

// TestRemoteModuleFromCache verifies that a module resolved via
// .terraform/modules/modules.json is parsed and stitched correctly.
// This simulates a registry module that has been downloaded by terraform init.
func TestRemoteModuleFromCache(t *testing.T) {
	dir := t.TempDir()

	// Simulate downloaded module at .terraform/modules/vpc
	cachedDir := filepath.Join(dir, ".terraform", "modules", "vpc")
	writeFile(t, filepath.Join(cachedDir, "main.tf"), `
variable "cidr" {}
resource "terraform_data" "this" { input = var.cidr }
output "vpc_id" { value = terraform_data.this.id }
`)

	// Write the modules manifest pointing to the cached directory
	manifest := map[string]interface{}{
		"Modules": []map[string]interface{}{
			{
				"Key":    "vpc",
				"Source": "registry.terraform.io/hashicorp/vpc/aws",
				"Dir":    ".terraform/modules/vpc",
			},
		},
	}
	manifestJSON, _ := json.Marshal(manifest)
	writeFile(t, filepath.Join(dir, ".terraform", "modules", "modules.json"), string(manifestJSON))

	writeFile(t, filepath.Join(dir, "main.tf"), `
module "vpc" {
  source  = "registry.terraform.io/hashicorp/vpc/aws"
  version = "1.0.0"
  input   = "10.0.0.0/16"
}
locals {
  vpc_id = module.vpc.vpc_id
}
`)

	graph, err := tfref.ParseWorkspace(dir)
	if err != nil {
		t.Fatalf("ParseWorkspace: %v", err)
	}

	// Backward refs from the remote module's resource should reach root::local.vpc_id
	target := tfref.NodeID{ModulePath: "module.vpc", Addr: "terraform_data.this"}
	results := tfref.DeepBackwardRefs(graph, target)
	if !hasRef(results, "", "local.vpc_id") {
		t.Errorf("expected root::local.vpc_id to reference module.vpc::aws_vpc.this via remote module cache; got: %v", results)
	}
}

// TestUnresolvableModuleIsError verifies that a module whose source cannot be
// resolved (not a relative path, not in the manifest) returns an explicit
// error rather than silently producing an incomplete result.
func TestUnresolvableModuleIsError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.tf"), `
module "unresolvable" {
  source  = "registry.terraform.io/some/module/aws"
  version = "1.0.0"
}
locals {
  val = module.unresolvable.output
}
`)
	// No .terraform/modules/modules.json present — module cannot be resolved.
	_, err := tfref.ParseWorkspace(dir)
	if err == nil {
		t.Fatal("expected an error when a non-local module source cannot be resolved, got nil")
	}
}

// TestUnresolvableRelativePathIsError verifies that a module with a relative
// source path pointing to a non-existent directory returns an explicit error.
func TestUnresolvableRelativePathIsError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.tf"), `
module "missing" {
  source = "./does-not-exist"
}
locals {
  val = module.missing.output
}
`)
	_, err := tfref.ParseWorkspace(dir)
	if err == nil {
		t.Fatal("expected an error for relative source path pointing to non-existent directory, got nil")
	}
}

// ── testdata fixture tests ────────────────────────────────────────────────────

// TestTofuWorkspace verifies that .tofu files are parsed correctly.
func TestTofuWorkspace(t *testing.T) {
	graph, err := tfref.ParseWorkspace("testdata/workspace-tofu")
	if err != nil {
		t.Fatalf("ParseWorkspace: %v", err)
	}
	results := tfref.DeepBackwardRefs(graph, tfref.NodeID{Addr: "random_string.main"})
	if len(results) == 0 {
		t.Error("expected non-empty backward refs from random_string.main in .tofu workspace")
	}
}

// TestMixedWorkspace verifies workspaces containing both .tf and .tofu files
// are parsed correctly and references across the mixed files are resolved.
func TestMixedWorkspace(t *testing.T) {
	graph, err := tfref.ParseWorkspace("testdata/workspace-mixed")
	if err != nil {
		t.Fatalf("ParseWorkspace: %v", err)
	}
	// local.combined is in .tf and references outputs from both .tf and .tofu child modules
	results := tfref.DeepBackwardRefs(graph, tfref.NodeID{Addr: "random_string.main"})
	if !hasRef(results, "", "local.combined") {
		t.Errorf("expected local.combined to transitively reference random_string.main; got: %v", results)
	}
	if !hasRef(results, "", "module.tofu_child") {
		t.Errorf("expected module.tofu_child to reference random_string.main; got: %v", results)
	}
}

// TestModuleBoundaryPrecision is the baz/zee scenario:
//
//foo produces output.result
//bar.derived_output depends on foo (via var.foo_result)
//bar.independent_output does NOT depend on foo
//baz consumes bar.independent_output → must NOT appear in graph from foo
//zee consumes bar.derived_output     → MUST appear in graph from foo
func TestModuleBoundaryPrecision(t *testing.T) {
	graph, err := tfref.ParseWorkspace("testdata/workspace-precision")
	if err != nil {
		t.Fatalf("ParseWorkspace: %v", err)
	}

	target := tfref.NodeID{ModulePath: "module.foo", Addr: "random_id.data"}
	results := tfref.DeepBackwardRefs(graph, target)

	if !hasRef(results, "", "module.zee") {
		t.Errorf("MUST: module.zee should appear (depends on bar.derived_output which depends on foo); got: %v", results)
	}
	if hasRef(results, "", "module.baz") {
		t.Error("MUST NOT: module.baz must not appear (only uses bar.independent_output)")
	}
}

// TestForEachModulePrecision verifies that module.buckets["prod"].result in the
// precision workspace resolves to module.buckets.result, not module.buckets.prod.
func TestForEachModulePrecision(t *testing.T) {
	graph, err := tfref.ParseWorkspace("testdata/workspace-precision")
	if err != nil {
		t.Fatalf("ParseWorkspace: %v", err)
	}
	target := tfref.NodeID{ModulePath: "module.buckets", Addr: "random_id.data"}
	results := tfref.DeepBackwardRefs(graph, target)
	if !hasRef(results, "", "local.prod_result") {
		t.Errorf("expected local.prod_result via module.buckets[string key]; got: %v", results)
	}
}

// TestModuleOutputRefSearch verifies that when searching for a module call
// (e.g. "module.cloud"), callers that reference specific outputs of that module
// (e.g. module.cloud.some_output["key"] inside a string interpolation) are
// included in the results.  This exercises the seedNodes expansion added to
// DeepBackwardRefs which seeds from child output nodes in addition to the bare
// module call node.
//
// It also verifies that a node which references multiple outputs of the target
// module appears exactly once in the results (no duplicates).
func TestModuleOutputRefSearch(t *testing.T) {
	dir := t.TempDir()
	childDir := filepath.Join(dir, "modules", "cloud")
	writeFile(t, filepath.Join(dir, "main.tf"), `
module "cloud" {
  source = "./modules/cloud"
}

# References module.cloud.vpc_sa_by_id via string interpolation + bracket access.
data "google_iam_policy" "prod" {
  members = [
    "serviceAccount:${module.cloud.vpc_sa_by_id["admin"]}",
  ]
}

# References two different outputs of module.cloud.
# Should appear exactly once in results despite having two edges to the module.
locals {
  combined = "${module.cloud.output_a}-${module.cloud.output_b}"
}
`)
	writeFile(t, filepath.Join(childDir, "main.tf"), `
resource "terraform_data" "this" {}

output "vpc_sa_by_id" {
  value = {}
}
output "output_a" {
  value = terraform_data.this.id
}
output "output_b" {
  value = terraform_data.this.id
}
`)
	graph, err := tfref.ParseWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}

	target := tfref.NodeID{Addr: "module.cloud"}
	results := tfref.DeepBackwardRefs(graph, target)

	// data.google_iam_policy.prod must appear: it references module.cloud.vpc_sa_by_id
	// inside a string interpolation with bracket access.
	if !hasRef(results, "", "data.google_iam_policy.prod") {
		t.Errorf("expected data.google_iam_policy.prod to appear (references module.cloud output in string interpolation); got: %v", results)
	}

	// local.combined must appear: it references module.cloud.output_a and .output_b.
	if !hasRef(results, "", "local.combined") {
		t.Errorf("expected local.combined to appear (references two outputs of module.cloud); got: %v", results)
	}

	// local.combined should appear exactly once even though it references two outputs.
	count := 0
	for _, r := range results {
		if r.Ref.From == (tfref.NodeID{Addr: "local.combined"}) {
			count++
		}
	}
	if count != 1 {
		t.Errorf("local.combined should appear exactly once in results; got %d times", count)
	}
}

// ── import / moved / removed block tests ─────────────────────────────────────

// TestImportMovedRemovedBlocks verifies that import, moved, and removed blocks
// that reference a module's resources appear in the backward-ref results for
// that module.  These blocks use address literals (traversal expressions) in
// their to/from attributes, so they must be walked by the parser and their
// opaque intermediate refs must be included in the BFS seed set.
func TestImportMovedRemovedBlocks(t *testing.T) {
	dir := t.TempDir()
	childDir := filepath.Join(dir, "modules", "cloud")
	writeFile(t, filepath.Join(dir, "main.tf"), `
module "cloud" {
  source = "./modules/cloud"
}

# import block referencing a resource inside module.cloud
import {
  to = module.cloud.terraform_data.main
  id = "my-bucket-id"
}

# moved block with from = resource inside module.cloud
moved {
  from = module.cloud.terraform_data.legacy
  to   = module.cloud.terraform_data.main
}

# removed block referencing a resource inside module.cloud
removed {
  from = module.cloud.terraform_data.old
  lifecycle {
    destroy = false
  }
}
`)
	writeFile(t, filepath.Join(childDir, "main.tf"), `
resource "terraform_data" "main" {}
output "result" { value = terraform_data.main.id }
`)
	graph, err := tfref.ParseWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}

	target := tfref.NodeID{Addr: "module.cloud"}
	results := tfref.DeepBackwardRefs(graph, target)

	importFound, movedFound, removedFound := false, false, false
	for _, r := range results {
		switch {
		case strings.HasPrefix(r.Ref.From.Addr, "import["):
			importFound = true
			// Edge target must be the actual resource node, not a synthetic output node.
			if r.Ref.To.ModulePath != "module.cloud" || r.Ref.To.Addr != "terraform_data.main" {
				t.Errorf("import block edge target should be NodeID{module.cloud, terraform_data.main}, got %+v", r.Ref.To)
			}
			// Node ID must include a filename (bracket syntax).
			if !strings.Contains(r.Ref.From.Addr, ".tf:") {
				t.Errorf("import node ID should contain filename, got %q", r.Ref.From.Addr)
			}
		case strings.HasPrefix(r.Ref.From.Addr, "moved["):
			movedFound = true
		case strings.HasPrefix(r.Ref.From.Addr, "removed["):
			removedFound = true
			if r.Ref.To.ModulePath != "module.cloud" || r.Ref.To.Addr != "terraform_data.old" {
				t.Errorf("removed block edge target should be NodeID{module.cloud, terraform_data.old}, got %+v", r.Ref.To)
			}
		}
	}
	if !importFound {
		t.Errorf("import block should appear in results; got: %v", results)
	}
	if !movedFound {
		t.Errorf("moved block should appear in results; got: %v", results)
	}
	if !removedFound {
		t.Errorf("removed block should appear in results; got: %v", results)
	}
}

// TestMovedBlockNonModule verifies that moved blocks referencing root-module
// resources (not modules) appear in the backward-ref results for that resource.
func TestMovedBlockNonModule(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.tf"), `
resource "terraform_data" "old" {}
resource "terraform_data" "new" {}

moved {
  from = terraform_data.old
  to   = terraform_data.new
}
`)
	graph, err := tfref.ParseWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}

	target := tfref.NodeID{Addr: "terraform_data.old"}
	results := tfref.DeepBackwardRefs(graph, target)

	found := false
	for _, r := range results {
		if strings.HasPrefix(r.Ref.From.Addr, "moved[") {
			found = true
		}
	}
	if !found {
		t.Errorf("moved block should appear in results for terraform_data.old; got: %v", results)
	}
}

// ── DOT output validation ─────────────────────────────────────────────────────

// assertValidDOT parses dot and fails the test if gographviz reports any error.
func assertValidDOT(t *testing.T, dot string) {
	t.Helper()
	// Strip leading comment lines (// ...) which are not DOT syntax but are
	// emitted by FormatDOT as a human-readable header.  gographviz does not
	// accept them.
	var lines []string
	for _, line := range strings.Split(dot, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		lines = append(lines, line)
	}
	cleaned := strings.Join(lines, "\n")

	ast, err := gographviz.ParseString(cleaned)
	if err != nil {
		t.Fatalf("FormatDOT produced invalid DOT: %v\noutput:\n%s", err, dot)
	}
	g := gographviz.NewGraph()
	if err := gographviz.Analyse(ast, g); err != nil {
		t.Fatalf("FormatDOT DOT graph analysis failed: %v\noutput:\n%s", err, dot)
	}
}

// captureFormatDOT calls FormatDOT and returns the output as a string.
func captureFormatDOT(workspace, targetStr, direction string, results []tfref.BackwardResult) string {
	var buf bytes.Buffer
	tfref.FormatDOT(&buf, workspace, targetStr, direction, results)
	return buf.String()
}

// TestFormatDOTBackward checks that a backward search with results produces
// valid DOT output.
func TestFormatDOTBackward(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.tf"), `
resource "terraform_data" "web" {}
resource "terraform_data" "ip" {
  input = terraform_data.web.id
}
`)
	graph, err := tfref.ParseWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	target := tfref.NodeID{Addr: "terraform_data.web"}
	results := tfref.DeepBackwardRefs(graph, target)
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	dot := captureFormatDOT(dir, "terraform_data.web", "backward", results)
	assertValidDOT(t, dot)
}

// TestFormatDOTForward checks that a forward search produces valid DOT output.
func TestFormatDOTForward(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.tf"), `
resource "terraform_data" "web" {}
resource "terraform_data" "ip" {
  input = terraform_data.web.id
}
`)
	graph, err := tfref.ParseWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	target := tfref.NodeID{Addr: "terraform_data.ip"}
	results := tfref.DeepForwardRefs(graph, target)
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	dot := captureFormatDOT(dir, "terraform_data.ip", "forward", results)
	assertValidDOT(t, dot)
}

// TestFormatDOTEmpty checks that an empty result set still produces valid DOT.
func TestFormatDOTEmpty(t *testing.T) {
	dot := captureFormatDOT("", "module.nonexistent", "backward", nil)
	assertValidDOT(t, dot)
}

// TestFormatDOTCrossModule checks that a cross-module backward search
// (output stitching, var stitching) produces valid DOT output.
func TestFormatDOTCrossModule(t *testing.T) {
	graph, err := tfref.ParseWorkspace("testdata/workspace-tf")
	if err != nil {
		t.Fatal(err)
	}
	target := tfref.ParseFullAddr("module.child_a")
	results := tfref.DeepBackwardRefs(graph, target)
	dot := captureFormatDOT("testdata/workspace-tf", tfref.FullAddr(target), "backward", results)
	assertValidDOT(t, dot)
}

// TestFormatDOTImportBlocks checks that a workspace containing import/moved/
// removed blocks produces valid DOT output (node IDs contain brackets and
// colons which must be properly quoted).
func TestFormatDOTImportBlocks(t *testing.T) {
	dir := t.TempDir()
	childDir := filepath.Join(dir, "modules", "cloud")
	writeFile(t, filepath.Join(dir, "main.tf"), `
module "cloud" { source = "./modules/cloud" }
import {
  to = module.cloud.terraform_data.main
  id = "bucket-id"
}
moved {
  from = module.cloud.terraform_data.legacy
  to   = module.cloud.terraform_data.main
}
`)
	writeFile(t, filepath.Join(childDir, "main.tf"), `
resource "terraform_data" "main" {}
resource "terraform_data" "legacy" {}
`)
	graph, err := tfref.ParseWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	target := tfref.NodeID{Addr: "module.cloud"}
	results := tfref.DeepBackwardRefs(graph, target)
	dot := captureFormatDOT(dir, "module.cloud", "backward", results)
	assertValidDOT(t, dot)
}

// TestNodeExists verifies the target existence check for various scenarios.
func TestNodeExists(t *testing.T) {
	dir := t.TempDir()
	childDir := filepath.Join(dir, "modules", "cloud")
	writeFile(t, filepath.Join(dir, "main.tf"), `
module "cloud" { source = "./modules/cloud" }
resource "terraform_data" "present" {}

# removed block makes terraform_data.legacy count as existing even though
# there is no resource block for it.
removed {
  from = terraform_data.legacy
  lifecycle { destroy = false }
}

# import block makes module.cloud.terraform_data.archived count as existing.
import {
  to = module.cloud.terraform_data.archived
  id = "archived-bucket"
}
`)
	writeFile(t, filepath.Join(childDir, "main.tf"), `
resource "terraform_data" "archived" {}
`)
	graph, err := tfref.ParseWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		addr   string
		exists bool
	}{
		{"module.cloud", true},                            // module block present
		{"terraform_data.present", true},                  // resource block present
		{"terraform_data.legacy", true},                   // only a removed block, still counts
		{"module.cloud.terraform_data.archived", true},    // defined in child + import block
		{"terraform_data.typo", false},                    // does not exist
		{"module.nonexistent", false},                     // no module block
		{"local.doesnotexist", false},                     // no locals attr
	}
	for _, tc := range cases {
		target := tfref.ParseFullAddr(tc.addr)
		got := tfref.NodeExists(graph, target)
		if got != tc.exists {
			t.Errorf("NodeExists(%q) = %v, want %v", tc.addr, got, tc.exists)
		}
	}
}
