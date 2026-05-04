package conformance

import (
	"os"
	"path/filepath"
	"strings"
)

const ToolHead Tool = "head"

func HeadCases() []Case {
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
			Name: "default_10",
			Tool: ToolHead,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", manyLines)
				return []string{p}, ""
			},
		},
		{
			Name: "n_3",
			Tool: ToolHead,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", manyLines)
				return []string{"-n", "3", p}, ""
			},
		},
		{
			Name: "n_negative",
			Tool: ToolHead,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "a\nb\nc\nd\ne\n")
				return []string{"-n", "-2", p}, ""
			},
		},
		{
			Name: "c_4_bytes",
			Tool: ToolHead,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "abcdefghij")
				return []string{"-c", "4", p}, ""
			},
		},
		{
			Name: "c_negative",
			Tool: ToolHead,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "abcdefghij")
				return []string{"-c", "-3", p}, ""
			},
		},
		{
			Name: "stdin_default",
			Tool: ToolHead,
			Args: func(_ Env) ([]string, string) {
				return []string{}, ""
			},
			Stdin: []byte(manyLines),
		},
		{
			Name: "multi_files_header",
			Tool: ToolHead,
			Args: func(e Env) ([]string, string) {
				a := mkfile(e, "a.txt", "AAA\n")
				b := mkfile(e, "b.txt", "BBB\n")
				return []string{"-n", "1", a, b}, ""
			},
		},
		{
			Name: "quiet_no_headers",
			Tool: ToolHead,
			Args: func(e Env) ([]string, string) {
				a := mkfile(e, "a.txt", "AAA\n")
				b := mkfile(e, "b.txt", "BBB\n")
				return []string{"-q", "-n", "1", a, b}, ""
			},
		},
		{
			Name: "verbose_one_file",
			Tool: ToolHead,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "AAA\n")
				return []string{"-v", "-n", "1", p}, ""
			},
		},
		{
			Name: "no_final_newline",
			Tool: ToolHead,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "a\nb")
				return []string{"-n", "5", p}, ""
			},
		},
		{
			Name: "missing_file_exit_nonzero",
			Tool: ToolHead,
			Args: func(_ Env) ([]string, string) {
				return []string{"/no/such/file"}, ""
			},
			ExpectExit:    ptr(1),
			CompareStdout: ptr(false),
		},
	}
}
