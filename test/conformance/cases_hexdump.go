package conformance

import (
	"bytes"
	"os"
	"path/filepath"
)

const ToolHexdump Tool = "hexdump"

func HexdumpCases() []Case {
	mkfile := func(t Env, name string, data []byte) string {
		p := filepath.Join(t.TempDir, name)
		_ = os.WriteFile(p, data, 0o644)
		return name
	}
	return []Case{
		{
			Name: "canonical",
			Tool: ToolHexdump,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "blob", []byte("hello world\n"))
				return []string{"-C", p}, ""
			},
		},
		{
			Name: "two_byte_hex",
			Tool: ToolHexdump,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "blob", []byte("hello world\n"))
				return []string{"-x", p}, ""
			},
		},
		{
			Name: "two_byte_dec",
			Tool: ToolHexdump,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "blob", []byte("hello world\n"))
				return []string{"-d", p}, ""
			},
		},
		{
			Name: "two_byte_octal",
			Tool: ToolHexdump,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "blob", []byte("hello world\n"))
				return []string{"-o", p}, ""
			},
		},
		{
			Name: "one_byte_octal",
			Tool: ToolHexdump,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "blob", []byte("hello world\n"))
				return []string{"-b", p}, ""
			},
		},
		{
			Name: "one_byte_char",
			Tool: ToolHexdump,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "blob", []byte("ab\n"))
				return []string{"-c", p}, ""
			},
		},
		{
			Name: "stdin_canonical",
			Tool: ToolHexdump,
			Args: func(_ Env) ([]string, string) {
				return []string{"-C"}, ""
			},
			Stdin: []byte("from stdin\n"),
		},
		{
			Name: "skip_and_limit",
			Tool: ToolHexdump,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "blob", []byte("0123456789ABCDEF"))
				return []string{"-C", "-s", "8", "-n", "4", p}, ""
			},
		},
		{
			Name: "squeeze_zero_block",
			Tool: ToolHexdump,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "zeros", bytes.Repeat([]byte{0}, 64))
				return []string{"-C", p}, ""
			},
		},
	}
}
