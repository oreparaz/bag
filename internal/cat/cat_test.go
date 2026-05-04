package cat

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runCat runs the in-process cat with args. Captures stdout.
// stdin is the optional bytes to feed via os.Stdin redirection.
func runCat(t *testing.T, stdin []byte, args ...string) (int, string) {
	t.Helper()

	rOut, wOut, _ := os.Pipe()
	oldOut := os.Stdout
	os.Stdout = wOut

	var oldIn *os.File
	if stdin != nil {
		oldIn = os.Stdin
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
		t.Fatalf("write: %v", err)
	}
	return p
}

func TestPlain(t *testing.T) {
	p := writeTmp(t, "a.txt", "hello\nworld\n")
	exit, out := runCat(t, nil, p)
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if out != "hello\nworld\n" {
		t.Errorf("out=%q", out)
	}
}

func TestStdin(t *testing.T) {
	exit, out := runCat(t, []byte("from stdin\n"))
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if out != "from stdin\n" {
		t.Errorf("out=%q", out)
	}
}

func TestStdinDash(t *testing.T) {
	exit, out := runCat(t, []byte("via dash\n"), "-")
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if out != "via dash\n" {
		t.Errorf("out=%q", out)
	}
}

func TestMultipleFiles(t *testing.T) {
	a := writeTmp(t, "a.txt", "A\n")
	b := writeTmp(t, "b.txt", "B\n")
	exit, out := runCat(t, nil, a, b)
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if out != "A\nB\n" {
		t.Errorf("out=%q", out)
	}
}

func TestNumberAll(t *testing.T) {
	p := writeTmp(t, "a.txt", "first\n\nthird\n")
	_, out := runCat(t, nil, "-n", p)
	want := "     1\tfirst\n     2\t\n     3\tthird\n"
	if out != want {
		t.Errorf("got %q want %q", out, want)
	}
}

func TestNumberNonBlank(t *testing.T) {
	p := writeTmp(t, "a.txt", "first\n\nthird\n")
	_, out := runCat(t, nil, "-b", p)
	want := "     1\tfirst\n\n     2\tthird\n"
	if out != want {
		t.Errorf("got %q want %q", out, want)
	}
}

func TestBOverridesN(t *testing.T) {
	p := writeTmp(t, "a.txt", "x\n\ny\n")
	_, out := runCat(t, nil, "-nb", p)
	want := "     1\tx\n\n     2\ty\n"
	if out != want {
		t.Errorf("got %q want %q", out, want)
	}
}

func TestSqueezeBlank(t *testing.T) {
	p := writeTmp(t, "a.txt", "a\n\n\n\nb\n")
	_, out := runCat(t, nil, "-s", p)
	if out != "a\n\nb\n" {
		t.Errorf("got %q", out)
	}
}

func TestShowEnds(t *testing.T) {
	p := writeTmp(t, "a.txt", "x\ny\n")
	_, out := runCat(t, nil, "-E", p)
	if out != "x$\ny$\n" {
		t.Errorf("got %q", out)
	}
}

func TestShowTabs(t *testing.T) {
	p := writeTmp(t, "a.txt", "a\tb\n")
	_, out := runCat(t, nil, "-T", p)
	if out != "a^Ib\n" {
		t.Errorf("got %q", out)
	}
}

func TestShowAll(t *testing.T) {
	p := writeTmp(t, "a.txt", "a\tb\x01c\n")
	_, out := runCat(t, nil, "-A", p)
	if out != "a^Ib^Ac$\n" {
		t.Errorf("got %q", out)
	}
}

func TestNonprintingHighBit(t *testing.T) {
	p := writeTmp(t, "a.txt", "\xff\n")
	_, out := runCat(t, nil, "-v", p)
	if out != "M-^?\n" {
		t.Errorf("got %q", out)
	}
}

func TestNonprintingDel(t *testing.T) {
	p := writeTmp(t, "a.txt", "\x7f\n")
	_, out := runCat(t, nil, "-v", p)
	if out != "^?\n" {
		t.Errorf("got %q", out)
	}
}

func TestTabPreservedWithoutT(t *testing.T) {
	p := writeTmp(t, "a.txt", "a\tb\n")
	_, out := runCat(t, nil, p)
	if out != "a\tb\n" {
		t.Errorf("got %q", out)
	}
	// -v alone keeps tab as tab.
	_, out = runCat(t, nil, "-v", p)
	if out != "a\tb\n" {
		t.Errorf("got %q (with -v)", out)
	}
}

func TestNonexistent(t *testing.T) {
	exit, _ := runCat(t, nil, "/does/not/exist/foo")
	if exit == 0 {
		t.Errorf("exit=0 expected non-zero")
	}
}

func TestUnknownOption(t *testing.T) {
	exit, _ := runCat(t, nil, "--no-such")
	if exit == 0 {
		t.Errorf("exit=0 expected non-zero")
	}
}

func TestBinaryPassthrough(t *testing.T) {
	// Without flags, cat must not alter bytes (memory-safe but byte-faithful).
	data := make([]byte, 256)
	for i := range data {
		data[i] = byte(i)
	}
	p := writeTmp(t, "a.bin", string(data))
	_, out := runCat(t, nil, p)
	if !bytes.Equal([]byte(out), data) {
		t.Errorf("binary roundtrip mismatch (len=%d)", len(out))
	}
}

func TestNonewlineLastLine(t *testing.T) {
	p := writeTmp(t, "a.txt", "a\nb")
	_, out := runCat(t, nil, "-n", p)
	want := "     1\ta\n     2\tb"
	if out != want {
		t.Errorf("got %q want %q", out, want)
	}
}

func TestDashDashEndsFlags(t *testing.T) {
	p := writeTmp(t, "-n", "literal-n-file\n")
	exit, out := runCat(t, nil, "--", p)
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if !strings.Contains(out, "literal-n-file") {
		t.Errorf("got %q", out)
	}
}
