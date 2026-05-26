package sed

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func runSed(t *testing.T, stdin []byte, args ...string) (int, string) {
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

func TestSubstituteFirst(t *testing.T) {
	_, out := runSed(t, []byte("xxx\n"), "s/x/y/")
	if out != "yxx\n" {
		t.Errorf("got %q", out)
	}
}

func TestSubstituteGlobal(t *testing.T) {
	_, out := runSed(t, []byte("xxx\n"), "s/x/y/g")
	if out != "yyy\n" {
		t.Errorf("got %q", out)
	}
}

func TestSubstituteIcase(t *testing.T) {
	_, out := runSed(t, []byte("AbC\n"), "s/abc/xyz/i")
	if out != "xyz\n" {
		t.Errorf("got %q", out)
	}
}

func TestAlternateDelimiter(t *testing.T) {
	_, out := runSed(t, []byte("/usr/bin\n"), "s|/usr|/opt|")
	if out != "/opt/bin\n" {
		t.Errorf("got %q", out)
	}
}

func TestBackreference(t *testing.T) {
	// sed's replacement syntax is \1..\9 and &, not Go's $1.
	// Default mode is BRE: groups are \(...\) and (...) is literal.
	// First-match path, BRE:
	_, out := runSed(t, []byte("alice 30\n"), `s/\(\w\+\) \(\d\+\)/\2 \1/`)
	if out != "30 alice\n" {
		t.Errorf("BRE first-match: got %q", out)
	}
	// First-match path, ERE (with -E): parens are unescaped.
	_, out = runSed(t, []byte("alice 30\n"), "-E", `s/(\w+) (\d+)/\2 \1/`)
	if out != "30 alice\n" {
		t.Errorf("ERE first-match: got %q", out)
	}
	// & expands to the whole match:
	_, out = runSed(t, []byte("foo bar\n"), `s/bar/[&]/`)
	if out != "foo [bar]\n" {
		t.Errorf("ampersand: got %q", out)
	}
	// /g path uses the same expander:
	_, out = runSed(t, []byte("a1 b2\n"), "-E", `s/(\w)(\d)/\2\1/g`)
	if out != "1a 2b\n" {
		t.Errorf("global: got %q", out)
	}
}

func TestDeleteLine(t *testing.T) {
	_, out := runSed(t, []byte("a\nb\nc\n"), "/b/d")
	if out != "a\nc\n" {
		t.Errorf("got %q", out)
	}
}

func TestPrintWithSuppress(t *testing.T) {
	_, out := runSed(t, []byte("a\nb\nc\n"), "-n", "2p")
	if out != "b\n" {
		t.Errorf("got %q", out)
	}
}

func TestQuit(t *testing.T) {
	_, out := runSed(t, []byte("a\nb\nc\n"), "2q")
	if out != "a\nb\n" {
		t.Errorf("got %q", out)
	}
}

func TestLineRange(t *testing.T) {
	_, out := runSed(t, []byte("a\nb\nc\nd\n"), "2,3d")
	if out != "a\nd\n" {
		t.Errorf("got %q", out)
	}
}

func TestLastLineAddr(t *testing.T) {
	_, out := runSed(t, []byte("a\nb\nc\n"), "$d")
	if out != "a\nb\n" {
		t.Errorf("got %q", out)
	}
}

func TestRegexAddrRange(t *testing.T) {
	in := "out\nstart\nin1\nin2\nend\nout\n"
	_, out := runSed(t, []byte(in), "/start/,/end/d")
	if out != "out\nout\n" {
		t.Errorf("got %q", out)
	}
}

func TestMultipleScripts(t *testing.T) {
	_, out := runSed(t, []byte("foo bar\n"), "-e", "s/foo/F/", "-e", "s/bar/B/")
	if out != "F B\n" {
		t.Errorf("got %q", out)
	}
}

func TestSemicolonSeparated(t *testing.T) {
	_, out := runSed(t, []byte("a\nb\nc\n"), "s/a/A/; s/b/B/")
	if out != "A\nB\nc\n" {
		t.Errorf("got %q", out)
	}
}

func TestInPlace(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	os.WriteFile(p, []byte("hello\n"), 0o644)

	exit := Main([]string{"-i", "s/hello/world/", p})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	body, _ := os.ReadFile(p)
	if string(body) != "world\n" {
		t.Errorf("got %q", body)
	}
}

func TestInPlaceWithBackup(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.txt")
	os.WriteFile(p, []byte("hello\n"), 0o644)

	exit := Main([]string{"-i.bak", "s/hello/bye/", p})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	body, _ := os.ReadFile(p)
	if string(body) != "bye\n" {
		t.Errorf("primary=%q", body)
	}
	bk, err := os.ReadFile(p + ".bak")
	if err != nil || string(bk) != "hello\n" {
		t.Errorf("backup=%q err=%v", bk, err)
	}
}

// TestInPlacePreservesMode: a 0600-mode file should remain 0600 after
// `sed -i` rewrites it. The original code clobbered modes to 0644.
func TestInPlacePreservesMode(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "secret.conf")
	os.WriteFile(p, []byte("hello\n"), 0o600)

	exit := Main([]string{"-i", "s/hello/world/", p})
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode after -i = %o, want 0600", info.Mode().Perm())
	}
}

func TestYTransliterate(t *testing.T) {
	// y is now supported — it transliterates each char in src to the
	// corresponding char in dst.
	_, out := runSed(t, []byte("hello\n"), "y/abcdefghijklmnopqrstuvwxyz/ABCDEFGHIJKLMNOPQRSTUVWXYZ/")
	if out != "HELLO\n" {
		t.Errorf("got %q", out)
	}
}

// Regression for the kernel-build asm-offsets script: default sed mode
// is BRE (\( \) are groups), `s` delimiter survives inside [[:space:]],
// blocks `/RE/{cmd1;cmd2;...}` work, and -n + s///p only prints when a
// substitution actually happened.
func TestKernelAsmOffsets(t *testing.T) {
	in := "->FOO #42 sizeof(foo)\n->BAR #44 sizeof(bar)\n"
	script := `s:^[[:space:]]*\.ascii[[:space:]]*"\(.*\)".*:\1:; /^->/{s:->#\(.*\):/* \1 */:; s:^->\([^ ]*\) [\$#]*\([^ ]*\) \(.*\):#define \1 \2 /* \3 */:; s:->::; p;}`
	_, out := runSed(t, []byte(in), "-n", script)
	want := "#define FOO 42 /* sizeof(foo) */\n#define BAR 44 /* sizeof(bar) */\n"
	if out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestSubstNFlagOnlyPrintsMatches(t *testing.T) {
	// `sed -n s/a/A/p` should print only lines where a substitution
	// actually happened — not every line.
	_, out := runSed(t, []byte("alpha\nbeta\ngamma\n"), "-n", "s/a/A/p")
	want := "Alpha\nbetA\ngAmma\n"
	if out != want {
		t.Errorf("got %q want %q", out, want)
	}
}

func TestUnsupportedCommand(t *testing.T) {
	exit, _ := runSed(t, []byte("a\n"), "Q") // capital Q isn't implemented
	if exit == 0 {
		t.Errorf("expected non-zero on unsupported command")
	}
}
