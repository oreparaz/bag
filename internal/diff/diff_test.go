package diff

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runDiff(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout = wOut
	os.Stderr = wErr
	defer func() {
		os.Stdout, os.Stderr = oldOut, oldErr
		rOut.Close()
		rErr.Close()
	}()
	exit := Main(args)
	wOut.Close()
	wErr.Close()
	out, _ := io.ReadAll(rOut)
	er, _ := io.ReadAll(rErr)
	return exit, string(out), string(er)
}

func TestEqualFiles(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	os.WriteFile(a, []byte("hello\nworld\n"), 0o644)
	os.WriteFile(b, []byte("hello\nworld\n"), 0o644)
	exit, out, _ := runDiff(t, a, b)
	if exit != 0 {
		t.Errorf("got exit %d", exit)
	}
	if out != "" {
		t.Errorf("expected no output, got %q", out)
	}
}

func TestSimpleEdit(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	os.WriteFile(a, []byte("alpha\nbeta\ngamma\n"), 0o644)
	os.WriteFile(b, []byte("alpha\nBETA\ngamma\n"), 0o644)
	exit, out, _ := runDiff(t, a, b)
	if exit != 1 {
		t.Errorf("got exit %d want 1", exit)
	}
	if !strings.Contains(out, "@@") {
		t.Errorf("expected hunk header, got %q", out)
	}
	if !strings.Contains(out, "-beta") || !strings.Contains(out, "+BETA") {
		t.Errorf("expected -/+ lines, got %q", out)
	}
}

func TestAddedLines(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	os.WriteFile(a, []byte("one\ntwo\n"), 0o644)
	os.WriteFile(b, []byte("one\ntwo\nthree\n"), 0o644)
	exit, out, _ := runDiff(t, a, b)
	if exit != 1 {
		t.Errorf("got exit %d", exit)
	}
	if !strings.Contains(out, "+three") {
		t.Errorf("expected +three, got %q", out)
	}
}

func TestRemovedLines(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	os.WriteFile(a, []byte("one\ntwo\nthree\n"), 0o644)
	os.WriteFile(b, []byte("one\nthree\n"), 0o644)
	_, out, _ := runDiff(t, a, b)
	if !strings.Contains(out, "-two") {
		t.Errorf("expected -two, got %q", out)
	}
}

func TestBrief(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	os.WriteFile(a, []byte("x\n"), 0o644)
	os.WriteFile(b, []byte("y\n"), 0o644)
	_, out, _ := runDiff(t, "-q", a, b)
	if !strings.Contains(out, "differ") || !strings.Contains(out, a) {
		t.Errorf("brief output unexpected: %q", out)
	}
}

func TestBinaryFile(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	os.WriteFile(a, []byte{0, 1, 2}, 0o644)
	os.WriteFile(b, []byte{0, 3, 2}, 0o644)
	_, out, _ := runDiff(t, a, b)
	if !strings.Contains(out, "Binary files") {
		t.Errorf("expected binary-files line, got %q", out)
	}
}

func TestIgnoreCase(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	os.WriteFile(a, []byte("Hello\n"), 0o644)
	os.WriteFile(b, []byte("hello\n"), 0o644)
	exit, _, _ := runDiff(t, "-i", a, b)
	if exit != 0 {
		t.Errorf("-i should make these equal; exit=%d", exit)
	}
}

func TestRecursiveDir(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	os.MkdirAll(a, 0o755)
	os.MkdirAll(b, 0o755)
	os.WriteFile(filepath.Join(a, "f"), []byte("X\n"), 0o644)
	os.WriteFile(filepath.Join(b, "f"), []byte("Y\n"), 0o644)
	os.WriteFile(filepath.Join(a, "only-in-a"), []byte("\n"), 0o644)
	exit, out, _ := runDiff(t, "-r", a, b)
	if exit != 1 {
		t.Errorf("got exit %d want 1", exit)
	}
	if !strings.Contains(out, "Only in") {
		t.Errorf("expected 'Only in' line, got %q", out)
	}
	if !strings.Contains(out, "-X") || !strings.Contains(out, "+Y") {
		t.Errorf("expected X/Y diff inside dir, got %q", out)
	}
}

func TestNoNewline(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	os.WriteFile(a, []byte("hello"), 0o644) // no \n
	os.WriteFile(b, []byte("world"), 0o644)
	_, out, _ := runDiff(t, a, b)
	if !strings.Contains(out, "No newline at end of file") {
		t.Errorf("expected no-newline marker, got %q", out)
	}
}

func TestReportIdentical(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	os.WriteFile(a, []byte("same\n"), 0o644)
	os.WriteFile(b, []byte("same\n"), 0o644)
	_, out, _ := runDiff(t, "-s", a, b)
	if !strings.Contains(out, "identical") {
		t.Errorf("expected 'identical', got %q", out)
	}
}
