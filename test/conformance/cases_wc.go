package conformance

import (
	"os"
	"path/filepath"
)

const ToolWC Tool = "wc"

func WCCases() []Case {
	mkfile := func(t Env, name, content string) string {
		p := filepath.Join(t.TempDir, name)
		_ = os.WriteFile(p, []byte(content), 0o644)
		return name
	}

	return []Case{
		{
			Name: "default_lwc",
			Tool: ToolWC,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "hello world\nfoo bar baz\n")
				return []string{p}, ""
			},
		},
		{
			Name: "lines_only",
			Tool: ToolWC,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "a\nb\nc\n")
				return []string{"-l", p}, ""
			},
		},
		{
			Name: "words_only",
			Tool: ToolWC,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "  one  two   three  \n")
				return []string{"-w", p}, ""
			},
		},
		{
			Name: "bytes_only",
			Tool: ToolWC,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "abcde")
				return []string{"-c", p}, ""
			},
		},
		// Note: -m (chars) is intentionally not in the conformance corpus.
		// Behavior is libc-dependent: glibc respects locale and falls back
		// to byte counting under C/POSIX, while musl always treats input
		// as UTF-8. Bag has unit tests covering its own behavior.
		{
			Name: "max_line_length",
			Tool: ToolWC,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "abc\nabcdef\n12\n")
				return []string{"-L", p}, ""
			},
		},
		{
			Name: "stdin_default",
			Tool: ToolWC,
			Args: func(_ Env) ([]string, string) {
				return []string{}, ""
			},
			Stdin: []byte("alpha beta\n"),
		},
		{
			Name: "multiple_files_with_total",
			Tool: ToolWC,
			Args: func(e Env) ([]string, string) {
				a := mkfile(e, "a.txt", "x\n")
				b := mkfile(e, "b.txt", "y\n")
				return []string{"-l", a, b}, ""
			},
		},
		{
			Name: "nonexistent_exit_nonzero",
			Tool: ToolWC,
			Args: func(_ Env) ([]string, string) {
				return []string{"/no/such/file"}, ""
			},
			ExpectExit:    ptr(1),
			CompareStdout: ptr(false),
		},
	}
}
