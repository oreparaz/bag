package head

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runHead(t *testing.T, stdin []byte, args ...string) (int, string) {
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

func tenLines() string {
	return strings.Join([]string{"l1", "l2", "l3", "l4", "l5", "l6", "l7", "l8", "l9", "l10", "l11", "l12"}, "\n") + "\n"
}

func TestDefaultTenLines(t *testing.T) {
	p := writeTmp(t, "a.txt", tenLines())
	_, out := runHead(t, nil, p)
	want := "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\n"
	if out != want {
		t.Errorf("got %q want %q", out, want)
	}
}

func TestNFlag(t *testing.T) {
	p := writeTmp(t, "a.txt", tenLines())
	_, out := runHead(t, nil, "-n", "3", p)
	if out != "l1\nl2\nl3\n" {
		t.Errorf("got %q", out)
	}
	_, out = runHead(t, nil, "-n3", p)
	if out != "l1\nl2\nl3\n" {
		t.Errorf("glued: got %q", out)
	}
}

func TestNNegative(t *testing.T) {
	p := writeTmp(t, "a.txt", "a\nb\nc\nd\ne\n")
	_, out := runHead(t, nil, "-n", "-2", p)
	if out != "a\nb\nc\n" {
		t.Errorf("got %q", out)
	}
}

func TestCBytes(t *testing.T) {
	p := writeTmp(t, "a.txt", "abcdefghij")
	_, out := runHead(t, nil, "-c", "4", p)
	if out != "abcd" {
		t.Errorf("got %q", out)
	}
}

func TestCBytesNegative(t *testing.T) {
	p := writeTmp(t, "a.txt", "abcdefghij")
	_, out := runHead(t, nil, "-c", "-3", p)
	if out != "abcdefg" {
		t.Errorf("got %q", out)
	}
}

func TestCSuffixK(t *testing.T) {
	data := bytes.Repeat([]byte{'a'}, 4096)
	p := writeTmp(t, "a.bin", string(data))
	_, out := runHead(t, nil, "-c", "1K", p)
	if len(out) != 1024 {
		t.Errorf("len=%d want 1024", len(out))
	}
}

func TestStdinDefault(t *testing.T) {
	in := tenLines()
	_, out := runHead(t, []byte(in))
	want := "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\n"
	if out != want {
		t.Errorf("got %q", out)
	}
}

func TestMultipleFilesHeaders(t *testing.T) {
	a := writeTmp(t, "a.txt", "AAA\n")
	b := writeTmp(t, "b.txt", "BBB\n")
	_, out := runHead(t, nil, "-n", "1", a, b)
	if !strings.Contains(out, "==> "+a+" <==\nAAA\n") {
		t.Errorf("missing header A; got %q", out)
	}
	if !strings.Contains(out, "==> "+b+" <==\nBBB\n") {
		t.Errorf("missing header B; got %q", out)
	}
}

func TestQuiet(t *testing.T) {
	a := writeTmp(t, "a.txt", "AAA\n")
	b := writeTmp(t, "b.txt", "BBB\n")
	_, out := runHead(t, nil, "-q", "-n", "1", a, b)
	if strings.Contains(out, "==>") {
		t.Errorf("quiet should suppress headers; got %q", out)
	}
}

func TestVerboseSingleFile(t *testing.T) {
	p := writeTmp(t, "a.txt", "AAA\n")
	_, out := runHead(t, nil, "-v", "-n", "1", p)
	if !strings.Contains(out, "==> "+p+" <==") {
		t.Errorf("verbose should show header; got %q", out)
	}
}

func TestNoFinalNewline(t *testing.T) {
	p := writeTmp(t, "a.txt", "a\nb")
	_, out := runHead(t, nil, "-n", "5", p)
	if out != "a\nb" {
		t.Errorf("got %q", out)
	}
}

func TestNonexistent(t *testing.T) {
	exit, _ := runHead(t, nil, "/no/such/file")
	if exit == 0 {
		t.Errorf("exit=0 expected non-zero")
	}
}

func TestInvalidCount(t *testing.T) {
	exit, _ := runHead(t, nil, "-n", "abc")
	if exit == 0 {
		t.Errorf("exit=0 expected non-zero on bad count")
	}
}

func TestParseCount(t *testing.T) {
	cases := map[string]int64{
		"10":   10,
		"-10":  -10,
		"1K":   1024,
		"2k":   2048,
		"3M":   3 * 1024 * 1024,
		"4b":   4 * 512,
		"-1K":  -1024,
	}
	for in, want := range cases {
		got, err := parseCount(in)
		if err != nil {
			t.Errorf("parseCount(%q) error: %v", in, err)
		}
		if got != want {
			t.Errorf("parseCount(%q) = %d want %d", in, got, want)
		}
	}
}
