package ls

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// runLS calls Main with the given args and captures stdout + stderr.
// Returns exit code and the two streams.
func runLS(t *testing.T, args ...string) (int, []byte, []byte) {
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
	return exit, out, er
}

func TestListsDirectoryAlphabetically(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"banana", "apple", "cherry"} {
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	exit, out, _ := runLS(t, dir)
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	got := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	want := []string{"apple", "banana", "cherry"}
	if !equalStrings(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestSkipsDotfilesByDefault(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".hidden"), nil, 0o600)
	os.WriteFile(filepath.Join(dir, "visible"), nil, 0o600)
	_, out, _ := runLS(t, dir)
	got := strings.TrimRight(string(out), "\n")
	if got != "visible" {
		t.Errorf("dotfile leaked or visible dropped: %q", got)
	}
}

func TestAllIncludesDotAndDotDot(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "x"), nil, 0o600)
	_, out, _ := runLS(t, "-a", dir)
	got := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(got) != 3 || got[0] != "." || got[1] != ".." {
		t.Errorf("-a missing . / ..: %v", got)
	}
}

func TestAlmostAllExcludesDotAndDotDot(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".hidden"), nil, 0o600)
	os.WriteFile(filepath.Join(dir, "v"), nil, 0o600)
	_, out, _ := runLS(t, "-A", dir)
	got := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	want := []string{".hidden", "v"}
	if !equalStrings(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestLongFormatMode(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x"), []byte("hi"), 0o640); err != nil {
		t.Fatal(err)
	}
	_, out, _ := runLS(t, "-l", dir)
	lines := bytes.Split(bytes.TrimRight(out, "\n"), []byte("\n"))
	if len(lines) < 2 {
		t.Fatalf("expected total + 1 line, got: %s", out)
	}
	if !bytes.HasPrefix(lines[0], []byte("total ")) {
		t.Errorf("expected 'total' header, got %q", lines[0])
	}
	if !bytes.HasPrefix(lines[1], []byte("-rw-r-----")) {
		t.Errorf("expected mode -rw-r----- for 0640, got %q", lines[1])
	}
	if !bytes.Contains(lines[1], []byte(" 2 ")) {
		t.Errorf("expected size 2 in long line, got %q", lines[1])
	}
}

func TestClassifyAppendsSuffix(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "script"), []byte("#!/bin/sh"), 0o755)
	os.WriteFile(filepath.Join(dir, "data"), []byte("x"), 0o644)
	os.Mkdir(filepath.Join(dir, "sub"), 0o755)
	_, out, _ := runLS(t, "-F", dir)
	got := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	sort.Strings(got)
	want := []string{"data", "script*", "sub/"}
	if !equalStrings(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestRecursivePrintsAllLevels(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "a"), 0o755)
	os.Mkdir(filepath.Join(dir, "a", "b"), 0o755)
	os.WriteFile(filepath.Join(dir, "a", "b", "c"), nil, 0o600)
	_, out, _ := runLS(t, "-R", dir)
	s := string(out)
	for _, want := range []string{filepath.Join(dir, "a") + ":", filepath.Join(dir, "a", "b") + ":", "c"} {
		if !strings.Contains(s, want) {
			t.Errorf("-R missing %q in:\n%s", want, s)
		}
	}
}

func TestSortBySize(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "small"), []byte("x"), 0o600)
	os.WriteFile(filepath.Join(dir, "big"), bytes.Repeat([]byte("y"), 100), 0o600)
	_, out, _ := runLS(t, "-S", dir)
	got := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(got) != 2 || got[0] != "big" || got[1] != "small" {
		t.Errorf("-S sort wrong: %v", got)
	}
}

func TestReverseOrder(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a", "b", "c"} {
		os.WriteFile(filepath.Join(dir, n), nil, 0o600)
	}
	_, out, _ := runLS(t, "-r", dir)
	got := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if !equalStrings(got, []string{"c", "b", "a"}) {
		t.Errorf("-r wrong: %v", got)
	}
}

func TestDirectoryFlag(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "x"), nil, 0o600)
	_, out, _ := runLS(t, "-d", dir)
	got := strings.TrimRight(string(out), "\n")
	if got != dir {
		t.Errorf("-d listed contents: %q", got)
	}
}

func TestMissingFileSetsExitCode(t *testing.T) {
	exit, _, er := runLS(t, "/nonexistent-bag-test")
	if exit == 0 {
		t.Errorf("expected non-zero exit; stderr=%s", er)
	}
	if !strings.Contains(string(er), "cannot access") {
		t.Errorf("expected diagnostic; got %s", er)
	}
}

func TestSymlinkInLongMode(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "target"), nil, 0o600)
	if err := os.Symlink("target", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	_, out, _ := runLS(t, "-l", dir)
	if !strings.Contains(string(out), "link -> target") {
		t.Errorf("symlink target not shown:\n%s", out)
	}
	if !strings.Contains(string(out), "lrw") && !strings.Contains(string(out), "lrwx") {
		t.Errorf("symlink mode prefix missing:\n%s", out)
	}
}

func TestHumanReadable(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "k"), bytes.Repeat([]byte("x"), 2048), 0o600)
	_, out, _ := runLS(t, "-lh", dir)
	if !strings.Contains(string(out), "2.0K") {
		t.Errorf("expected 2.0K in -lh output:\n%s", out)
	}
}

// TestConformanceSimpleListing: compare bag's plain `ls DIR` output to
// system ls's `LC_ALL=C ls DIR` for a small fixture. Both should sort
// the same way and emit the same byte stream.
func TestConformanceSimpleListing(t *testing.T) {
	sys, err := exec.LookPath("ls")
	if err != nil {
		t.Skip("no system ls; skipping conformance test")
	}
	dir := t.TempDir()
	for _, n := range []string{"alpha", "beta", "gamma", "Beta"} {
		os.WriteFile(filepath.Join(dir, n), nil, 0o600)
	}
	_, bagOut, _ := runLS(t, dir)
	cmd := exec.Command(sys, dir)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	sysOut, err := cmd.Output()
	if err != nil {
		t.Fatalf("system ls: %v", err)
	}
	if !bytes.Equal(bagOut, sysOut) {
		t.Errorf("conformance diff:\n bag:%q\n sys:%q", bagOut, sysOut)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
