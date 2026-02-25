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
//	tfref . module.cloud --direction forward
//	tfref . module.foo.aws_s3_bucket.mybucket
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
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

	direction := flag.String("direction", "backward", "traversal direction: backward (who depends on target) or forward (what does target depend on)")

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

	if !tfref.NodeExists(graph, target) {
		fmt.Fprintf(os.Stderr, "error: %q does not exist in this workspace\n", targetStr)
		os.Exit(1)
	}

	var results []tfref.BackwardResult
	switch *direction {
	case "forward":
		results = tfref.DeepForwardRefs(graph, target)
	default:
		results = tfref.DeepBackwardRefs(graph, target)
	}

	tfref.FormatDOT(os.Stdout, absWorkspace, targetStr, *direction, results)
}

// ── Arg reordering ────────────────────────────────────────────────────────────

// reorderArgs moves flag args before positional args so that
// "tfref WORKSPACE TARGET --flag value" works identically to
// "tfref --flag value WORKSPACE TARGET".
func reorderArgs(args []string) []string {
	valueTakers := map[string]bool{
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
