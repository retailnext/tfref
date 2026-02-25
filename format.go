// Copyright (c) 2026 RetailNext, Inc. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

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
// Render the output with:
//
//	dot -Tsvg out.dot > out.svg
//
// or paste into https://dreampuf.github.io/GraphvizOnline/
func FormatDOT(w io.Writer, workspace, targetStr, direction string, results []BackwardResult) {
	p := func(format string, args ...any) { fmt.Fprintf(w, format, args...) }

	p("// tfref %s: %s\n", direction, targetStr)
	p("// workspace: %s\n", workspace)
	p("// %d node(s) found\n", len(results))
	if len(results) == 0 {
		p("// (nothing found — the target exists but has no %s)\n", direction+" references")
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

	for _, r := range results {
		fa := FullAddr(r.Ref.From)
		ta := FullAddr(r.Ref.To)
		seenNodes[fa] = true
		seenNodes[ta] = true
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

