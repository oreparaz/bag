package stat

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runStat(t *testing.T, args ...string) (int, string, string) {
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

func TestDefaultBlock(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x")
	os.WriteFile(p, []byte("hello"), 0o644)
	exit, out, er := runStat(t, p)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%s", exit, er)
	}
	for _, want := range []string{"File:", "Size:", "Access:", "Modify:", "Change:"} {
		if !strings.Contains(out, want) {
			t.Errorf("default output missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "5") {
		t.Errorf("size 5 not in output:\n%s", out)
	}
}

func TestFormatSize(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x")
	os.WriteFile(p, []byte("abcdef"), 0o644)
	_, out, _ := runStat(t, "-c", "%s", p)
	if strings.TrimSpace(out) != "6" {
		t.Errorf("got %q", out)
	}
}

func TestFormatName(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x")
	os.WriteFile(p, nil, 0o644)
	_, out, _ := runStat(t, "-c", "%n", p)
	if strings.TrimSpace(out) != p {
		t.Errorf("got %q want %q", out, p)
	}
}

func TestFormatPermsOctal(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x")
	os.WriteFile(p, nil, 0o751)
	_, out, _ := runStat(t, "-c", "%a", p)
	if strings.TrimSpace(out) != "751" {
		t.Errorf("got %q", out)
	}
}

func TestFormatTypeString(t *testing.T) {
	dir := t.TempDir()
	_, out, _ := runStat(t, "-c", "%F", dir)
	if strings.TrimSpace(out) != "directory" {
		t.Errorf("got %q", out)
	}
}

func TestSymlinkNoDeref(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")
	os.WriteFile(target, []byte("x"), 0o644)
	os.Symlink("target", link)
	_, out, _ := runStat(t, "-c", "%F", link)
	if strings.TrimSpace(out) != "symbolic link" {
		t.Errorf("expected symbolic link, got %q", out)
	}
}

func TestSymlinkDeref(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")
	os.WriteFile(target, []byte("x"), 0o644)
	os.Symlink("target", link)
	_, out, _ := runStat(t, "-L", "-c", "%F", link)
	if strings.TrimSpace(out) != "regular file" {
		t.Errorf("expected regular file with -L, got %q", out)
	}
}

func TestMissingFile(t *testing.T) {
	exit, _, er := runStat(t, "/nonexistent-bag-stat-zzz")
	if exit == 0 {
		t.Errorf("expected non-zero exit")
	}
	if !strings.Contains(er, "cannot stat") {
		t.Errorf("expected diagnostic, got %s", er)
	}
}

func TestPercentY(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x")
	os.WriteFile(p, nil, 0o644)
	_, out, _ := runStat(t, "-c", "%Y", p)
	if strings.TrimSpace(out) == "" {
		t.Errorf("expected unix mtime, got empty")
	}
}

func TestTerse(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x")
	os.WriteFile(p, []byte("hi"), 0o644)
	_, out, _ := runStat(t, "-t", p)
	if !strings.HasPrefix(out, p+" ") {
		t.Errorf("terse should start with path: %q", out)
	}
	if !strings.Contains(out, "2 ") {
		t.Errorf("terse should include size 2: %q", out)
	}
}

func TestNFormatSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")
	os.WriteFile(target, nil, 0o644)
	os.Symlink("target", link)
	_, out, _ := runStat(t, "-c", "%N", link)
	if !strings.Contains(out, "->") {
		t.Errorf("%%N should show target for symlink: %q", out)
	}
}
