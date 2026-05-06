package safefs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRefusePathTraversal(t *testing.T) {
	bad := []string{"/etc/passwd", "../foo", "a/../b", "a/../../b"}
	for _, b := range bad {
		if err := RefusePathTraversal(b); err == nil {
			t.Errorf("expected error for %q", b)
		}
	}
	good := []string{"foo", "a/b", "a/b/c", "./a"}
	for _, g := range good {
		if err := RefusePathTraversal(g); err != nil {
			t.Errorf("unexpected error for %q: %v", g, err)
		}
	}
}

func TestEnsureNoSymlinkInPath(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(old)

	// Plain directories under root — fine.
	os.MkdirAll("safe/sub", 0o755)
	if err := EnsureNoSymlinkInPath(".", "safe/sub/file.txt"); err != nil {
		t.Errorf("plain path rejected: %v", err)
	}

	// Symlink in the middle — refused.
	os.MkdirAll("real", 0o755)
	os.Symlink("real", "evil")
	if err := EnsureNoSymlinkInPath(".", "evil/file.txt"); err == nil {
		t.Errorf("expected ErrSymlinkInPath for path through symlink")
	} else if !errors.Is(err, ErrSymlinkInPath) {
		t.Errorf("got %v, want ErrSymlinkInPath", err)
	}
}

// TestEnsureNoSymlinkInPathIgnoresAboveRoot: components above the
// extraction root must not be checked. Otherwise on macOS where /var is
// a system symlink to /private/var, every extraction into a tempdir
// fails. Here we simulate the same shape with our own symlink-in-the-
// middle of the absolute path.
func TestEnsureNoSymlinkInPathIgnoresAboveRoot(t *testing.T) {
	dir := t.TempDir()
	// Construct an absolute path with a symlink in its prefix.
	os.MkdirAll(filepath.Join(dir, "real-root"), 0o755)
	os.Symlink(filepath.Join(dir, "real-root"), filepath.Join(dir, "alias-root"))

	// Use alias-root as the extraction root. Even though the prefix
	// /tmp/.../alias-root is a symlink, we should NOT refuse.
	root := filepath.Join(dir, "alias-root")
	target := filepath.Join(root, "fresh", "file.txt")
	if err := EnsureNoSymlinkInPath(root, target); err != nil {
		t.Errorf("EnsureNoSymlinkInPath should ignore symlinks above root, got %v", err)
	}

	// But a symlink inside the root must still be refused.
	os.MkdirAll(filepath.Join(root, "real-sub"), 0o755)
	os.Symlink("real-sub", filepath.Join(root, "trap"))
	target = filepath.Join(root, "trap", "file.txt")
	if err := EnsureNoSymlinkInPath(root, target); err == nil {
		t.Errorf("expected ErrSymlinkInPath for path through symlink inside root")
	}
}

func TestCreateExclRefusesExistingFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateExcl(p, 0o644); err == nil {
		t.Errorf("expected EEXIST")
	}
}

func TestCreateExclRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real")
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(target, []byte("real"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateExcl(link, 0o644); err == nil {
		t.Errorf("expected open to fail on symlink leaf")
	}
}

func TestCreateTruncRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real")
	link := filepath.Join(dir, "link")
	os.WriteFile(target, []byte("real"), 0o644)
	os.Symlink(target, link)
	if _, err := CreateTrunc(link, 0o644); err == nil {
		t.Errorf("expected ELOOP on symlink leaf")
	}
	// Real should NOT have been truncated.
	body, _ := os.ReadFile(target)
	if string(body) != "real" {
		t.Errorf("symlink target was clobbered: %q", body)
	}
}

func TestMkdirAllNoSymlinkLeaf(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(old)

	os.MkdirAll("real", 0o755)
	os.Symlink("real", "link")
	if err := MkdirAllNoSymlinkLeaf(".", "link", 0o755); err == nil {
		t.Errorf("expected refusal of existing symlink leaf")
	}
}
