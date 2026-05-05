package conformance

import (
	"os"
	"path/filepath"
)

const ToolSort Tool = "sort"

// SortCases runs in the C locale (we set LANG=C / LC_ALL=C in the runner's
// env). That keeps GNU sort's collation byte-ordered, matching bag.
func SortCases() []Case {
	mkfile := func(t Env, name, content string) string {
		p := filepath.Join(t.TempDir, name)
		_ = os.WriteFile(p, []byte(content), 0o644)
		return name
	}
	return []Case{
		{
			Name: "lexical",
			Tool: ToolSort,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "banana\napple\ncherry\n")
				return []string{p}, ""
			},
		},
		{
			Name: "numeric",
			Tool: ToolSort,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "10\n2\n1\n11\n")
				return []string{"-n", p}, ""
			},
		},
		{
			Name: "reverse",
			Tool: ToolSort,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "a\nb\nc\n")
				return []string{"-r", p}, ""
			},
		},
		{
			Name: "unique",
			Tool: ToolSort,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "z\na\nz\na\n")
				return []string{"-u", p}, ""
			},
		},
		{
			Name: "key_field2",
			Tool: ToolSort,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "1 zebra\n2 apple\n3 mango\n")
				return []string{"-k", "2", p}, ""
			},
		},
		{
			Name: "key_numeric",
			Tool: ToolSort,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "alpha 100\nbeta 9\ngamma 50\n")
				return []string{"-k", "2n", p}, ""
			},
		},
		{
			Name: "separator_colon",
			Tool: ToolSort,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "z:1\na:2\nm:3\n")
				return []string{"-t:", "-k", "1", p}, ""
			},
		},
		{
			Name: "stdin",
			Tool: ToolSort,
			Args: func(_ Env) ([]string, string) {
				return nil, ""
			},
			Stdin: []byte("c\na\nb\n"),
		},
		{
			Name: "check_sorted",
			Tool: ToolSort,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "a\nb\nc\n")
				return []string{"-c", p}, ""
			},
		},
		{
			Name: "check_unsorted_exit_1",
			Tool: ToolSort,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "c\nb\na\n")
				return []string{"-c", p}, ""
			},
			ExpectExit:    ptr(1),
			CompareStdout: ptr(false),
		},
	}
}
