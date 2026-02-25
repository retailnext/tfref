// Copyright (c) 2026 RetailNext, Inc. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

// Package tfref parses Terraform / OpenTofu workspaces and builds a
// position-annotated reference graph that can be queried for forward and
// backward (transitive) dependencies across module boundaries.
package tfref

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
)

// NodeID uniquely identifies a Terraform block or named value within a
// specific module scope.
//
// Examples:
//
//	NodeID{"", "aws_instance.web"}          root-module resource
//	NodeID{"", "var.region"}                root-module variable
//	NodeID{"", "local.bar"}                 root-module local value
//	NodeID{"module.foo", "var.cidr"}        variable in child module "foo"
//	NodeID{"module.foo/module.bar", "output.result"}  nested child module
type NodeID struct {
	// ModulePath is the canonical slash-separated module call path.
	// Empty string means the root module.
	// "module.foo" means the direct child called "foo".
	// "module.foo/module.bar" means "bar" nested inside "foo".
	ModulePath string

	// Addr is the Terraform address of the block within the module, e.g.
	// "aws_instance.web", "var.region", "local.bar", "output.result",
	// "module.child", "data.aws_ami.ubuntu".
	Addr string
}

// String returns a human-readable representation of the node.
func (n NodeID) String() string {
	if n.ModulePath == "" {
		return n.Addr
	}
	return n.ModulePath + "::" + n.Addr
}

// Ref is a single directed reference from one node to another, carrying the
// exact source location of the expression token that creates the reference.
type Ref struct {
	// From is the node that contains the reference expression.
	From NodeID

	// To is the node being referenced.
	To NodeID

	// Subject is the source range of the reference expression token (e.g. the
	// traversal "module.foo.output_name") within the original .tf file.
	Subject hcl.Range
}

// String returns a human-readable one-line description of the reference.
func (r Ref) String() string {
	return fmt.Sprintf("%s → %s  @ %s:%d:%d",
		r.From, r.To,
		r.Subject.Filename,
		r.Subject.Start.Line,
		r.Subject.Start.Column,
	)
}

// Graph holds the complete set of reference edges for a workspace, indexed
// for both forward (what does X depend on?) and backward (what depends on X?)
// lookups.
type Graph struct {
	// Forward maps a node to all Refs it makes (outbound edges).
	Forward map[NodeID][]Ref

	// Backward maps a node to all Refs that point at it (inbound edges).
	Backward map[NodeID][]Ref

	// Defined is the set of nodes that have an explicit defining block in the
	// workspace source — resource, data, module, variable, output, locals
	// attributes, and the to/from targets of import/moved/removed blocks.
	// Use NodeExists to check whether a target address is valid.
	Defined map[NodeID]bool
}

// NewGraph allocates an empty Graph.
func NewGraph() *Graph {
	return &Graph{
		Forward:  make(map[NodeID][]Ref),
		Backward: make(map[NodeID][]Ref),
		Defined:  make(map[NodeID]bool),
	}
}

// Add inserts a reference edge into the graph, updating both the forward and
// backward indexes.
func (g *Graph) Add(r Ref) {
	g.Forward[r.From] = append(g.Forward[r.From], r)
	g.Backward[r.To] = append(g.Backward[r.To], r)
}

// BackwardResult is one entry in a transitive backward reference walk.
type BackwardResult struct {
	// Ref is the directed reference edge.
	Ref Ref

	// Depth is the number of hops from the original target.
	// 1 means the node directly references the target.
	// 2 means it references something that references the target, etc.
	Depth int
}