package conformance

import (
	"bytes"
	"os"
	"path/filepath"
)

const ToolBase64 Tool = "base64"

func Base64Cases() []Case {
	mkfile := func(t Env, name string, data []byte) string {
		p := filepath.Join(t.TempDir, name)
		_ = os.WriteFile(p, data, 0o644)
		return name
	}
	all256 := func() []byte {
		b := make([]byte, 256)
		for i := range b {
			b[i] = byte(i)
		}
		return b
	}()

	return []Case{
		{
			Name:  "encode_default",
			Tool:  ToolBase64,
			Args:  func(_ Env) ([]string, string) { return nil, "" },
			Stdin: []byte("hello"),
		},
		{
			Name: "encode_long_default_wrap_at_76",
			Tool: ToolBase64,
			Args: func(_ Env) ([]string, string) { return nil, "" },
			Stdin: bytes.Repeat([]byte{'A'}, 200),
		},
		{
			Name:  "encode_wrap_zero",
			Tool:  ToolBase64,
			Args:  func(_ Env) ([]string, string) { return []string{"-w", "0"}, "" },
			Stdin: bytes.Repeat([]byte{'A'}, 200),
		},
		{
			Name:  "encode_wrap_10",
			Tool:  ToolBase64,
			Args:  func(_ Env) ([]string, string) { return []string{"-w", "10"}, "" },
			Stdin: []byte("hello world!"),
		},
		{
			Name: "encode_from_file",
			Tool: ToolBase64,
			Args: func(e Env) ([]string, string) {
				p := mkfile(e, "in.txt", []byte("foobar"))
				return []string{p}, ""
			},
		},
		{
			Name:  "decode_simple",
			Tool:  ToolBase64,
			Args:  func(_ Env) ([]string, string) { return []string{"-d"}, "" },
			Stdin: []byte("aGVsbG8=\n"),
		},
		{
			Name:  "decode_multiline",
			Tool:  ToolBase64,
			Args:  func(_ Env) ([]string, string) { return []string{"--decode"}, "" },
			Stdin: []byte("Zm9v\nYmFy\n"),
		},
		{
			Name:  "decode_ignore_garbage",
			Tool:  ToolBase64,
			Args:  func(_ Env) ([]string, string) { return []string{"-d", "-i"}, "" },
			Stdin: []byte("aG**Vs\tbG8\n=\n"),
		},
		{
			Name:  "encode_empty",
			Tool:  ToolBase64,
			Args:  func(_ Env) ([]string, string) { return nil, "" },
			Stdin: []byte(""),
		},
		{
			Name:  "encode_all_bytes",
			Tool:  ToolBase64,
			Args:  func(_ Env) ([]string, string) { return nil, "" },
			Stdin: all256,
		},
	}
}
