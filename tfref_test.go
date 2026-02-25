package tfref_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eriksw/tfref"
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
resource "aws_vpc" "main" {}
resource "aws_subnet" "public" {
  vpc_id = aws_vpc.main.id
}
`)
	graph, err := tfref.ParseWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	results := tfref.DeepBackwardRefs(graph, tfref.NodeID{Addr: "aws_vpc.main"})
	if !hasRef(results, "", "aws_subnet.public") {
		t.Errorf("expected aws_subnet.public to reference aws_vpc.main; got: %v", results)
	}
}

// TestLocalGranularity verifies that individual locals are separate nodes.
// local.instance_id references web; local.unrelated_val does not.
// output.out must appear (via instance_id) but NOT via unrelated_val.
func TestLocalGranularity(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.tf"), `
resource "aws_instance" "web" {}
locals {
  instance_id   = aws_instance.web.id
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
	results := tfref.DeepBackwardRefs(graph, tfref.NodeID{Addr: "aws_instance.web"})
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
resource "aws_vpc" "main" { cidr_block = "10.0.0.0/16" }
locals {
  tag = "vpc-${aws_vpc.main.id}-suffix"
}
`)
	graph, err := tfref.ParseWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	results := tfref.DeepBackwardRefs(graph, tfref.NodeID{Addr: "aws_vpc.main"})
	if !hasRef(results, "", "local.tag") {
		t.Errorf("expected local.tag via string interpolation; got: %v", results)
	}
}

// TestTernaryRef verifies references in both branches of a ternary are captured.
func TestTernaryRef(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.tf"), `
variable "flag" {}
resource "aws_vpc" "main" { cidr_block = "10.0.0.0/16" }
locals {
  chosen = var.flag ? aws_vpc.main.id : "fallback"
}
`)
	graph, err := tfref.ParseWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	results := tfref.DeepBackwardRefs(graph, tfref.NodeID{Addr: "aws_vpc.main"})
	if !hasRef(results, "", "local.chosen") {
		t.Errorf("expected local.chosen via ternary; got: %v", results)
	}
}

// TestForExpressionRef verifies references inside for-expressions are captured.
func TestForExpressionRef(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.tf"), `
resource "aws_subnet" "pub"  { cidr_block = "10.0.1.0/24" }
resource "aws_subnet" "priv" { cidr_block = "10.0.2.0/24" }
locals {
  subnet_ids = [for s in [aws_subnet.pub, aws_subnet.priv] : s.id]
}
`)
	graph, err := tfref.ParseWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	results := tfref.DeepBackwardRefs(graph, tfref.NodeID{Addr: "aws_subnet.pub"})
	if !hasRef(results, "", "local.subnet_ids") {
		t.Errorf("expected local.subnet_ids via for-expression; got: %v", results)
	}
}

// TestSplatRef verifies [*] splat expressions capture the referenced resource.
func TestSplatRef(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.tf"), `
resource "aws_instance" "web" {
  count = 2
  ami   = "ami-0"
}
locals {
  all_ids = aws_instance.web[*].id
}
`)
	graph, err := tfref.ParseWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	results := tfref.DeepBackwardRefs(graph, tfref.NodeID{Addr: "aws_instance.web"})
	if !hasRef(results, "", "local.all_ids") {
		t.Errorf("expected local.all_ids via splat; got: %v", results)
	}
}

// TestBracketAccess verifies resource["name"] resolves identically to resource.name.
func TestBracketAccess(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.tf"), `
resource "aws_vpc" "main" {}
locals {
  vpc_id = aws_vpc["main"].id
}
`)
	graph, err := tfref.ParseWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	results := tfref.DeepBackwardRefs(graph, tfref.NodeID{Addr: "aws_vpc.main"})
	if !hasRef(results, "", "local.vpc_id") {
		t.Errorf("expected local.vpc_id via bracket syntax; got: %v", results)
	}
}

// TestDependsOn verifies explicit depends_on lists create dependency edges.
func TestDependsOn(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.tf"), `
resource "aws_vpc" "main" {}
resource "aws_internet_gateway" "gw" {
  depends_on = [aws_vpc.main]
}
`)
	graph, err := tfref.ParseWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	results := tfref.DeepBackwardRefs(graph, tfref.NodeID{Addr: "aws_vpc.main"})
	if !hasRef(results, "", "aws_internet_gateway.gw") {
		t.Errorf("expected aws_internet_gateway.gw depends_on aws_vpc.main; got: %v", results)
	}
}

// TestCountRef verifies resource.name[count.index] and resource.name[0] both
// resolve to resource.name.
func TestCountRef(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.tf"), `
resource "aws_subnet" "sn" {
  count      = 3
  cidr_block = "10.0.0.0/24"
}
locals {
  first     = aws_subnet.sn[0].id
  via_count = aws_subnet.sn[count.index].id
}
`)
	graph, err := tfref.ParseWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	results := tfref.DeepBackwardRefs(graph, tfref.NodeID{Addr: "aws_subnet.sn"})
	if !hasRef(results, "", "local.first") {
		t.Error("expected local.first -> aws_subnet.sn[0]")
	}
	if !hasRef(results, "", "local.via_count") {
		t.Error("expected local.via_count -> aws_subnet.sn[count.index]")
	}
}

// TestPositionInfo verifies Ref.Subject carries a non-zero source range.
func TestPositionInfo(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.tf"), `
resource "aws_vpc" "main" {}
resource "aws_subnet" "public" {
  vpc_id = aws_vpc.main.id
}
`)
	graph, err := tfref.ParseWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	results := tfref.DeepBackwardRefs(graph, tfref.NodeID{Addr: "aws_vpc.main"})
	for _, r := range results {
		if r.Ref.From.Addr == "aws_subnet.public" {
			if r.Ref.Subject.Start.Line == 0 {
				t.Error("expected non-zero line in Ref.Subject")
			}
			if r.Ref.Subject.Filename == "" {
				t.Error("expected non-empty filename in Ref.Subject")
			}
			return
		}
	}
	t.Error("ref from aws_subnet.public not found")
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
resource "aws_instance" "web" {}
output "result" { value = aws_instance.web.id }
`)
	graph, err := tfref.ParseWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	target := tfref.NodeID{ModulePath: "module.child", Addr: "aws_instance.web"}
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
resource "aws_vpc" "main" {}
module "child" {
  source = "./modules/child"
  vpc_id = aws_vpc.main.id
}
`)
	writeFile(t, filepath.Join(childDir, "main.tf"), `
variable "vpc_id" {}
resource "aws_subnet" "pub" { vpc_id = var.vpc_id }
output "subnet_id" { value = aws_subnet.pub.id }
`)
	graph, err := tfref.ParseWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	results := tfref.DeepBackwardRefs(graph, tfref.NodeID{Addr: "aws_vpc.main"})
	if !hasRef(results, "module.child", "aws_subnet.pub") {
		t.Errorf("expected module.child::aws_subnet.pub transitively depends on root::aws_vpc.main; got: %v", results)
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
resource "aws_s3_bucket" "b" { bucket = var.name }
output "result" { value = aws_s3_bucket.b.id }
`)
	graph, err := tfref.ParseWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	target := tfref.NodeID{ModulePath: "module.child", Addr: "aws_s3_bucket.b"}
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
resource "aws_s3_bucket" "b" { bucket = var.name }
output "result" { value = aws_s3_bucket.b.id }
`)
	graph, err := tfref.ParseWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	target := tfref.NodeID{ModulePath: "module.child", Addr: "aws_s3_bucket.b"}
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
resource "aws_s3_bucket" "b" { bucket = "bucket-${var.index}" }
output "result" { value = aws_s3_bucket.b.id }
`)
	graph, err := tfref.ParseWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	target := tfref.NodeID{ModulePath: "module.child", Addr: "aws_s3_bucket.b"}
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
resource "aws_vpc" "this" { cidr_block = var.cidr }
output "vpc_id" { value = aws_vpc.this.id }
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
  cidr    = "10.0.0.0/16"
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
	target := tfref.NodeID{ModulePath: "module.vpc", Addr: "aws_vpc.this"}
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
	results := tfref.DeepBackwardRefs(graph, tfref.NodeID{Addr: "aws_vpc.main"})
	if len(results) == 0 {
		t.Error("expected non-empty backward refs from aws_vpc.main in .tofu workspace")
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
	results := tfref.DeepBackwardRefs(graph, tfref.NodeID{Addr: "aws_vpc.main"})
	if !hasRef(results, "", "local.combined") {
		t.Errorf("expected local.combined to transitively reference aws_vpc.main; got: %v", results)
	}
	if !hasRef(results, "", "module.tofu_child") {
		t.Errorf("expected module.tofu_child to reference aws_vpc.main; got: %v", results)
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

	target := tfref.NodeID{ModulePath: "module.foo", Addr: "aws_s3_bucket.data"}
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
	target := tfref.NodeID{ModulePath: "module.buckets", Addr: "aws_s3_bucket.data"}
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
resource "aws_vpc" "this" {}

output "vpc_sa_by_id" {
  value = {}
}
output "output_a" {
  value = aws_vpc.this.id
}
output "output_b" {
  value = aws_vpc.this.arn
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
  to = module.cloud.aws_s3_bucket.main
  id = "my-bucket-id"
}

# moved block with from = resource inside module.cloud
moved {
  from = module.cloud.aws_s3_bucket.legacy
  to   = module.cloud.aws_s3_bucket.main
}

# removed block referencing a resource inside module.cloud
removed {
  from = module.cloud.aws_s3_bucket.old
  lifecycle {
    destroy = false
  }
}
`)
	writeFile(t, filepath.Join(childDir, "main.tf"), `
resource "aws_s3_bucket" "main" {}
output "result" { value = aws_s3_bucket.main.id }
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
		case strings.HasPrefix(r.Ref.From.Addr, "import."):
			importFound = true
		case strings.HasPrefix(r.Ref.From.Addr, "moved."):
			movedFound = true
		case strings.HasPrefix(r.Ref.From.Addr, "removed."):
			removedFound = true
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
resource "aws_s3_bucket" "old" {}
resource "aws_s3_bucket" "new" {}

moved {
  from = aws_s3_bucket.old
  to   = aws_s3_bucket.new
}
`)
	graph, err := tfref.ParseWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}

	target := tfref.NodeID{Addr: "aws_s3_bucket.old"}
	results := tfref.DeepBackwardRefs(graph, target)

	found := false
	for _, r := range results {
		if strings.HasPrefix(r.Ref.From.Addr, "moved.") {
			found = true
		}
	}
	if !found {
		t.Errorf("moved block should appear in results for aws_s3_bucket.old; got: %v", results)
	}
}
