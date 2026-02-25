package tfref

import (
	"fmt"
	"strings"
)

// ParseFullAddr parses a full Terraform address string into a NodeID.
//
// The full address encodes the module path as leading "module.NAME" segments;
// the remainder is the local address within that module.
//
// Examples:
//
//	"module.cloud"                       => NodeID{"", "module.cloud"}
//	"local.foo"                          => NodeID{"", "local.foo"}
//	"module.foo.aws_s3_bucket.mybucket"  => NodeID{"module.foo", "aws_s3_bucket.mybucket"}
//	"module.foo.output.result"           => NodeID{"module.foo", "output.result"}
//	"module.foo.module.bar.local.x"      => NodeID{"module.foo/module.bar", "local.x"}
func ParseFullAddr(s string) NodeID {
	parts := strings.Split(s, ".")
	var moduleParts []string
	// Greedily consume leading module.LABEL pairs while at least 2 parts remain
	// after the pair (so the remainder can still form a valid address).
	for len(parts) > 2 && parts[0] == "module" {
		moduleParts = append(moduleParts, "module."+parts[1])
		parts = parts[2:]
	}
	return NodeID{
		ModulePath: strings.Join(moduleParts, "/"),
		Addr:       strings.Join(parts, "."),
	}
}

// moduleChildPath returns the module path of the child module for a module
// call node, e.g. NodeID{"", "module.cloud"} → "module.cloud",
// NodeID{"module.foo", "module.bar"} → "module.foo/module.bar".
// Returns ("", false) if the node is not a module call.
func moduleChildPath(n NodeID) (string, bool) {
	if !strings.HasPrefix(n.Addr, "module.") {
		return "", false
	}
	if n.ModulePath == "" {
		return n.Addr, true
	}
	return n.ModulePath + "/" + n.Addr, true
}

// seedNodes returns the set of nodes to use as BFS roots for a backward
// search.  For a module call target, this includes both the bare module call
// node AND all of its child output nodes — because callers that reference
// specific module outputs get stitched to those output nodes, not to the bare
// module call node.
//
// It also includes opaque "module.X.SOMETHING" intermediate nodes that were
// not stitched (e.g. from import/moved/removed blocks that reference resources
// inside the module directly rather than via a named output).
func seedNodes(graph *Graph, target NodeID) []NodeID {
	seeds := []NodeID{target}
	childPath, ok := moduleChildPath(target)
	if !ok {
		return seeds
	}
	added := map[NodeID]bool{target: true}
	addSeed := func(n NodeID) {
		if !added[n] {
			added[n] = true
			seeds = append(seeds, n)
		}
	}

	// Child output nodes: callers that reference specific outputs of the module
	// get stitched to NodeID{childPath, "output.X"} nodes.
	for node := range graph.Backward {
		if node.ModulePath == childPath && strings.HasPrefix(node.Addr, "output.") {
			addSeed(node)
		}
	}
	for node := range graph.Forward {
		if node.ModulePath == childPath && strings.HasPrefix(node.Addr, "output.") {
			addSeed(node)
		}
	}

	// Opaque intermediate nodes like NodeID{"", "module.cloud.google_project"} that
	// arise when import/moved/removed blocks reference resources inside the module
	// directly (using a 3-or-4-part traversal like module.cloud.TYPE.name).
	// These don't get stitched because there is no matching output, so they live
	// as unresolved nodes in the graph under the caller's module path.
	opaquePrefix := target.Addr + "." // e.g. "module.cloud."
	for node := range graph.Backward {
		if node.ModulePath == target.ModulePath && strings.HasPrefix(node.Addr, opaquePrefix) {
			addSeed(node)
		}
	}

	return seeds
}

// DeepBackwardRefs performs a breadth-first transitive walk over the backward
// reference graph starting from target, returning every Ref that directly or
// indirectly references the target node.
//
// When target is a module call (e.g. NodeID{"", "module.cloud"}), the search
// also seeds from all output nodes of that module so that callers referencing
// specific outputs (e.g. module.cloud.vpc_sa_email) are correctly included.
//
// Results are returned in BFS order (closest dependents first).  Each entry
// carries the exact source range of the reference token.
func DeepBackwardRefs(graph *Graph, target NodeID) []BackwardResult {
	type item struct {
		node  NodeID
		depth int
	}

	seeds := seedNodes(graph, target)
	visited := make(map[NodeID]bool, len(seeds))
	queue := make([]item, 0, len(seeds))
	for _, s := range seeds {
		if !visited[s] {
			visited[s] = true
			queue = append(queue, item{s, 0})
		}
	}
	var results []BackwardResult

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		for _, ref := range graph.Backward[cur.node] {
			if !visited[ref.From] {
				visited[ref.From] = true
				results = append(results, BackwardResult{Ref: ref, Depth: cur.depth + 1})
				queue = append(queue, item{ref.From, cur.depth + 1})
			}
		}
	}
	return results
}

// DeepForwardRefs performs the same BFS walk in the forward direction,
// returning everything that the target directly or transitively depends on.
func DeepForwardRefs(graph *Graph, target NodeID) []BackwardResult {
	type item struct {
		node  NodeID
		depth int
	}

	visited := map[NodeID]bool{target: true}
	queue := []item{{target, 0}}
	var results []BackwardResult

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		for _, ref := range graph.Forward[cur.node] {
			results = append(results, BackwardResult{Ref: ref, Depth: cur.depth + 1})
			if !visited[ref.To] {
				visited[ref.To] = true
				queue = append(queue, item{ref.To, cur.depth + 1})
			}
		}
	}
	return results
}

// PrintBackwardRefs prints a human-readable report of every node that
// directly or transitively references target.
func PrintBackwardRefs(graph *Graph, target NodeID) {
	results := DeepBackwardRefs(graph, target)
	if len(results) == 0 {
		fmt.Printf("Nothing references %s\n", target)
		return
	}
	fmt.Printf("Everything that (directly or transitively) references %s:\n\n", target)
	printResults(results)
}

// PrintForwardRefs prints a human-readable report of everything that
// target directly or transitively depends on.
func PrintForwardRefs(graph *Graph, target NodeID) {
	results := DeepForwardRefs(graph, target)
	if len(results) == 0 {
		fmt.Printf("%s has no outbound references\n", target)
		return
	}
	fmt.Printf("Everything that %s directly or transitively depends on:\n\n", target)
	printResults(results)
}

func printResults(results []BackwardResult) {
	for _, r := range results {
		indent := strings.Repeat("  ", r.Depth-1)
		rng := r.Ref.Subject
		fmt.Printf("%s[depth %d] %s\n", indent, r.Depth, r.Ref.From)
		fmt.Printf("%s           via ref to %s\n", indent, r.Ref.To)
		fmt.Printf("%s           @ %s:%d:%d\n\n",
			indent, rng.Filename, rng.Start.Line, rng.Start.Column)
	}
}