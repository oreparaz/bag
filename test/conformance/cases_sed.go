package conformance

import (
	"os"
	"path/filepath"
)

const ToolSed Tool = "sed"

// SedCases stays inside the GNU-BRE / RE2 intersection. ERE-specific
// patterns are explicitly opted in with -E.
func SedCases() []Case {
	mkfile := func(t Env, name, content string) string {
		p := filepath.Join(t.TempDir, name)
		_ = os.WriteFile(p, []byte(content), 0o644)
		return name
	}

	return []Case{
		{
			Name: "substitute_first",
			Tool: ToolSed,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "xxx\n")
				return []string{"s/x/y/", p}, ""
			},
		},
		{
			Name: "substitute_global",
			Tool: ToolSed,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "xxx\n")
				return []string{"s/x/y/g", p}, ""
			},
		},
		{
			Name: "alternate_delim",
			Tool: ToolSed,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "/usr/bin\n")
				return []string{"s|/usr|/opt|", p}, ""
			},
		},
		{
			Name: "delete_regex",
			Tool: ToolSed,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "a\nb\nc\n")
				return []string{"/b/d", p}, ""
			},
		},
		{
			Name: "delete_line_range",
			Tool: ToolSed,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "a\nb\nc\nd\n")
				return []string{"2,3d", p}, ""
			},
		},
		{
			Name: "print_with_n",
			Tool: ToolSed,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "a\nb\nc\n")
				return []string{"-n", "2p", p}, ""
			},
		},
		{
			Name: "quit_at_line",
			Tool: ToolSed,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "a\nb\nc\n")
				return []string{"2q", p}, ""
			},
		},
		{
			Name: "last_line_delete",
			Tool: ToolSed,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "a\nb\nc\n")
				return []string{"$d", p}, ""
			},
		},
		{
			Name: "regex_range_delete",
			Tool: ToolSed,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "out\nstart\nin\nend\nout\n")
				return []string{"/start/,/end/d", p}, ""
			},
		},
		{
			Name: "multi_e",
			Tool: ToolSed,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "foo bar\n")
				return []string{"-e", "s/foo/F/", "-e", "s/bar/B/", p}, ""
			},
		},
		{
			Name: "stdin",
			Tool: ToolSed,
			Args: func(_ Env) ([]string, string) {
				return []string{"s/hello/world/"}, ""
			},
			Stdin: []byte("hello earth\nhello mars\n"),
		},
	}
}
