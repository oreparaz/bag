package wc

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runWC(t *testing.T, stdin []byte, args ...string) (int, string) {
	t.Helper()
	rOut, wOut, _ := os.Pipe()
	oldOut := os.Stdout
	os.Stdout = wOut
	if stdin != nil {
		oldIn := os.Stdin
		rIn, wIn, _ := os.Pipe()
		os.Stdin = rIn
		go func() { wIn.Write(stdin); wIn.Close() }()
		defer func() { os.Stdin = oldIn }()
	}
	exit := Main(args)
	wOut.Close()
	os.Stdout = oldOut
	out, _ := io.ReadAll(rOut)
	return exit, string(out)
}

func writeTmp(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDefault(t *testing.T) {
	p := writeTmp(t, "a.txt", "hello world\nfoo bar baz\n")
	_, out := runWC(t, nil, p)
	// 2 lines, 5 words, 24 bytes.
	fields := strings.Fields(out)
	if len(fields) != 4 {
		t.Fatalf("got %d fields: %q", len(fields), out)
	}
	if fields[0] != "2" || fields[1] != "5" || fields[2] != "24" {
		t.Errorf("expected '2 5 24'; got %q", out)
	}
}

func TestLinesOnly(t *testing.T) {
	p := writeTmp(t, "a.txt", "a\nb\nc\n")
	_, out := runWC(t, nil, "-l", p)
	if !strings.HasPrefix(strings.TrimLeft(out, " "), "3 ") {
		t.Errorf("got %q", out)
	}
}

func TestWordsOnly(t *testing.T) {
	p := writeTmp(t, "a.txt", "  one  two   three  \n")
	_, out := runWC(t, nil, "-w", p)
	if !strings.HasPrefix(strings.TrimLeft(out, " "), "3 ") {
		t.Errorf("got %q", out)
	}
}

func TestBytesOnly(t *testing.T) {
	p := writeTmp(t, "a.txt", "abcde")
	_, out := runWC(t, nil, "-c", p)
	if !strings.HasPrefix(strings.TrimLeft(out, " "), "5 ") {
		t.Errorf("got %q", out)
	}
}

func TestCharsUTF8(t *testing.T) {
	// "héllo" — 5 codepoints, 6 bytes.
	p := writeTmp(t, "a.txt", "héllo")
	_, outBytes := runWC(t, nil, "-c", p)
	_, outChars := runWC(t, nil, "-m", p)
	if !strings.HasPrefix(strings.TrimLeft(outBytes, " "), "6 ") {
		t.Errorf("bytes got %q", outBytes)
	}
	if !strings.HasPrefix(strings.TrimLeft(outChars, " "), "5 ") {
		t.Errorf("chars got %q", outChars)
	}
}

func TestMaxLineLength(t *testing.T) {
	p := writeTmp(t, "a.txt", "abc\nabcdef\n12\n")
	_, out := runWC(t, nil, "-L", p)
	if !strings.HasPrefix(strings.TrimLeft(out, " "), "6 ") {
		t.Errorf("got %q", out)
	}
}

func TestMaxLineLengthTabExpanded(t *testing.T) {
	// `\tabc\n` — tab rounds the column to 8, then "abc" → column 11.
	p := writeTmp(t, "a.txt", "\tabc\n")
	_, out := runWC(t, nil, "-L", p)
	if !strings.HasPrefix(strings.TrimLeft(out, " "), "11 ") {
		t.Errorf("got %q (want max-line=11)", out)
	}
}

func TestMaxLineLengthMultiByte(t *testing.T) {
	// "héllo" — 5 columns, 6 bytes. With UTF-8 locale -L should report 5.
	t.Setenv("LC_ALL", "C.UTF-8")
	p := writeTmp(t, "a.txt", "héllo\n")
	_, out := runWC(t, nil, "-L", p)
	if !strings.HasPrefix(strings.TrimLeft(out, " "), "5 ") {
		t.Errorf("got %q (want max-line=5)", out)
	}
}

func TestCharsUTF8LargeBoundary(t *testing.T) {
	// Build input where a 2-byte rune straddles the 32 KiB read boundary.
	// Pre-fix the orphaned bytes were each counted as a RuneError, so the
	// reported -m value was off by one for every straddling rune.
	t.Setenv("LC_ALL", "C.UTF-8")
	var b strings.Builder
	// Pad up to position 32*1024 - 1 with ASCII, then place a 2-byte rune.
	pad := strings.Repeat("a", 32*1024-1)
	b.WriteString(pad)
	b.WriteString("é") // 2 bytes; first byte at offset 32*1024-1, second at 32*1024
	b.WriteString("\n")
	p := writeTmp(t, "big.txt", b.String())
	_, out := runWC(t, nil, "-m", p)
	want := int64(32*1024 - 1 + 1 + 1) // pad + é + \n = 32769 runes
	got := strings.TrimLeft(out, " ")
	if !strings.HasPrefix(got, fmt.Sprintf("%d ", want)) {
		t.Errorf("got %q, want prefix %d", out, want)
	}
}

func TestStdin(t *testing.T) {
	exit, out := runWC(t, []byte("a b c\n"), "-w")
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if !strings.HasPrefix(strings.TrimLeft(out, " "), "3") {
		t.Errorf("got %q", out)
	}
}

func TestMultipleFilesTotal(t *testing.T) {
	a := writeTmp(t, "a.txt", "x\n")
	b := writeTmp(t, "b.txt", "y\n")
	_, out := runWC(t, nil, "-l", a, b)
	if !strings.Contains(out, "total") {
		t.Errorf("missing total line in %q", out)
	}
}

func TestNonexistent(t *testing.T) {
	exit, _ := runWC(t, nil, "/no/such/file")
	if exit == 0 {
		t.Errorf("exit=0 expected non-zero")
	}
}
