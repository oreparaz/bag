package conformance

import (
	"os"
	"path/filepath"
)

const ToolUniq Tool = "uniq"

func UniqCases() []Case {
	mkfile := func(t Env, name, content string) string {
		p := filepath.Join(t.TempDir, name)
		_ = os.WriteFile(p, []byte(content), 0o644)
		return name
	}
	return []Case{
		{
			Name: "default",
			Tool: ToolUniq,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "a\na\nb\nc\nc\nc\n")
				return []string{p}, ""
			},
		},
		{
			Name: "count",
			Tool: ToolUniq,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "a\na\nb\n")
				return []string{"-c", p}, ""
			},
		},
		{
			Name: "duplicates_only",
			Tool: ToolUniq,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "a\na\nb\nc\nc\n")
				return []string{"-d", p}, ""
			},
		},
		{
			Name: "unique_only",
			Tool: ToolUniq,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "a\na\nb\nc\nc\n")
				return []string{"-u", p}, ""
			},
		},
		{
			Name: "ignore_case",
			Tool: ToolUniq,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "Foo\nFOO\nbar\n")
				return []string{"-i", p}, ""
			},
		},
		{
			Name: "skip_fields",
			Tool: ToolUniq,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "1 alpha\n2 alpha\n3 beta\n")
				return []string{"-f", "1", p}, ""
			},
		},
		{
			Name: "stdin",
			Tool: ToolUniq,
			Args: func(_ Env) ([]string, string) {
				return nil, ""
			},
			Stdin: []byte("x\nx\ny\n"),
		},
	}
}
