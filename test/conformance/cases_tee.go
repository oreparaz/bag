package conformance

import (
	"os"
	"path/filepath"
)

const ToolTee Tool = "tee"

func TeeCases() []Case {
	return []Case{
		{
			Name: "stdout_passthrough",
			Tool: ToolTee,
			Args: func(_ Env) ([]string, string) {
				return nil, ""
			},
			Stdin: []byte("simple\n"),
		},
		{
			Name: "single_file",
			Tool: ToolTee,
			Args: func(_ Env) ([]string, string) {
				return []string{"out.txt"}, ""
			},
			Stdin:       []byte("via tee\n"),
			CompareFile: "out.txt",
		},
		{
			Name: "multiple_files",
			Tool: ToolTee,
			Args: func(_ Env) ([]string, string) {
				return []string{"a.txt", "b.txt"}, ""
			},
			Stdin:       []byte("twice\n"),
			CompareFile: "a.txt",
		},
		{
			Name: "append",
			Tool: ToolTee,
			Args: func(e Env) ([]string, string) {
				p := filepath.Join(e.TempDir, "out.txt")
				_ = os.WriteFile(p, []byte("first\n"), 0o644)
				return []string{"-a", "out.txt"}, ""
			},
			Stdin:       []byte("second\n"),
			CompareFile: "out.txt",
		},
	}
}
