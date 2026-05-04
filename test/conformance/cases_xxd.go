package conformance

import (
	"bytes"
	"os"
	"path/filepath"
)

const ToolXXD Tool = "xxd"

func XXDCases() []Case {
	mkbin := func(t Env, name string, data []byte) string {
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
			Name: "default_dump",
			Tool: ToolXXD,
			Args: func(e Env) ([]string, string) {
				p := mkbin(e, "blob.bin", []byte("hello world\n"))
				return []string{p}, ""
			},
		},
		{
			Name: "default_dump_all_bytes",
			Tool: ToolXXD,
			Args: func(e Env) ([]string, string) {
				p := mkbin(e, "blob.bin", all256)
				return []string{p}, ""
			},
		},
		{
			Name: "stdin_default",
			Tool: ToolXXD,
			Args: func(_ Env) ([]string, string) {
				return nil, ""
			},
			Stdin: []byte("the quick brown fox jumps over the lazy dog\n"),
		},
		{
			Name: "plain",
			Tool: ToolXXD,
			Args: func(e Env) ([]string, string) {
				p := mkbin(e, "blob.bin", []byte("hello"))
				return []string{"-p", p}, ""
			},
		},
		{
			Name: "uppercase_plain",
			Tool: ToolXXD,
			Args: func(e Env) ([]string, string) {
				p := mkbin(e, "blob.bin", []byte{0xab, 0xcd, 0xef})
				return []string{"-u", "-p", p}, ""
			},
		},
		{
			Name: "cols_5",
			Tool: ToolXXD,
			Args: func(e Env) ([]string, string) {
				p := mkbin(e, "blob.bin", []byte("0123456789"))
				return []string{"-c", "5", p}, ""
			},
		},
		{
			Name: "group_4",
			Tool: ToolXXD,
			Args: func(e Env) ([]string, string) {
				p := mkbin(e, "blob.bin", []byte("01234567"))
				return []string{"-g", "4", p}, ""
			},
		},
		{
			Name: "skip_5",
			Tool: ToolXXD,
			Args: func(e Env) ([]string, string) {
				p := mkbin(e, "blob.bin", []byte("0123456789"))
				return []string{"-s", "5", "-p", p}, ""
			},
		},
		{
			Name: "limit_3",
			Tool: ToolXXD,
			Args: func(e Env) ([]string, string) {
				p := mkbin(e, "blob.bin", []byte("0123456789"))
				return []string{"-l", "3", "-p", p}, ""
			},
		},
		{
			Name:  "revert_default",
			Tool:  ToolXXD,
			Args:  func(_ Env) ([]string, string) { return []string{"-r"}, "" },
			Stdin: []byte("00000000: 6865 6c6c 6f0a                           hello.\n"),
		},
		{
			Name:  "revert_plain",
			Tool:  ToolXXD,
			Args:  func(_ Env) ([]string, string) { return []string{"-r", "-p"}, "" },
			Stdin: []byte("68656c6c6f\n"),
		},
		{
			Name: "roundtrip_full",
			Tool: ToolXXD,
			Args: func(e Env) ([]string, string) {
				p := mkbin(e, "blob.bin", bytes.Repeat([]byte("ABCD"), 64))
				return []string{p}, ""
			},
		},
	}
}
