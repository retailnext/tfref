// Command tfref traces backward (and forward) references across a
// Terraform / OpenTofu workspace.
//
// Usage:
//
//	tfref [WORKSPACE] TARGET [flags]
//
// Examples:
//
//	tfref . module.cloud
//	tfref . module.cloud --format json
//	tfref . module.cloud --direction forward
//	tfref . module.foo.aws_s3_bucket.mybucket
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eriksw/tfref"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: tfref [WORKSPACE] TARGET [flags]")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "  WORKSPACE  path to workspace root (default: current directory)")
		fmt.Fprintln(os.Stderr, "  TARGET     full Terraform address, e.g.:")
		fmt.Fprintln(os.Stderr, "               module.cloud")
		fmt.Fprintln(os.Stderr, "               module.foo.aws_s3_bucket.mybucket")
		fmt.Fprintln(os.Stderr, "               local.result")
		fmt.Fprintln(os.Stderr, "               module.foo.output.result")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "flags:")
		flag.PrintDefaults()
	}

	format := flag.String("format", "dot", "output format: dot (default, renderable with graphviz), json")
	direction := flag.String("direction", "backward", "traversal direction: backward (who depends on target) or forward (what does target depend on)")
	showInternals := flag.Bool("show-internals", false, "include nodes internal to the target module in output")

	// Pre-process args so flags may appear anywhere (before or after positional args).
	os.Args = reorderArgs(os.Args)
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		flag.Usage()
		os.Exit(1)
	}

	var workspaceDir, targetStr string
	if len(args) == 1 {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error getting cwd: %v\n", err)
			os.Exit(1)
		}
		workspaceDir = wd
		targetStr = args[0]
	} else {
		workspaceDir = args[0]
		targetStr = args[1]
	}

	absWorkspace, err := filepath.Abs(workspaceDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error resolving workspace path: %v\n", err)
		os.Exit(1)
	}

	target := tfref.ParseFullAddr(targetStr)
	if target.Addr == "" {
		fmt.Fprintf(os.Stderr, "error: could not parse target address %q\n", targetStr)
		os.Exit(1)
	}

	graph, err := tfref.ParseWorkspace(absWorkspace)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error parsing workspace: %v\n", err)
		os.Exit(1)
	}

	var results []tfref.BackwardResult
	switch *direction {
	case "forward":
		results = tfref.DeepForwardRefs(graph, target)
	default:
		results = tfref.DeepBackwardRefs(graph, target)
	}

	// Filter internal nodes unless --show-internals is set.
	if !*showInternals && target.ModulePath != "" {
		prefix := target.ModulePath
		filtered := results[:0]
		for _, r := range results {
			mp := r.Ref.From.ModulePath
			if mp == prefix || strings.HasPrefix(mp, prefix+"/") {
				continue
			}
			filtered = append(filtered, r)
		}
		results = filtered
	}

	switch *format {
	case "json":
		printJSON(absWorkspace, targetStr, *direction, results)
	default:
		printDOT(absWorkspace, targetStr, *direction, results)
	}
}

// relPath returns the workspace-relative form of an absolute path.
func relPath(base, abs string) string {
	rel, err := filepath.Rel(base, abs)
	if err != nil {
		return abs
	}
	return rel
}

// fullAddr converts a NodeID to the canonical full Terraform address string.
func fullAddr(n tfref.NodeID) string {
	if n.ModulePath == "" {
		return n.Addr
	}
	parts := strings.Split(n.ModulePath, "/")
	return strings.Join(parts, ".") + "." + n.Addr
}

// dotID returns a DOT-safe quoted node identifier.
func dotID(s string) string {
	// Wrap in double-quotes; escape any embedded double-quotes.
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

// printDOT emits a Graphviz DOT digraph.
//
// Nodes are full Terraform addresses.  Edge labels show the source file and
// line where the reference expression appears.  The target node is styled with
// a double border so it is easy to locate in a rendered graph.
//
// Render with: dot -Tsvg out.dot > out.svg
// Or paste into https://dreampuf.github.io/GraphvizOnline/
func printDOT(workspace, targetStr, direction string, results []tfref.BackwardResult) {
	// Comment header with context.
	fmt.Printf("// tfref %s: %s\n", direction, targetStr)
	fmt.Printf("// workspace: %s\n", workspace)
	fmt.Printf("// %d node(s) found\n", len(results))
	if len(results) == 0 {
		fmt.Printf("// (nothing found — the target may not exist or have no %s)\n", direction+" references")
	}
	fmt.Println()
	fmt.Println("digraph tfref {")
	fmt.Println("  rankdir=BT;  // bottom-to-top: dependents above, dependencies below")
	fmt.Println("  node [fontname=\"Helvetica\", fontsize=10];")
	fmt.Println("  edge [fontname=\"Helvetica\", fontsize=9, color=\"#555555\"];")
	fmt.Println()

	// Target node — double-bordered box so it stands out.
	fmt.Printf("  %-40s [shape=box, style=\"filled,bold\", fillcolor=\"#d0e8ff\", label=%s];\n",
		dotID(targetStr), dotID(targetStr))

	// Collect all unique node addresses.
	type edge struct{ from, to, file string; line int }
	var edges []edge
	seenNodes := map[string]bool{targetStr: true}

	for _, r := range results {
		fa := fullAddr(r.Ref.From)
		seenNodes[fa] = true
		rng := r.Ref.Subject
		edges = append(edges, edge{
			from: fa,
			to:   fullAddr(r.Ref.To),
			file: relPath(workspace, rng.Filename),
			line: rng.Start.Line,
		})
	}

	// Emit non-target node declarations, sorted for stable output.
	nodeList := make([]string, 0, len(seenNodes))
	for n := range seenNodes {
		if n != targetStr {
			nodeList = append(nodeList, n)
		}
	}
	sort.Strings(nodeList)
	if len(nodeList) > 0 {
		for _, n := range nodeList {
			fmt.Printf("  %s;\n", dotID(n))
		}
		fmt.Println()
	}

	// Emit edges, sorted for stable output.
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].from != edges[j].from {
			return edges[i].from < edges[j].from
		}
		return edges[i].to < edges[j].to
	})
	for _, e := range edges {
		label := fmt.Sprintf("%s:%d", e.file, e.line)
		fmt.Printf("  %-40s -> %-40s [label=%s];\n",
			dotID(e.from), dotID(e.to), dotID(label))
	}

	fmt.Println("}")
}

// ── JSON output ───────────────────────────────────────────────────────────────

type jsonNode struct {
	FullAddr   string `json:"full_addr"`
	ModulePath string `json:"module_path,omitempty"`
	Addr       string `json:"addr"`
	Depth      int    `json:"depth"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	Column     int    `json:"column"`
	Via        string `json:"via"`
}

type jsonOutput struct {
	Target    string     `json:"target"`
	Direction string     `json:"direction"`
	Workspace string     `json:"workspace"`
	NodeCount int        `json:"node_count"`
	Nodes     []jsonNode `json:"nodes"`
}

func printJSON(workspace, targetStr, direction string, results []tfref.BackwardResult) {
	nodes := make([]jsonNode, 0, len(results))
	seen := map[string]bool{}
	for _, r := range results {
		fa := fullAddr(r.Ref.From)
		if seen[fa] {
			continue
		}
		seen[fa] = true
		rng := r.Ref.Subject
		nodes = append(nodes, jsonNode{
			FullAddr:   fa,
			ModulePath: r.Ref.From.ModulePath,
			Addr:       r.Ref.From.Addr,
			Depth:      r.Depth,
			File:       relPath(workspace, rng.Filename),
			Line:       rng.Start.Line,
			Column:     rng.Start.Column,
			Via:        fullAddr(r.Ref.To),
		})
	}
	out := jsonOutput{
		Target:    targetStr,
		Direction: direction,
		Workspace: workspace,
		NodeCount: len(nodes),
		Nodes:     nodes,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "error encoding JSON: %v\n", err)
		os.Exit(1)
	}
}

// ── Arg reordering ────────────────────────────────────────────────────────────

// reorderArgs moves flag args before positional args so that
// "tfref WORKSPACE TARGET --flag value" works identically to
// "tfref --flag value WORKSPACE TARGET".
func reorderArgs(args []string) []string {
	valueTakers := map[string]bool{
		"-format": true, "--format": true,
		"-direction": true, "--direction": true,
	}
	cmd := args[0]
	rest := args[1:]
	var flagArgs, posArgs []string
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		if !strings.HasPrefix(a, "-") {
			posArgs = append(posArgs, a)
			continue
		}
		flagArgs = append(flagArgs, a)
		name := strings.SplitN(strings.TrimLeft(a, "-"), "=", 2)[0]
		if !strings.Contains(a, "=") && valueTakers["-"+name] {
			i++
			if i < len(rest) {
				flagArgs = append(flagArgs, rest[i])
			}
		}
	}
	result := append([]string{cmd}, flagArgs...)
	result = append(result, posArgs...)
	return result
}
