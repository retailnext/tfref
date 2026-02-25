package tfref

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
)

// FormatDOT writes a Graphviz DOT digraph to w representing the reference
// results for targetStr in the given workspace.
//
// direction is "backward" or "forward".  workspace is used to make file paths
// in edge labels relative; pass an empty string to keep absolute paths.
//
// When collapseInternals is true (the default --show-internals=false mode),
// edges whose target is a node inside the target module are collapsed so they
// point directly to the target module node itself.  This produces a clean
// "who depends on module.cloud" graph without internal implementation detail.
// When collapseInternals is false the full edge targets (e.g.
// module.cloud.output.vpc_sa_email) are rendered as separate nodes.
//
// Render the output with:
//
//	dot -Tsvg out.dot > out.svg
//
// or paste into https://dreampuf.github.io/GraphvizOnline/
func FormatDOT(w io.Writer, workspace, targetStr, direction string, results []BackwardResult, collapseInternals bool) {
	p := func(format string, args ...any) { fmt.Fprintf(w, format, args...) }

	// Determine the child-module prefix so we can collapse internal edge targets.
	target := ParseFullAddr(targetStr)
	childPath, isModule := ModuleChildPath(target)
	childPathSlash := childPath + "/"

	// effectiveTo returns the DOT node address for an edge target.  If
	// collapseInternals is true and the target lives inside the module, it is
	// replaced with the module call node itself.
	effectiveTo := func(to NodeID) string {
		if collapseInternals && isModule {
			if to.ModulePath == childPath || strings.HasPrefix(to.ModulePath, childPathSlash) {
				return targetStr
			}
		}
		return FullAddr(to)
	}

	p("// tfref %s: %s\n", direction, targetStr)
	p("// workspace: %s\n", workspace)
	p("// %d node(s) found\n", len(results))
	if len(results) == 0 {
		p("// (nothing found — the target may not exist or have no %s)\n", direction+" references")
	}
	p("\n")
	p("digraph tfref {\n")
	p("  rankdir=BT;  // bottom-to-top: dependents above, dependencies below\n")
	p("  node [fontname=\"Helvetica\", fontsize=10];\n")
	p("  edge [fontname=\"Helvetica\", fontsize=9, color=\"#555555\"];\n")
	p("\n")

	// Target node — filled box so it stands out.
	p("  %-40s [shape=box, style=\"filled,bold\", fillcolor=\"#d0e8ff\", label=%s];\n",
		dotQuote(targetStr), dotQuote(targetStr))

	type edge struct {
		from, to, file string
		line           int
	}
	var edges []edge
	seenNodes := map[string]bool{targetStr: true}
	seenEdges := map[string]bool{} // deduplicate after collapse

	for _, r := range results {
		fa := FullAddr(r.Ref.From)
		ta := effectiveTo(r.Ref.To)
		// After collapsing, multiple results may produce the same from→to pair.
		edgeKey := fa + "\x00" + ta
		if seenEdges[edgeKey] {
			continue
		}
		seenEdges[edgeKey] = true
		seenNodes[fa] = true
		// Internal collapsed targets (== targetStr) are already in seenNodes.
		if ta != targetStr {
			seenNodes[ta] = true
		}
		rng := r.Ref.Subject
		edges = append(edges, edge{
			from: fa,
			to:   ta,
			file: dotRelPath(workspace, rng.Filename),
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
			p("  %s;\n", dotQuote(n))
		}
		p("\n")
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
		p("  %-40s -> %-40s [label=%s];\n",
			dotQuote(e.from), dotQuote(e.to), dotQuote(label))
	}

	p("}\n")
}

// FullAddr converts a NodeID to its canonical full Terraform address string.
//
//	NodeID{"", "module.cloud"}                → "module.cloud"
//	NodeID{"module.foo", "aws_s3_bucket.b"}   → "module.foo.aws_s3_bucket.b"
//	NodeID{"module.foo/module.bar", "var.x"}  → "module.foo.module.bar.var.x"
func FullAddr(n NodeID) string {
	if n.ModulePath == "" {
		return n.Addr
	}
	parts := strings.Split(n.ModulePath, "/")
	return strings.Join(parts, ".") + "." + n.Addr
}

// dotQuote returns a DOT-safe double-quoted identifier.
func dotQuote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

func dotRelPath(base, abs string) string {
	if base == "" || abs == "" {
		return abs
	}
	rel, err := filepath.Rel(base, abs)
	if err != nil {
		return abs
	}
	return rel
}
