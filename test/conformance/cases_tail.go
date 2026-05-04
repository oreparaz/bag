package conformance

import (
	"os"
	"path/filepath"
	"strings"
)

const ToolTail Tool = "tail"

func TailCases() []Case {
	mkfile := func(t Env, name, content string) string {
		p := filepath.Join(t.TempDir, name)
		_ = os.WriteFile(p, []byte(content), 0o644)
		return name
	}
	manyLines := strings.Join([]string{
		"l1", "l2", "l3", "l4", "l5", "l6", "l7", "l8", "l9", "l10",
		"l11", "l12", "l13", "l14", "l15",
	}, "\n") + "\n"

	return []Case{
		{
			Name: "default_last_10",
			Tool: ToolTail,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", manyLines)
				return []string{p}, ""
			},
		},
		{
			Name: "n_3_last",
			Tool: ToolTail,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", manyLines)
				return []string{"-n", "3", p}, ""
			},
		},
		{
			Name: "n_plus3_from_start",
			Tool: ToolTail,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "a\nb\nc\nd\ne\n")
				return []string{"-n", "+3", p}, ""
			},
		},
		{
			Name: "c_5_last_bytes",
			Tool: ToolTail,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "0123456789")
				return []string{"-c", "5", p}, ""
			},
		},
		{
			Name: "c_plus5_from_byte",
			Tool: ToolTail,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "0123456789")
				return []string{"-c", "+5", p}, ""
			},
		},
		{
			Name: "stdin_default",
			Tool: ToolTail,
			Args: func(_ Env) ([]string, string) {
				return []string{}, ""
			},
			Stdin: []byte(manyLines),
		},
		{
			Name: "multi_files_header",
			Tool: ToolTail,
			Args: func(e Env) ([]string, string) {
				a := mkfile(e, "a.txt", "AAA\n")
				b := mkfile(e, "b.txt", "BBB\n")
				return []string{"-n", "1", a, b}, ""
			},
		},
		{
			Name: "quiet_no_headers",
			Tool: ToolTail,
			Args: func(e Env) ([]string, string) {
				a := mkfile(e, "a.txt", "AAA\n")
				b := mkfile(e, "b.txt", "BBB\n")
				return []string{"-q", "-n", "1", a, b}, ""
			},
		},
		{
			Name: "n_zero_empty_output",
			Tool: ToolTail,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", manyLines)
				return []string{"-n", "0", p}, ""
			},
		},
		{
			Name: "missing_file_exit_nonzero",
			Tool: ToolTail,
			Args: func(_ Env) ([]string, string) {
				return []string{"/no/such/file"}, ""
			},
			ExpectExit:    ptr(1),
			CompareStdout: ptr(false),
		},
	}
}
