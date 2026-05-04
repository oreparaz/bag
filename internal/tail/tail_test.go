package tail

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runTail(t *testing.T, stdin []byte, args ...string) (int, string) {
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

func ten() string {
	return strings.Join([]string{"l1", "l2", "l3", "l4", "l5", "l6", "l7", "l8", "l9", "l10", "l11", "l12"}, "\n") + "\n"
}

func TestDefaultLast10(t *testing.T) {
	p := writeTmp(t, "a.txt", ten())
	_, out := runTail(t, nil, p)
	want := "l3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\nl11\nl12\n"
	if out != want {
		t.Errorf("got %q", out)
	}
}

func TestNLast(t *testing.T) {
	p := writeTmp(t, "a.txt", ten())
	_, out := runTail(t, nil, "-n", "3", p)
	if out != "l10\nl11\nl12\n" {
		t.Errorf("got %q", out)
	}
}

func TestNFromStart(t *testing.T) {
	p := writeTmp(t, "a.txt", "a\nb\nc\nd\ne\n")
	_, out := runTail(t, nil, "-n", "+3", p)
	if out != "c\nd\ne\n" {
		t.Errorf("got %q", out)
	}
}

func TestCBytesLast(t *testing.T) {
	p := writeTmp(t, "a.txt", "0123456789")
	_, out := runTail(t, nil, "-c", "3", p)
	if out != "789" {
		t.Errorf("got %q", out)
	}
}

func TestCBytesFromStart(t *testing.T) {
	p := writeTmp(t, "a.txt", "0123456789")
	_, out := runTail(t, nil, "-c", "+5", p)
	if out != "456789" {
		t.Errorf("got %q", out)
	}
}

func TestStdin(t *testing.T) {
	_, out := runTail(t, []byte(ten()), "-n", "2")
	if out != "l11\nl12\n" {
		t.Errorf("got %q", out)
	}
}

func TestMultipleFiles(t *testing.T) {
	a := writeTmp(t, "a.txt", "AAA\n")
	b := writeTmp(t, "b.txt", "BBB\n")
	_, out := runTail(t, nil, "-n", "1", a, b)
	if !strings.Contains(out, "==> "+a+" <==\nAAA\n") {
		t.Errorf("missing A header; got %q", out)
	}
	if !strings.Contains(out, "==> "+b+" <==\nBBB\n") {
		t.Errorf("missing B header; got %q", out)
	}
}

func TestN0(t *testing.T) {
	p := writeTmp(t, "a.txt", ten())
	_, out := runTail(t, nil, "-n", "0", p)
	if out != "" {
		t.Errorf("got %q want empty", out)
	}
}

func TestParseTailCount(t *testing.T) {
	type want struct {
		n  int64
		fs bool
	}
	cases := map[string]want{
		"10":  {10, false},
		"-5":  {5, false},
		"+3":  {3, true},
		"1K":  {1024, false},
		"+2K": {2048, true},
	}
	for in, w := range cases {
		n, fs, err := parseTailCount(in)
		if err != nil {
			t.Errorf("%q: error %v", in, err)
		}
		if n != w.n || fs != w.fs {
			t.Errorf("%q: got (%d, %v) want (%d, %v)", in, n, fs, w.n, w.fs)
		}
	}
}

func TestRejectFollow(t *testing.T) {
	exit, _ := runTail(t, []byte("x\n"), "-f")
	if exit == 0 {
		t.Errorf("expected non-zero exit when -f passed")
	}
}

func TestNonexistent(t *testing.T) {
	exit, _ := runTail(t, nil, "/no/such/path")
	if exit == 0 {
		t.Errorf("exit=0 expected non-zero")
	}
}
