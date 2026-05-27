package cmp

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runCMP(t *testing.T, args ...string) (int, string, string) {
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
	os.WriteFile(a, []byte("hello"), 0o644)
	os.WriteFile(b, []byte("hello"), 0o644)
	exit, out, er := runCMP(t, a, b)
	if exit != 0 {
		t.Errorf("equal files: exit=%d stderr=%s", exit, er)
	}
	if out != "" {
		t.Errorf("equal files should be silent, got %q", out)
	}
}

func TestDifferAtByte(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	os.WriteFile(a, []byte("hello world\n"), 0o644)
	os.WriteFile(b, []byte("hello WORLD\n"), 0o644)
	exit, out, _ := runCMP(t, a, b)
	if exit != 1 {
		t.Errorf("expected exit 1, got %d", exit)
	}
	if !strings.Contains(out, "byte 7") {
		t.Errorf("expected byte 7 in output, got %q", out)
	}
}

func TestSilent(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	os.WriteFile(a, []byte("x"), 0o644)
	os.WriteFile(b, []byte("y"), 0o644)
	exit, out, _ := runCMP(t, "-s", a, b)
	if exit != 1 {
		t.Errorf("got exit %d", exit)
	}
	if out != "" {
		t.Errorf("silent should produce no output, got %q", out)
	}
}

func TestVerbose(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	os.WriteFile(a, []byte("axc"), 0o644)
	os.WriteFile(b, []byte("ayc"), 0o644)
	_, out, _ := runCMP(t, "-l", a, b)
	if !strings.HasPrefix(strings.TrimSpace(out), "2") {
		t.Errorf("verbose output should start with byte 2: %q", out)
	}
}

func TestEOFOnShorter(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	os.WriteFile(a, []byte("short"), 0o644)
	os.WriteFile(b, []byte("shortextra"), 0o644)
	exit, _, er := runCMP(t, a, b)
	if exit != 1 {
		t.Errorf("got exit %d", exit)
	}
	if !strings.Contains(er, "EOF") {
		t.Errorf("expected EOF diagnostic, got %q", er)
	}
}

func TestMissingFile(t *testing.T) {
	exit, _, _ := runCMP(t, "/nonexistent", "/nonexistent2")
	if exit != 2 {
		t.Errorf("got exit %d want 2", exit)
	}
}

func TestSkipBytes(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	os.WriteFile(a, []byte("HEAD-same"), 0o644)
	os.WriteFile(b, []byte("HXXX-same"), 0o644) // first 4 differ, last 5 same
	exit, _, _ := runCMP(t, "-i", "4", a, b)
	if exit != 0 {
		t.Errorf("got exit %d want 0", exit)
	}
}

func TestMaxBytes(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	os.WriteFile(a, []byte("equal-then-diff"), 0o644)
	os.WriteFile(b, []byte("equal-then-DIFF"), 0o644)
	exit, _, _ := runCMP(t, "-n", "5", a, b)
	if exit != 0 {
		t.Errorf("got exit %d want 0 (first 5 bytes match)", exit)
	}
}
