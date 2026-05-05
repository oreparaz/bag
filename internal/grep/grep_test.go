package grep

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runGrep(t *testing.T, stdin []byte, args ...string) (int, string) {
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

func TestBasicMatch(t *testing.T) {
	exit, out := runGrep(t, []byte("alpha\nbeta\ngamma\n"), "be")
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if out != "beta\n" {
		t.Errorf("got %q", out)
	}
}

func TestNoMatchExit1(t *testing.T) {
	exit, out := runGrep(t, []byte("x\n"), "z")
	if exit != 1 {
		t.Errorf("exit=%d want 1", exit)
	}
	if out != "" {
		t.Errorf("expected empty stdout, got %q", out)
	}
}

func TestIgnoreCase(t *testing.T) {
	_, out := runGrep(t, []byte("Foo\nbar\n"), "-i", "FOO")
	if out != "Foo\n" {
		t.Errorf("got %q", out)
	}
}

func TestInvertMatch(t *testing.T) {
	_, out := runGrep(t, []byte("a\nb\nc\n"), "-v", "b")
	if out != "a\nc\n" {
		t.Errorf("got %q", out)
	}
}

func TestCount(t *testing.T) {
	_, out := runGrep(t, []byte("xa\nxb\nyc\n"), "-c", "x")
	if strings.TrimRight(out, "\n") != "2" {
		t.Errorf("got %q", out)
	}
}

func TestLineNumber(t *testing.T) {
	_, out := runGrep(t, []byte("aa\nbb\ncc\n"), "-n", "bb")
	if out != "2:bb\n" {
		t.Errorf("got %q", out)
	}
}

func TestFixedStringEscapesRegex(t *testing.T) {
	exit, out := runGrep(t, []byte("a.b\nazb\n"), "-F", "a.b")
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if out != "a.b\n" {
		t.Errorf("-F should literal-match, got %q", out)
	}
}

func TestWordRegexp(t *testing.T) {
	_, out := runGrep(t, []byte("foobar\nfoo bar\n"), "-w", "foo")
	if out != "foo bar\n" {
		t.Errorf("got %q", out)
	}
}

func TestLineRegexp(t *testing.T) {
	_, out := runGrep(t, []byte("ab\na\nabc\n"), "-x", "a")
	if out != "a\n" {
		t.Errorf("got %q", out)
	}
}

func TestQuietExitOnMatch(t *testing.T) {
	exit, out := runGrep(t, []byte("zzz\n"), "-q", "z")
	if exit != 0 || out != "" {
		t.Errorf("exit=%d out=%q", exit, out)
	}
}

func TestMultipleFilesPrefixesName(t *testing.T) {
	a := writeTmp(t, "a", "ham\n")
	b := writeTmp(t, "b", "ham\n")
	_, out := runGrep(t, nil, "ham", a, b)
	if !strings.Contains(out, a+":ham") || !strings.Contains(out, b+":ham") {
		t.Errorf("got %q", out)
	}
}

func TestRecursiveWithInclude(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hit\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "sub", "b.go"), []byte("hit\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "sub", "c.txt"), []byte("hit\n"), 0o644)

	exit, out := runGrep(t, nil, "-r", "--include=*.txt", "hit", dir)
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if !strings.Contains(out, "a.txt:hit") {
		t.Errorf("missing a.txt: %q", out)
	}
	if !strings.Contains(out, "c.txt:hit") {
		t.Errorf("missing c.txt: %q", out)
	}
	if strings.Contains(out, "b.go") {
		t.Errorf("b.go should be filtered: %q", out)
	}
}

func TestContext(t *testing.T) {
	_, out := runGrep(t, []byte("a\nb\nMATCH\nd\ne\n"), "-C", "1", "MATCH")
	want := "b\nMATCH\nd\n"
	if out != want {
		t.Errorf("got %q want %q", out, want)
	}
}

func TestFilesWithMatches(t *testing.T) {
	a := writeTmp(t, "a", "yes\n")
	b := writeTmp(t, "b", "no\n")
	_, out := runGrep(t, nil, "-l", "yes", a, b)
	if !strings.Contains(out, a) {
		t.Errorf("got %q", out)
	}
	if strings.Contains(out, b) {
		t.Errorf("b should not be reported: %q", out)
	}
}

func TestFilesWithoutMatch(t *testing.T) {
	a := writeTmp(t, "a", "yes\n")
	b := writeTmp(t, "b", "no\n")
	_, out := runGrep(t, nil, "-L", "yes", a, b)
	if strings.Contains(out, a) {
		t.Errorf("got %q", out)
	}
	if !strings.Contains(out, b) {
		t.Errorf("b expected: %q", out)
	}
}

func TestPatternFromMultipleE(t *testing.T) {
	_, out := runGrep(t, []byte("aa\nbb\ncc\n"), "-e", "aa", "-e", "cc")
	if out != "aa\ncc\n" {
		t.Errorf("got %q", out)
	}
}
