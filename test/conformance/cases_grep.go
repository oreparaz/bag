package conformance

import (
	"os"
	"path/filepath"
	"strings"
)

const ToolGrep Tool = "grep"

// GrepCases stays in the well-defined intersection of GNU grep's BRE
// behavior and Go's RE2: patterns are either pure literals (so -F is a
// no-op) or use ERE explicitly via -E. We avoid BRE-specific syntax
// (escaped grouping etc.) so cross-distro variation is bounded.
func GrepCases() []Case {
	mkfile := func(t Env, name, content string) string {
		p := filepath.Join(t.TempDir, name)
		_ = os.WriteFile(p, []byte(content), 0o644)
		return name
	}
	threeLines := strings.Join([]string{"alpha", "beta", "gamma"}, "\n") + "\n"

	return []Case{
		{
			Name: "fixed_match",
			Tool: ToolGrep,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", threeLines)
				return []string{"-F", "beta", p}, ""
			},
		},
		{
			Name: "ignore_case",
			Tool: ToolGrep,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "Foo\nbar\n")
				return []string{"-Fi", "FOO", p}, ""
			},
		},
		{
			Name: "invert",
			Tool: ToolGrep,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "x\ny\nx\n")
				return []string{"-Fv", "x", p}, ""
			},
		},
		{
			Name: "count",
			Tool: ToolGrep,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "xa\nxb\nyc\n")
				return []string{"-Fc", "x", p}, ""
			},
		},
		{
			Name: "line_number",
			Tool: ToolGrep,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "aa\nbb\ncc\n")
				return []string{"-Fn", "bb", p}, ""
			},
		},
		{
			Name: "no_match_exit_1",
			Tool: ToolGrep,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "x\n")
				return []string{"-F", "z", p}, ""
			},
			ExpectExit:    ptr(1),
			CompareStdout: ptr(true),
		},
		{
			Name: "files_with_match",
			Tool: ToolGrep,
			Args: func(e Env) ([]string, string) {
				a := mkfile(e, "a", "yes\n")
				b := mkfile(e, "b", "no\n")
				return []string{"-Fl", "yes", a, b}, ""
			},
		},
		{
			Name: "files_without_match",
			Tool: ToolGrep,
			Args: func(e Env) ([]string, string) {
				a := mkfile(e, "a", "yes\n")
				b := mkfile(e, "b", "no\n")
				return []string{"-FL", "yes", a, b}, ""
			},
		},
		{
			Name: "stdin_default",
			Tool: ToolGrep,
			Args: func(_ Env) ([]string, string) {
				return []string{"-F", "be"}, ""
			},
			Stdin: []byte("alpha\nbeta\ngamma\n"),
		},
		{
			Name: "ere_alternation",
			Tool: ToolGrep,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "ant\nbat\ncat\n")
				return []string{"-E", "^(ant|cat)$", p}, ""
			},
		},
		{
			Name: "context_C",
			Tool: ToolGrep,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "a\nb\nMATCH\nd\ne\n")
				return []string{"-FC", "1", "MATCH", p}, ""
			},
		},
		{
			Name: "word_regexp",
			Tool: ToolGrep,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "foobar\nfoo bar\n")
				return []string{"-Fw", "foo", p}, ""
			},
		},
	}
}
