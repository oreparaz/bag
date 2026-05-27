package rm

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runRM(t *testing.T, args ...string) (int, string, string) {
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

func TestRemovesFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x")
	os.WriteFile(p, []byte("data"), 0o600)
	exit, _, er := runRM(t, p)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%s", exit, er)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("file still exists: %v", err)
	}
}

func TestRefusesDirWithoutR(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "d")
	os.Mkdir(target, 0o755)
	exit, _, er := runRM(t, target)
	if exit == 0 {
		t.Errorf("expected refusal without -r")
	}
	if !strings.Contains(er, "Is a directory") {
		t.Errorf("expected diagnostic, got %s", er)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("directory was deleted anyway")
	}
}

func TestRemovesTreeWithR(t *testing.T) {
	dir := t.TempDir()
	tree := filepath.Join(dir, "tree")
	os.MkdirAll(filepath.Join(tree, "sub", "deep"), 0o755)
	os.WriteFile(filepath.Join(tree, "sub", "deep", "f"), []byte("x"), 0o600)
	exit, _, er := runRM(t, "-r", tree)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%s", exit, er)
	}
	if _, err := os.Stat(tree); !os.IsNotExist(err) {
		t.Errorf("tree still exists: %v", err)
	}
}

func TestForceIgnoresMissing(t *testing.T) {
	dir := t.TempDir()
	exit, _, er := runRM(t, "-f", filepath.Join(dir, "nope"))
	if exit != 0 {
		t.Errorf("-f should ignore missing; exit=%d %s", exit, er)
	}
}

func TestMissingFileWithoutForce(t *testing.T) {
	dir := t.TempDir()
	exit, _, er := runRM(t, filepath.Join(dir, "nope"))
	if exit == 0 {
		t.Errorf("expected error on missing file")
	}
	if !strings.Contains(er, "no such") && !strings.Contains(er, "exist") {
		t.Errorf("unexpected stderr: %s", er)
	}
}

func TestPreserveRoot(t *testing.T) {
	exit, _, er := runRM(t, "-rf", "/")
	if exit == 0 {
		t.Fatal("rm -rf / must refuse without --no-preserve-root")
	}
	if !strings.Contains(er, "dangerous") || !strings.Contains(er, "preserve-root") {
		t.Errorf("expected preserve-root diagnostic, got %s", er)
	}
}

func TestVerboseOutput(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x")
	os.WriteFile(p, nil, 0o600)
	_, out, _ := runRM(t, "-v", p)
	if !strings.Contains(out, "removed") {
		t.Errorf("verbose output missing: %s", out)
	}
}

func TestDirFlagRemovesEmptyDir(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "empty")
	os.Mkdir(target, 0o755)
	exit, _, er := runRM(t, "-d", target)
	if exit != 0 {
		t.Errorf("-d failed: %s", er)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("dir still exists")
	}
}

func TestDirFlagRefusesNonEmptyDir(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "full")
	os.Mkdir(target, 0o755)
	os.WriteFile(filepath.Join(target, "x"), nil, 0o600)
	exit, _, er := runRM(t, "-d", target)
	if exit == 0 {
		t.Errorf("-d should refuse non-empty dir")
	}
	if er == "" {
		t.Errorf("expected diagnostic")
	}
}
