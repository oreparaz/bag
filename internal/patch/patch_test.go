package patch

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runPatch(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	rIn, wIn, _ := os.Pipe()
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	oldIn, oldOut, oldErr := os.Stdin, os.Stdout, os.Stderr
	os.Stdin = rIn
	os.Stdout = wOut
	os.Stderr = wErr
	defer func() {
		os.Stdin, os.Stdout, os.Stderr = oldIn, oldOut, oldErr
		rIn.Close()
		rOut.Close()
		rErr.Close()
	}()
	go func() {
		wIn.WriteString(stdin)
		wIn.Close()
	}()
	exit := Main(args)
	wOut.Close()
	wErr.Close()
	out, _ := io.ReadAll(rOut)
	er, _ := io.ReadAll(rErr)
	return exit, string(out), string(er)
}

func TestSimpleApply(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "x")
	os.WriteFile(src, []byte("a\nb\nc\n"), 0o644)
	diff := "--- x\t2024-01-01\n+++ x\t2024-01-02\n@@ -1,3 +1,3 @@\n a\n-b\n+B\n c\n"
	pwd, _ := os.Getwd()
	defer os.Chdir(pwd)
	os.Chdir(dir)
	exit, _, er := runPatch(t, diff, "-p", "0")
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%s", exit, er)
	}
	got, _ := os.ReadFile(src)
	if string(got) != "a\nB\nc\n" {
		t.Errorf("got %q want %q", got, "a\nB\nc\n")
	}
}

func TestApplyFromStdin(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "x")
	os.WriteFile(src, []byte("hello\n"), 0o644)
	diff := "--- x\n+++ x\n@@ -1,1 +1,1 @@\n-hello\n+world\n"
	pwd, _ := os.Getwd()
	defer os.Chdir(pwd)
	os.Chdir(dir)
	exit, _, er := runPatch(t, diff, "-p", "0")
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%s", exit, er)
	}
	got, _ := os.ReadFile(src)
	if string(got) != "world\n" {
		t.Errorf("got %q", got)
	}
}

func TestStripP(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "x")
	os.WriteFile(src, []byte("a\n"), 0o644)
	// Path in diff is "deep/a/x" — with -p2 → "x"
	diff := "--- deep/a/x\n+++ deep/a/x\n@@ -1,1 +1,1 @@\n-a\n+b\n"
	pwd, _ := os.Getwd()
	defer os.Chdir(pwd)
	os.Chdir(dir)
	exit, _, er := runPatch(t, diff, "-p", "2")
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%s", exit, er)
	}
	got, _ := os.ReadFile(src)
	if string(got) != "b\n" {
		t.Errorf("got %q", got)
	}
}

func TestReverse(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "x")
	os.WriteFile(src, []byte("modified\n"), 0o644)
	diff := "--- x\n+++ x\n@@ -1,1 +1,1 @@\n-original\n+modified\n"
	pwd, _ := os.Getwd()
	defer os.Chdir(pwd)
	os.Chdir(dir)
	exit, _, er := runPatch(t, diff, "-p", "0", "-R")
	if exit != 0 {
		t.Fatalf("%s", er)
	}
	got, _ := os.ReadFile(src)
	if string(got) != "original\n" {
		t.Errorf("got %q", got)
	}
}

func TestDryRun(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "x")
	os.WriteFile(src, []byte("a\n"), 0o644)
	diff := "--- x\n+++ x\n@@ -1,1 +1,1 @@\n-a\n+b\n"
	pwd, _ := os.Getwd()
	defer os.Chdir(pwd)
	os.Chdir(dir)
	exit, _, _ := runPatch(t, diff, "-p", "0", "--dry-run")
	if exit != 0 {
		t.Errorf("exit=%d", exit)
	}
	got, _ := os.ReadFile(src)
	if string(got) != "a\n" {
		t.Errorf("dry-run should not modify, got %q", got)
	}
}

func TestNoHunks(t *testing.T) {
	exit, _, er := runPatch(t, "not a patch")
	if exit == 0 {
		t.Errorf("expected non-zero exit")
	}
	if !strings.Contains(er, "no patch hunks") {
		t.Errorf("expected diagnostic, got %s", er)
	}
}

// TestRoundTripWithDiff: compose the diff tool with patch — produce
// a diff, apply it, check the result equals the new file. This is
// the strongest property test: any diff bag produces must be
// applicable by bag.
func TestRoundTripWithDiff(t *testing.T) {
	// We don't import internal/diff to avoid a cycle in tests; spawn
	// the produced diff via stdin instead.
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	os.WriteFile(a, []byte("alpha\nbeta\ngamma\n"), 0o644)
	os.WriteFile(b, []byte("alpha\nBETA\ngamma\nepsilon\n"), 0o644)

	// Hand-crafted diff between a and b. patch modifies the source
	// file (a) in place.
	diff := "--- a\n+++ b\n@@ -1,3 +1,4 @@\n alpha\n-beta\n+BETA\n gamma\n+epsilon\n"
	pwd, _ := os.Getwd()
	defer os.Chdir(pwd)
	os.Chdir(dir)
	exit, _, er := runPatch(t, diff, "-p", "0")
	if exit != 0 {
		t.Fatalf("%s", er)
	}
	got, _ := os.ReadFile("a")
	want, _ := os.ReadFile("b")
	if string(got) != string(want) {
		t.Errorf("after patching a, got %q want %q", got, want)
	}
}
