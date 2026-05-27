package cp

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runCP(t *testing.T, args ...string) (int, string, string) {
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

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	os.WriteFile(src, []byte("hello"), 0o644)
	exit, _, er := runCP(t, src, dst)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%s", exit, er)
	}
	got, _ := os.ReadFile(dst)
	if !bytes.Equal(got, []byte("hello")) {
		t.Errorf("got %q", got)
	}
}

func TestCopyIntoExistingDir(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dstDir := filepath.Join(dir, "out")
	os.WriteFile(src, []byte("hi"), 0o644)
	os.Mkdir(dstDir, 0o755)
	exit, _, er := runCP(t, src, dstDir)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%s", exit, er)
	}
	got, err := os.ReadFile(filepath.Join(dstDir, "src"))
	if err != nil || !bytes.Equal(got, []byte("hi")) {
		t.Errorf("expected src copied into out/: err=%v got=%q", err, got)
	}
}

func TestRefusesDirWithoutR(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "d")
	os.Mkdir(src, 0o755)
	exit, _, er := runCP(t, src, filepath.Join(dir, "d2"))
	if exit == 0 {
		t.Errorf("expected refusal without -r")
	}
	if !strings.Contains(er, "-r not specified") {
		t.Errorf("expected diagnostic, got %s", er)
	}
}

func TestRecursiveCopy(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "tree")
	os.MkdirAll(filepath.Join(src, "sub"), 0o755)
	os.WriteFile(filepath.Join(src, "sub", "f"), []byte("x"), 0o600)
	dst := filepath.Join(dir, "out")
	exit, _, er := runCP(t, "-r", src, dst)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%s", exit, er)
	}
	got, err := os.ReadFile(filepath.Join(dst, "sub", "f"))
	if err != nil || string(got) != "x" {
		t.Errorf("recursive copy lost data: %v %q", err, got)
	}
}

func TestPreserveMode(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "s")
	dst := filepath.Join(dir, "d")
	os.WriteFile(src, []byte("x"), 0o641)
	exit, _, er := runCP(t, "-p", src, dst)
	if exit != 0 {
		t.Fatalf("exit=%d %s", exit, er)
	}
	st, _ := os.Stat(dst)
	if st.Mode().Perm() != 0o641 {
		t.Errorf("mode = %o want 641", st.Mode().Perm())
	}
}

func TestNoClobberSkipsExisting(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "s")
	dst := filepath.Join(dir, "d")
	os.WriteFile(src, []byte("new"), 0o644)
	os.WriteFile(dst, []byte("old"), 0o644)
	exit, _, er := runCP(t, "-n", src, dst)
	if exit != 0 {
		t.Fatalf("exit=%d %s", exit, er)
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "old" {
		t.Errorf("destination overwritten despite -n: %q", got)
	}
}

func TestMultipleSourcesIntoDir(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a", "b"} {
		os.WriteFile(filepath.Join(dir, n), []byte(n), 0o644)
	}
	dst := filepath.Join(dir, "out")
	os.Mkdir(dst, 0o755)
	exit, _, er := runCP(t,
		filepath.Join(dir, "a"), filepath.Join(dir, "b"), dst)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%s", exit, er)
	}
	for _, n := range []string{"a", "b"} {
		got, err := os.ReadFile(filepath.Join(dst, n))
		if err != nil || string(got) != n {
			t.Errorf("missing/wrong %s: %v %q", n, err, got)
		}
	}
}

func TestSymlinkCopyAsLink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")
	dst := filepath.Join(dir, "link2")
	os.WriteFile(target, []byte("t"), 0o644)
	os.Symlink("target", link)
	exit, _, er := runCP(t, "-P", link, dst)
	if exit != 0 {
		t.Fatalf("exit=%d %s", exit, er)
	}
	info, err := os.Lstat(dst)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("expected symlink at dst, got mode=%v err=%v", info.Mode(), err)
	}
}

func TestSymlinkDereferencedByDefault(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")
	dst := filepath.Join(dir, "out")
	os.WriteFile(target, []byte("data"), 0o644)
	os.Symlink("target", link)
	exit, _, er := runCP(t, link, dst)
	if exit != 0 {
		t.Fatalf("exit=%d %s", exit, er)
	}
	info, _ := os.Lstat(dst)
	if info.Mode()&os.ModeSymlink != 0 {
		t.Errorf("expected regular file, got symlink")
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "data" {
		t.Errorf("dereferenced content wrong: %q", got)
	}
}

func TestArchiveImpliesRPNoDeref(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "tree")
	os.MkdirAll(src, 0o755)
	os.Symlink("nowhere", filepath.Join(src, "link"))
	dst := filepath.Join(dir, "out")
	exit, _, er := runCP(t, "-a", src, dst)
	if exit != 0 {
		t.Fatalf("exit=%d %s", exit, er)
	}
	info, err := os.Lstat(filepath.Join(dst, "link"))
	if err != nil {
		t.Fatalf("missing link in archive copy: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("-a should preserve symlinks; got mode=%v", info.Mode())
	}
}

func TestVerbose(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "s")
	dst := filepath.Join(dir, "d")
	os.WriteFile(src, nil, 0o644)
	_, out, _ := runCP(t, "-v", src, dst)
	if !strings.Contains(out, "->") {
		t.Errorf("verbose output missing: %q", out)
	}
}
