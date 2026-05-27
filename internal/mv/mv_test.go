package mv

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runMV(t *testing.T, args ...string) (int, string, string) {
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

func TestRename(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a")
	dst := filepath.Join(dir, "b")
	os.WriteFile(src, []byte("data"), 0o644)
	exit, _, er := runMV(t, src, dst)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%s", exit, er)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("src still exists")
	}
	got, _ := os.ReadFile(dst)
	if !bytes.Equal(got, []byte("data")) {
		t.Errorf("dst content wrong: %q", got)
	}
}

func TestMoveIntoDirectory(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "x")
	dstDir := filepath.Join(dir, "out")
	os.WriteFile(src, []byte("y"), 0o644)
	os.Mkdir(dstDir, 0o755)
	exit, _, er := runMV(t, src, dstDir)
	if exit != 0 {
		t.Fatalf("%s", er)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("src not removed")
	}
	got, _ := os.ReadFile(filepath.Join(dstDir, "x"))
	if string(got) != "y" {
		t.Errorf("dst missing/wrong: %q", got)
	}
}

func TestNoClobberSkipsExisting(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a")
	dst := filepath.Join(dir, "b")
	os.WriteFile(src, []byte("new"), 0o644)
	os.WriteFile(dst, []byte("old"), 0o644)
	exit, _, er := runMV(t, "-n", src, dst)
	if exit != 0 {
		t.Fatalf("%s", er)
	}
	if _, err := os.Stat(src); err != nil {
		t.Errorf("-n should leave src in place when dst exists; err=%v", err)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "old" {
		t.Errorf("dst overwritten despite -n: %q", got)
	}
}

func TestForceReplacesExisting(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a")
	dst := filepath.Join(dir, "b")
	os.WriteFile(src, []byte("new"), 0o644)
	os.WriteFile(dst, []byte("old"), 0o600)
	exit, _, er := runMV(t, "-f", src, dst)
	if exit != 0 {
		t.Fatalf("%s", er)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "new" {
		t.Errorf("dst not replaced: %q", got)
	}
}

func TestVerbose(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a")
	dst := filepath.Join(dir, "b")
	os.WriteFile(src, nil, 0o644)
	_, out, _ := runMV(t, "-v", src, dst)
	if !strings.Contains(out, "renamed") {
		t.Errorf("verbose output missing: %q", out)
	}
}

func TestMultipleSourcesIntoDir(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	os.WriteFile(a, []byte("a"), 0o644)
	os.WriteFile(b, []byte("b"), 0o644)
	dst := filepath.Join(dir, "out")
	os.Mkdir(dst, 0o755)
	exit, _, er := runMV(t, a, b, dst)
	if exit != 0 {
		t.Fatalf("%s", er)
	}
	for _, n := range []string{"a", "b"} {
		got, err := os.ReadFile(filepath.Join(dst, n))
		if err != nil || string(got) != n {
			t.Errorf("missing/wrong %s: %v %q", n, err, got)
		}
	}
}

func TestMultiSourceNonDirRejected(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	c := filepath.Join(dir, "c")
	os.WriteFile(a, nil, 0o644)
	os.WriteFile(b, nil, 0o644)
	os.WriteFile(c, nil, 0o644)
	exit, _, er := runMV(t, a, b, c)
	if exit == 0 {
		t.Errorf("expected error: multi-source requires dir destination")
	}
	if !strings.Contains(er, "not a directory") {
		t.Errorf("expected diagnostic, got %s", er)
	}
}

func TestRenameDirectory(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src-tree")
	dst := filepath.Join(dir, "dst-tree")
	os.MkdirAll(filepath.Join(src, "sub"), 0o755)
	os.WriteFile(filepath.Join(src, "sub", "f"), []byte("x"), 0o600)
	exit, _, er := runMV(t, src, dst)
	if exit != 0 {
		t.Fatalf("%s", er)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("src tree not removed")
	}
	got, _ := os.ReadFile(filepath.Join(dst, "sub", "f"))
	if string(got) != "x" {
		t.Errorf("dst tree wrong: %q", got)
	}
}
