package conformance

import (
	"os"
	"path/filepath"
)

const ToolFind Tool = "find"

func FindCases() []Case {
	mkfile := func(t Env, rel, content string) {
		full := filepath.Join(t.TempDir, rel)
		_ = os.MkdirAll(filepath.Dir(full), 0o755)
		_ = os.WriteFile(full, []byte(content), 0o644)
	}
	setupTree := func(e Env) {
		mkfile(e, "src/a.txt", "AAA\n")
		mkfile(e, "src/b.go", "")
		mkfile(e, "src/sub/c.txt", "CCC\n")
		mkfile(e, "src/sub/inner/d.go", "")
	}
	return []Case{
		{
			Name: "name",
			Tool: ToolFind,
			Args: func(e Env) ([]string, string) {
				setupTree(e)
				return []string{"src", "-name", "*.txt"}, ""
			},
			SortStdoutLines: true,
		},
		{
			Name: "type_dir",
			Tool: ToolFind,
			Args: func(e Env) ([]string, string) {
				setupTree(e)
				return []string{"src", "-type", "d"}, ""
			},
			SortStdoutLines: true,
		},
		{
			Name: "type_file",
			Tool: ToolFind,
			Args: func(e Env) ([]string, string) {
				setupTree(e)
				return []string{"src", "-type", "f"}, ""
			},
			SortStdoutLines: true,
		},
		{
			Name: "maxdepth",
			Tool: ToolFind,
			Args: func(e Env) ([]string, string) {
				setupTree(e)
				return []string{"src", "-maxdepth", "1", "-type", "f"}, ""
			},
			SortStdoutLines: true,
		},
		{
			Name: "mindepth",
			Tool: ToolFind,
			Args: func(e Env) ([]string, string) {
				setupTree(e)
				return []string{"src", "-mindepth", "2", "-type", "f"}, ""
			},
			SortStdoutLines: true,
		},
		{
			Name: "size_plus_4k",
			Tool: ToolFind,
			Args: func(e Env) ([]string, string) {
				_ = os.WriteFile(filepath.Join(e.TempDir, "small"), []byte("x"), 0o644)
				_ = os.WriteFile(filepath.Join(e.TempDir, "big"), make([]byte, 8192), 0o644)
				return []string{".", "-type", "f", "-size", "+4k"}, ""
			},
			SortStdoutLines: true,
		},
		{
			Name: "empty",
			Tool: ToolFind,
			Args: func(e Env) ([]string, string) {
				_ = os.WriteFile(filepath.Join(e.TempDir, "x"), []byte("hi"), 0o644)
				_ = os.WriteFile(filepath.Join(e.TempDir, "y"), nil, 0o644)
				return []string{".", "-type", "f", "-empty"}, ""
			},
			SortStdoutLines: true,
		},
		{
			Name: "not_name",
			Tool: ToolFind,
			Args: func(e Env) ([]string, string) {
				setupTree(e)
				return []string{"src", "-type", "f", "-not", "-name", "*.go"}, ""
			},
			SortStdoutLines: true,
		},
		{
			Name: "or_groups",
			Tool: ToolFind,
			Args: func(e Env) ([]string, string) {
				setupTree(e)
				return []string{"src", "(", "-name", "a.txt", "-o", "-name", "*.go", ")"}, ""
			},
			SortStdoutLines: true,
		},
		{
			Name: "prune",
			Tool: ToolFind,
			Args: func(e Env) ([]string, string) {
				setupTree(e)
				return []string{"src", "-name", "sub", "-prune", "-o", "-type", "f", "-print"}, ""
			},
			SortStdoutLines: true,
		},
	}
}
