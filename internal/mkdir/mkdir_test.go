package mkdir

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runMkdir(t *testing.T, args ...string) (int, string, string) {
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

func TestCreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "newdir")
	exit, _, er := runMkdir(t, target)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%s", exit, er)
	}
	if st, err := os.Stat(target); err != nil || !st.IsDir() {
		t.Errorf("directory not created: err=%v", err)
	}
}

func TestParentsCreatesIntermediate(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a", "b", "c")
	exit, _, er := runMkdir(t, "-p", target)
	if exit != 0 {
		t.Fatalf("-p failed: %s", er)
	}
	if st, err := os.Stat(target); err != nil || !st.IsDir() {
		t.Errorf("nested directory not created")
	}
}

func TestWithoutParentsFailsOnExisting(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "exists")
	os.Mkdir(target, 0o755)
	exit, _, er := runMkdir(t, target)
	if exit == 0 {
		t.Errorf("expected error on existing dir; stderr=%s", er)
	}
}

func TestParentsSilentOnExisting(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "exists")
	os.Mkdir(target, 0o755)
	exit, _, er := runMkdir(t, "-p", target)
	if exit != 0 {
		t.Errorf("-p should be silent on existing dir; stderr=%s", er)
	}
}

func TestModeFlag(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "m")
	exit, _, er := runMkdir(t, "-m", "700", target)
	if exit != 0 {
		t.Fatalf("exit=%d %s", exit, er)
	}
	st, _ := os.Stat(target)
	if st.Mode().Perm() != 0o700 {
		t.Errorf("mode = %o want 700", st.Mode().Perm())
	}
}

func TestVerbose(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "v")
	_, out, _ := runMkdir(t, "-v", target)
	if !strings.Contains(out, "created directory") || !strings.Contains(out, "v") {
		t.Errorf("verbose output missing: %q", out)
	}
}

func TestMissingOperand(t *testing.T) {
	exit, _, er := runMkdir(t)
	if exit == 0 {
		t.Errorf("expected error on no args")
	}
	if !strings.Contains(er, "missing operand") {
		t.Errorf("expected diagnostic, got %s", er)
	}
}
