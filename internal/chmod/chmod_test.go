package chmod

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runCM(t *testing.T, args ...string) (int, string, string) {
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

func mode(t *testing.T, p string) os.FileMode {
	t.Helper()
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return st.Mode().Perm()
}

func TestOctalLiteral(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "x")
	os.WriteFile(f, nil, 0o644)
	exit, _, er := runCM(t, "600", f)
	if exit != 0 {
		t.Fatalf("%s", er)
	}
	if got := mode(t, f); got != 0o600 {
		t.Errorf("mode = %o want 600", got)
	}
}

func TestOctalLeadingZero(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "x")
	os.WriteFile(f, nil, 0o644)
	_, _, er := runCM(t, "0755", f)
	if got := mode(t, f); got != 0o755 {
		t.Errorf("mode = %o want 755 (stderr=%s)", got, er)
	}
}

func TestSymbolicAddX(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "x")
	os.WriteFile(f, nil, 0o644)
	_, _, er := runCM(t, "u+x", f)
	if got := mode(t, f); got != 0o744 {
		t.Errorf("mode = %o want 744 (stderr=%s)", got, er)
	}
}

func TestSymbolicAllAddX(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "x")
	os.WriteFile(f, nil, 0o644)
	_, _, er := runCM(t, "a+x", f)
	if got := mode(t, f); got != 0o755 {
		t.Errorf("mode = %o want 755 (stderr=%s)", got, er)
	}
}

func TestSymbolicRemoveW(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "x")
	os.WriteFile(f, nil, 0o644)
	_, _, er := runCM(t, "go-w", f)
	if got := mode(t, f); got != 0o644 {
		// already no w for go; should be a no-op.
		t.Errorf("mode = %o want 644 (stderr=%s)", got, er)
	}
	os.Chmod(f, 0o666)
	runCM(t, "go-w", f)
	if got := mode(t, f); got != 0o644 {
		t.Errorf("after go-w from 666: mode = %o want 644", got)
	}
}

func TestSymbolicSet(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "x")
	os.WriteFile(f, nil, 0o777)
	_, _, _ = runCM(t, "u=r,g=,o=", f)
	if got := mode(t, f); got != 0o400 {
		t.Errorf("mode = %o want 400", got)
	}
}

func TestCapitalXForDir(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	os.Mkdir(sub, 0o644)
	_, _, _ = runCM(t, "a+X", sub)
	if got := mode(t, sub); got != 0o755 {
		t.Errorf("dir mode = %o want 755", got)
	}
	file := filepath.Join(dir, "f")
	os.WriteFile(file, nil, 0o644)
	_, _, _ = runCM(t, "a+X", file)
	if got := mode(t, file); got != 0o644 {
		t.Errorf("file mode = %o want 644 (X should NOT set on plain file)", got)
	}
	exec := filepath.Join(dir, "e")
	os.WriteFile(exec, nil, 0o744)
	_, _, _ = runCM(t, "g+X", exec)
	if got := mode(t, exec); got != 0o754 {
		t.Errorf("exec file mode = %o want 754 (X copies because owner has x)", got)
	}
}

func TestRecursive(t *testing.T) {
	// Use a mode that keeps directory-traverse bits (755), otherwise
	// the test itself can't re-stat the children afterwards. We're
	// testing that -R reaches the leaf, not testing perm-stripping.
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "a", "b"), 0o755)
	f := filepath.Join(dir, "a", "b", "f")
	os.WriteFile(f, nil, 0o644)
	_, _, er := runCM(t, "-R", "755", dir)
	if got := mode(t, f); got != 0o755 {
		t.Errorf("recursive: leaf mode = %o want 755 (stderr=%s)", got, er)
	}
	// Use symbolic recursive too, like the README example. Restore
	// write bits before returning so t.TempDir's cleanup can unlink.
	_, _, er = runCM(t, "-R", "a-w", dir)
	if got := mode(t, f); got != 0o555 {
		t.Errorf("recursive a-w: leaf mode = %o want 555 (stderr=%s)", got, er)
	}
	t.Cleanup(func() { _, _, _ = runCM(t, "-R", "u+w", dir) })
}

func TestVerbose(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "x")
	os.WriteFile(f, nil, 0o644)
	_, out, _ := runCM(t, "-v", "600", f)
	if !strings.Contains(out, "mode of") || !strings.Contains(out, "0600") {
		t.Errorf("verbose output missing: %q", out)
	}
}

func TestChangesSuppressesUnchanged(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "x")
	os.WriteFile(f, nil, 0o644)
	_, out, _ := runCM(t, "-c", "644", f)
	if out != "" {
		t.Errorf("-c should be silent when nothing changed; got %q", out)
	}
	_, out, _ = runCM(t, "-c", "600", f)
	if !strings.Contains(out, "changed") {
		t.Errorf("-c should report actual change; got %q", out)
	}
}

func TestReference(t *testing.T) {
	dir := t.TempDir()
	ref := filepath.Join(dir, "ref")
	target := filepath.Join(dir, "tgt")
	os.WriteFile(ref, nil, 0o751)
	os.WriteFile(target, nil, 0o644)
	exit, _, er := runCM(t, "--reference", ref, target)
	if exit != 0 {
		t.Fatalf("%s", er)
	}
	if got := mode(t, target); got != 0o751 {
		t.Errorf("mode = %o want 751", got)
	}
}

func TestMissingOperand(t *testing.T) {
	exit, _, er := runCM(t)
	if exit == 0 {
		t.Errorf("expected error on no args")
	}
	if !strings.Contains(er, "missing operand") {
		t.Errorf("expected diagnostic, got %s", er)
	}
}
