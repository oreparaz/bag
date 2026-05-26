// Command bag is a multicall binary (busybox-style) that ships memory-safe
// drop-in replacements for common Unix tools.
//
// Dispatch order:
//  1. If basename(argv[0]) is a known tool, run that tool with argv[1:].
//  2. Otherwise treat argv[1] as the tool name and run it with argv[2:].
//  3. With no tool, print the list of tools.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/oreparaz/bag/internal/ag"
	base64cmd "github.com/oreparaz/bag/internal/base64cmd"
	"github.com/oreparaz/bag/internal/cat"
	"github.com/oreparaz/bag/internal/cmpressor"
	"github.com/oreparaz/bag/internal/compress"
	"github.com/oreparaz/bag/internal/curl"
	"github.com/oreparaz/bag/internal/cut"
	"github.com/oreparaz/bag/internal/find"
	"github.com/oreparaz/bag/internal/gpgcmd"
	"github.com/oreparaz/bag/internal/grep"
	"github.com/oreparaz/bag/internal/head"
	"github.com/oreparaz/bag/internal/hexdump"
	"github.com/oreparaz/bag/internal/scp"
	"github.com/oreparaz/bag/internal/sed"
	bagsort "github.com/oreparaz/bag/internal/sort"
	bagssh "github.com/oreparaz/bag/internal/ssh"
	"github.com/oreparaz/bag/internal/tail"
	"github.com/oreparaz/bag/internal/tarcmd"
	"github.com/oreparaz/bag/internal/tee"
	"github.com/oreparaz/bag/internal/uniq"
	"github.com/oreparaz/bag/internal/vi"
	"github.com/oreparaz/bag/internal/wc"
	"github.com/oreparaz/bag/internal/wget"
	"github.com/oreparaz/bag/internal/xxd"
	"github.com/oreparaz/bag/internal/zipcmd"
)

// Tool is one entry in the multicall dispatch table.
type Tool struct {
	Name string
	Run  func(args []string) int
	Help string
}

func tools() []Tool {
	all := []Tool{
		{Name: "ag", Run: ag.Main, Help: "recursive code search (RE2)"},
		{Name: "base64", Run: base64cmd.Main, Help: "encode/decode base64"},
		{Name: "cat", Run: cat.Main, Help: "concatenate files"},
		{Name: "curl", Run: curl.Main, Help: "transfer URLs"},
		{Name: "cut", Run: cut.Main, Help: "select fields/bytes"},
		{Name: "find", Run: find.Main, Help: "walk and filter files"},
		{Name: "gpg", Run: gpgcmd.Main, Help: "OpenPGP encrypt/decrypt/sign/verify"},
		{Name: "grep", Run: grep.Main, Help: "search lines (RE2)"},
		{Name: "head", Run: head.Main, Help: "first lines/bytes"},
		{Name: "hexdump", Run: hexdump.Main, Help: "BSD hex dump"},
		{Name: "scp", Run: scp.Main, Help: "secure copy (over SSH)"},
		{Name: "sed", Run: sed.Main, Help: "stream editor (subset)"},
		{Name: "sort", Run: bagsort.Main, Help: "sort lines"},
		{Name: "ssh", Run: bagssh.Main, Help: "minimal SSH client"},
		{Name: "tail", Run: tail.Main, Help: "last lines/bytes"},
		{Name: "tar", Run: tarcmd.Main, Help: "tape archiver"},
		{Name: "tee", Run: tee.Main, Help: "stdin to stdout + files"},
		{Name: "uniq", Run: uniq.Main, Help: "collapse adjacent dups"},
		{Name: "vi", Run: vi.Main, Help: "modal text editor"},
		{Name: "wc", Run: wc.Main, Help: "count lines/words/bytes"},
		{Name: "wget", Run: wget.Main, Help: "download files"},
		{Name: "xxd", Run: xxd.Main, Help: "hex dump / reverse"},
		{Name: "zip", Run: func(a []string) int { return zipcmd.MainAs("zip", a) }, Help: "create zip archive"},
		{Name: "unzip", Run: func(a []string) int { return zipcmd.MainAs("unzip", a) }, Help: "extract zip archive"},
	}
	all = append(all, compressorTools()...)
	return all
}

// compressorTools registers gzip / bzip2 / xz / zstd plus their un* and *cat
// aliases as separate Tool entries. Each entry's Run closure pins the
// codec format and the alias-specific defaults (decompress mode for un*,
// always-stdout for *cat).
func compressorTools() []Tool {
	type entry struct {
		name           string
		fmtIs          compress.Format
		decompress     bool
		alwaysStdout   bool
		help           string
	}
	entries := []entry{
		{"gzip", compress.FormatGzip, false, false, "compress files (gzip)"},
		{"gunzip", compress.FormatGzip, true, false, "decompress gzip files"},
		{"zcat", compress.FormatGzip, true, true, "decompress gzip to stdout"},

		{"bzip2", compress.FormatBzip2, false, false, "compress files (bzip2)"},
		{"bunzip2", compress.FormatBzip2, true, false, "decompress bzip2 files"},
		{"bzcat", compress.FormatBzip2, true, true, "decompress bzip2 to stdout"},

		{"xz", compress.FormatXZ, false, false, "compress files (xz)"},
		{"unxz", compress.FormatXZ, true, false, "decompress xz files"},
		{"xzcat", compress.FormatXZ, true, true, "decompress xz to stdout"},

		{"zstd", compress.FormatZstd, false, false, "compress files (zstd)"},
		{"unzstd", compress.FormatZstd, true, false, "decompress zstd files"},
		{"zstdcat", compress.FormatZstd, true, true, "decompress zstd to stdout"},
	}
	out := make([]Tool, 0, len(entries))
	for _, e := range entries {
		e := e
		t := cmpressor.Tool{
			Name:              e.name,
			Format:            e.fmtIs,
			DefaultDecompress: e.decompress,
			AlwaysStdout:      e.alwaysStdout,
		}
		out = append(out, Tool{
			Name: e.name,
			Help: e.help,
			Run:  func(args []string) int { return cmpressor.Main(t, args) },
		})
	}
	return out
}

func main() {
	// Restore default SIGPIPE handling so streaming tools (cat/head/tail/
	// grep/...) terminate cleanly when their stdout pipe is closed by
	// a downstream consumer, instead of Go's default of printing
	// "signal: broken pipe" and exiting 1.
	resetSIGPIPE()
	os.Exit(dispatch(os.Args))
}

func dispatch(argv []string) int {
	all := tools()
	byName := map[string]Tool{}
	for _, t := range all {
		byName[t.Name] = t
	}

	if len(argv) == 0 {
		return usage(all, 2)
	}

	base := filepath.Base(argv[0])
	// On Windows the binary is suffixed with .exe; strip it so a
	// `curl.exe` symlink/copy still dispatches to the curl tool.
	base = strings.TrimSuffix(base, ".exe")
	base = strings.TrimSuffix(base, ".EXE")
	// Strip a "bag-" prefix (some packagers ship binaries that way).
	base = strings.TrimPrefix(base, "bag-")
	// Don't treat an empty post-strip base as a lookup key.
	if base != "" {
		if t, ok := byName[base]; ok {
			return t.Run(argv[1:])
		}
	}

	if len(argv) >= 2 {
		// Top-level flags
		switch argv[1] {
		case "--list", "list":
			for _, t := range all {
				fmt.Printf("%s\t%s\n", t.Name, t.Help)
			}
			return 0
		case "--version", "-V", "version":
			fmt.Println(versionString())
			return 0
		case "-h", "--help", "help":
			return usage(all, 0)
		}
		if t, ok := byName[argv[1]]; ok {
			return t.Run(argv[2:])
		}
		fmt.Fprintf(os.Stderr, "bag: unknown tool %q\n", argv[1])
		return 2
	}

	return usage(all, 2)
}

func usage(all []Tool, code int) int {
	out := os.Stderr
	if code == 0 {
		out = os.Stdout
	}
	fmt.Fprintln(out, "Usage: bag <tool> [args...]")
	fmt.Fprintln(out, "       <tool> [args...]   (when invoked via symlink)")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Tools:")
	names := make([]string, 0, len(all))
	for _, t := range all {
		names = append(names, t.Name)
	}
	sort.Strings(names)
	byName := map[string]Tool{}
	for _, t := range all {
		byName[t.Name] = t
	}
	for _, n := range names {
		fmt.Fprintf(out, "  %-8s %s\n", n, byName[n].Help)
	}
	return code
}

// version is set via -ldflags at build time. Default is "dev".
var version = "dev"

func versionString() string {
	return fmt.Sprintf("bag %s", version)
}
