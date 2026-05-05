package find

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func runFind(t *testing.T, args ...string) (int, string) {
	t.Helper()
	rOut, wOut, _ := os.Pipe()
	oldOut := os.Stdout
	os.Stdout = wOut
	exit := Main(args)
	wOut.Close()
	os.Stdout = oldOut
	out, _ := io.ReadAll(rOut)
	return exit, string(out)
}

func setupTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mk := func(rel, content string) {
		full := filepath.Join(dir, rel)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(content), 0o644)
	}
	mk("a.txt", "hello\n")
	mk("b.go", "")
	mk("sub/c.txt", "deep\n")
	mk("sub/inner/d.go", "")
	return dir
}

func sortedLines(s string) []string {
	xs := strings.Split(strings.TrimRight(s, "\n"), "\n")
	sort.Strings(xs)
	return xs
}

func TestNameMatch(t *testing.T) {
	dir := setupTree(t)
	_, out := runFind(t, dir, "-name", "*.txt")
	got := sortedLines(out)
	want := []string{filepath.Join(dir, "a.txt"), filepath.Join(dir, "sub/c.txt")}
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestType(t *testing.T) {
	dir := setupTree(t)
	_, out := runFind(t, dir, "-type", "d")
	got := sortedLines(out)
	wantSet := map[string]bool{
		dir:                          true,
		filepath.Join(dir, "sub"):    true,
		filepath.Join(dir, "sub/inner"): true,
	}
	for _, g := range got {
		if !wantSet[g] {
			t.Errorf("unexpected dir %q", g)
		}
	}
}

func TestMaxDepth(t *testing.T) {
	dir := setupTree(t)
	_, out := runFind(t, dir, "-maxdepth", "1", "-type", "f")
	got := sortedLines(out)
	for _, g := range got {
		rel, _ := filepath.Rel(dir, g)
		if strings.Contains(rel, "/") {
			t.Errorf("maxdepth violated: %q", g)
		}
	}
}

func TestMinDepth(t *testing.T) {
	dir := setupTree(t)
	_, out := runFind(t, dir, "-mindepth", "2", "-type", "f")
	for _, g := range sortedLines(out) {
		rel, _ := filepath.Rel(dir, g)
		parts := strings.Count(rel, "/")
		if parts < 1 {
			t.Errorf("mindepth violated: %q", g)
		}
	}
}

func TestPrune(t *testing.T) {
	dir := setupTree(t)
	_, out := runFind(t, dir, "-name", "sub", "-prune", "-o", "-type", "f", "-print")
	got := sortedLines(out)
	for _, g := range got {
		if strings.Contains(g, "/sub/") {
			t.Errorf("file inside pruned dir leaked: %q", g)
		}
	}
}

func TestSize(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "small"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "big"), make([]byte, 4096), 0o644)
	_, out := runFind(t, dir, "-size", "+1k")
	if !strings.Contains(out, "/big") {
		t.Errorf("expected big file matching +1k: %q", out)
	}
	if strings.Contains(out, "/small") {
		t.Errorf("small file matched +1k: %q", out)
	}
}

func TestEmpty(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "full"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "empty"), nil, 0o644)
	_, out := runFind(t, dir, "-type", "f", "-empty")
	if !strings.Contains(out, "/empty") {
		t.Errorf("missing empty file: %q", out)
	}
}

func TestPrint0(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a"), nil, 0o644)
	os.WriteFile(filepath.Join(dir, "b"), nil, 0o644)
	_, out := runFind(t, dir, "-type", "f", "-print0")
	if strings.Contains(out, "\n") {
		t.Errorf("print0 should use NUL: %q", out)
	}
	if !strings.Contains(out, "\x00") {
		t.Errorf("expected NUL separators: %q", out)
	}
}

func TestNotAndOr(t *testing.T) {
	dir := setupTree(t)
	_, out := runFind(t, dir, "-type", "f", "-not", "-name", "*.go")
	for _, g := range sortedLines(out) {
		if strings.HasSuffix(g, ".go") {
			t.Errorf("excluded .go file present: %q", g)
		}
	}

	_, out = runFind(t, dir, "(", "-name", "a.txt", "-o", "-name", "*.go", ")")
	got := sortedLines(out)
	for _, g := range got {
		base := filepath.Base(g)
		if base != "a.txt" && !strings.HasSuffix(base, ".go") {
			t.Errorf("unexpected: %q", g)
		}
	}
}

func TestExec(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "x"), []byte(""), 0o644)
	exit, _ := runFind(t, dir, "-name", "x", "-exec", "true", "{}", ";")
	if exit != 0 {
		t.Errorf("exit=%d", exit)
	}
}

// TestDeleteEmptiesDirsFirst: -delete on a non-empty tree must remove
// children before parents (GNU find implies -depth with -delete).
func TestDeleteEmptiesDirsFirst(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "to-remove")
	os.MkdirAll(subdir, 0o755)
	os.WriteFile(filepath.Join(subdir, "x"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(subdir, "y"), []byte(""), 0o644)

	exit, _ := runFind(t, dir, "-mindepth", "1", "-delete")
	if exit != 0 {
		t.Fatalf("delete exit=%d", exit)
	}
	if _, err := os.Stat(subdir); err == nil {
		t.Errorf("subdir not removed")
	}
	// dir itself must still exist (-mindepth 1 protects the root).
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("dir went away: %v", err)
	}
}
