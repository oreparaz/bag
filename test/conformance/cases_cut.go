package conformance

import (
	"os"
	"path/filepath"
)

const ToolCut Tool = "cut"

func CutCases() []Case {
	mkfile := func(t Env, name, content string) string {
		p := filepath.Join(t.TempDir, name)
		_ = os.WriteFile(p, []byte(content), 0o644)
		return name
	}
	return []Case{
		{
			Name: "fields_one",
			Tool: ToolCut,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "a:b:c\nd:e:f\n")
				return []string{"-d:", "-f2", p}, ""
			},
		},
		{
			Name: "fields_range",
			Tool: ToolCut,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "a:b:c:d\n")
				return []string{"-d:", "-f", "2-3", p}, ""
			},
		},
		{
			Name: "fields_open_end",
			Tool: ToolCut,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "a:b:c:d\n")
				return []string{"-d:", "-f", "2-", p}, ""
			},
		},
		{
			Name: "chars_range",
			Tool: ToolCut,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "abcdef\n")
				return []string{"-c", "2-4", p}, ""
			},
		},
		{
			Name: "complement",
			Tool: ToolCut,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "abcdef\n")
				return []string{"-c", "2-4", "--complement", p}, ""
			},
		},
		{
			Name: "skip_no_delim",
			Tool: ToolCut,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "a:b\nno_delim\n")
				return []string{"-d:", "-f1", "-s", p}, ""
			},
		},
		{
			Name: "output_delim",
			Tool: ToolCut,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "a.txt", "a:b:c\n")
				return []string{"-d:", "-f", "1,3", "--output-delimiter", ",", p}, ""
			},
		},
		{
			Name: "stdin",
			Tool: ToolCut,
			Args: func(_ Env) ([]string, string) {
				return []string{"-d:", "-f3"}, ""
			},
			Stdin: []byte("k:v:1\nk:v:2\n"),
		},
	}
}
