// Command bag is a multicall binary (busybox-style) that ships memory-safe
// drop-in replacements for common Unix tools.
//
// Dispatch order:
//  1. If basename(argv[0]) is a known tool, run that tool with argv[1:].
//  2. Otherwise treat argv[1] as the tool name and run it with argv[2:].
//  3. With no tool, print the list of tools.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/oreparaz/bag/internal/curl"
	"github.com/oreparaz/bag/internal/wget"
)

// Tool is one entry in the multicall dispatch table.
type Tool struct {
	Name string
	Run  func(args []string) int
	Help string
}

func tools() []Tool {
	return []Tool{
		{Name: "curl", Run: curl.Main, Help: "transfer URLs"},
		{Name: "wget", Run: wget.Main, Help: "download files"},
	}
}

func main() {
	os.Exit(dispatch(os.Args))
}

func dispatch(argv []string) int {
	all := tools()
	byName := map[string]Tool{}
	for _, t := range all {
		byName[t.Name] = t
	}

	if len(argv) == 0 {
		return usage(all, 2)
	}

	base := filepath.Base(argv[0])
	// Strip a "bag-" prefix (some packagers ship binaries that way).
	base = strings.TrimPrefix(base, "bag-")

	if t, ok := byName[base]; ok {
		return t.Run(argv[1:])
	}

	if len(argv) >= 2 {
		// Top-level flags
		switch argv[1] {
		case "--list", "list":
			for _, t := range all {
				fmt.Printf("%s\t%s\n", t.Name, t.Help)
			}
			return 0
		case "--version", "-V", "version":
			fmt.Println(versionString())
			return 0
		case "-h", "--help", "help":
			return usage(all, 0)
		}
		if t, ok := byName[argv[1]]; ok {
			return t.Run(argv[2:])
		}
		fmt.Fprintf(os.Stderr, "bag: unknown tool %q\n", argv[1])
		return 2
	}

	return usage(all, 2)
}

func usage(all []Tool, code int) int {
	out := os.Stderr
	if code == 0 {
		out = os.Stdout
	}
	fmt.Fprintln(out, "Usage: bag <tool> [args...]")
	fmt.Fprintln(out, "       <tool> [args...]   (when invoked via symlink)")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Tools:")
	names := make([]string, 0, len(all))
	for _, t := range all {
		names = append(names, t.Name)
	}
	sort.Strings(names)
	byName := map[string]Tool{}
	for _, t := range all {
		byName[t.Name] = t
	}
	for _, n := range names {
		fmt.Fprintf(out, "  %-8s %s\n", n, byName[n].Help)
	}
	return code
}

// version is set via -ldflags at build time. Default is "dev".
var version = "dev"

func versionString() string {
	return fmt.Sprintf("bag %s", version)
}
