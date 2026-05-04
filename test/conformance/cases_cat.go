package conformance

import (
	"os"
	"path/filepath"
)

const (
	ToolCat Tool = "cat"
)

// CatCases is the cat conformance corpus. We compare stdout byte-for-byte
// against GNU coreutils cat.
//
// File-input cases use basenames so the temp-dir absolute path doesn't
// leak into stdout (cat usually doesn't print paths, but head and others
// do; we keep the convention uniform).
//
// Where alpine's BusyBox cat differs from GNU cat (e.g. cat -A on certain
// non-printables), we mark cases with relaxations rather than skipping.
func CatCases() []Case {
	// mkfile writes content into the case's TempDir under name and returns
	// the basename. The spawned process's cwd is the same TempDir.
	mkfile := func(t Env, name, content string) string {
		p := filepath.Join(t.TempDir, name)
		_ = os.WriteFile(p, []byte(content), 0o644)
		return name
	}

	return []Case{
		{
			Name: "single_file",
			Tool: ToolCat,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "hello\nworld\n")
				return []string{p}, ""
			},
		},
		{
			Name: "multiple_files",
			Tool: ToolCat,
			Args: func(e Env) ([]string, string) {
				a := mkfile(e, "a.txt", "A\n")
				b := mkfile(e, "b.txt", "B\n")
				c := mkfile(e, "c.txt", "C\n")
				return []string{a, b, c}, ""
			},
		},
		{
			Name: "stdin",
			Tool: ToolCat,
			Args: func(_ Env) ([]string, string) {
				return []string{}, ""
			},
			Stdin: []byte("from stdin\n"),
		},
		{
			Name: "stdin_dash",
			Tool: ToolCat,
			Args: func(_ Env) ([]string, string) {
				return []string{"-"}, ""
			},
			Stdin: []byte("via dash\n"),
		},
		{
			Name: "number_all",
			Tool: ToolCat,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "first\n\nthird\n")
				return []string{"-n", p}, ""
			},
		},
		{
			Name: "number_nonblank",
			Tool: ToolCat,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "first\n\nthird\n")
				return []string{"-b", p}, ""
			},
		},
		{
			Name: "squeeze_blank",
			Tool: ToolCat,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "x\n\n\n\ny\n")
				return []string{"-s", p}, ""
			},
		},
		{
			Name: "show_ends",
			Tool: ToolCat,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "x\ny\n")
				return []string{"-E", p}, ""
			},
		},
		{
			Name: "show_tabs",
			Tool: ToolCat,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "a\tb\n")
				return []string{"-T", p}, ""
			},
		},
		{
			Name: "show_all",
			Tool: ToolCat,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "a\tb\x01c\n")
				return []string{"-A", p}, ""
			},
		},
		{
			Name: "binary_passthrough",
			Tool: ToolCat,
			Args: func(e Env) ([]string, string) {
				data := make([]byte, 256)
				for i := range data {
					data[i] = byte(i)
				}
				p := filepath.Join(e.TempDir, "blob")
				_ = os.WriteFile(p, data, 0o644)
				return []string{p}, ""
			},
		},
		{
			Name: "no_final_newline",
			Tool: ToolCat,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "a\nb")
				return []string{"-n", p}, ""
			},
		},
		{
			Name: "nonexistent_file_exit_nonzero",
			Tool: ToolCat,
			Args: func(_ Env) ([]string, string) {
				return []string{"/no/such/file"}, ""
			},
			ExpectExitMatch: ptr(true),
			ExpectExit:      ptr(1),
			CompareStdout:   ptr(false),
		},
	}
}
