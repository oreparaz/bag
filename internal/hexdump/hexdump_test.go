package hexdump

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runHD(t *testing.T, stdin []byte, args ...string) (int, string) {
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

func writeTmp(t *testing.T, name string, data []byte) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCanonical(t *testing.T) {
	_, out := runHD(t, []byte("hello world\n"), "-C")
	want := "00000000  68 65 6c 6c 6f 20 77 6f  72 6c 64 0a              |hello world.|\n0000000c\n"
	if out != want {
		t.Errorf("got %q\nwant %q", out, want)
	}
}

func TestDefaultTwoByteHex(t *testing.T) {
	_, out := runHD(t, []byte("hello world\n"))
	if !strings.Contains(out, "0000000 6568 6c6c") {
		t.Errorf("got %q", out)
	}
}

func TestSqueeze(t *testing.T) {
	// 64 zero bytes -> 4 identical 16-byte rows -> first row + "*" + final offset.
	zeros := make([]byte, 64)
	_, out := runHD(t, zeros, "-C")
	if !strings.Contains(out, "*\n") {
		t.Errorf("expected '*' collapsing line: %q", out)
	}
}

func TestVerboseDoesNotSqueeze(t *testing.T) {
	zeros := make([]byte, 64)
	_, out := runHD(t, zeros, "-Cv")
	if strings.Contains(out, "*\n") {
		t.Errorf("with -v we should NOT collapse: %q", out)
	}
}

func TestSkipAndLimit(t *testing.T) {
	p := writeTmp(t, "blob", []byte("0123456789ABCDEF"))
	_, out := runHD(t, nil, "-C", "-s", "8", "-n", "4", p)
	if !strings.Contains(out, "00000008  38 39 41 42") {
		t.Errorf("got %q", out)
	}
}

func TestOneByteChar(t *testing.T) {
	_, out := runHD(t, []byte("ab\n"), "-c")
	if !strings.Contains(out, "   a   b  \\n") {
		t.Errorf("got %q", out)
	}
}
