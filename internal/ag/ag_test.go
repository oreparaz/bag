package ag

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runAg(t *testing.T, cwd string, args ...string) (int, string, string) {
	t.Helper()
	old, _ := os.Getwd()
	if cwd != "" {
		os.Chdir(cwd)
	}
	defer os.Chdir(old)

	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout = wOut
	os.Stderr = wErr
	exit := Main(args)
	wOut.Close()
	wErr.Close()
	os.Stdout = oldOut
	os.Stderr = oldErr
	out, _ := io.ReadAll(rOut)
	se, _ := io.ReadAll(rErr)
	return exit, string(out), string(se)
}

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for path, content := range files {
		full := filepath.Join(root, path)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(content), 0o644)
	}
}

func TestRecursiveByDefault(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"a.txt":         "alpha\n",
		"sub/b.txt":     "alpha beta\n",
		"sub/inner/c.txt": "alpha\n",
	})
	exit, out, _ := runAg(t, dir, "alpha")
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	for _, want := range []string{"a.txt", "sub/b.txt", "sub/inner/c.txt"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output: %q", want, out)
		}
	}
}

func TestSmartCaseLowerInsensitive(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"a.txt": "ALPHA\nalpha\nAlpha\n",
	})
	_, out, _ := runAg(t, dir, "alpha")
	// All three lines should match because pattern is all-lowercase.
	if strings.Count(out, "\n") < 3 {
		t.Errorf("expected 3 matches; got %q", out)
	}
}

func TestSmartCaseMixedSensitive(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"a.txt": "ALPHA\nalpha\nAlpha\n",
	})
	_, out, _ := runAg(t, dir, "Alpha")
	// Only the exact-case "Alpha" should match.
	count := strings.Count(out, "Alpha\n")
	if count != 1 {
		t.Errorf("expected exactly 1 match; got %d in %q", count, out)
	}
	if strings.Contains(out, "ALPHA") || strings.Contains(out, "alpha\n") {
		t.Errorf("uppercase pattern should be case-sensitive: %q", out)
	}
}

func TestSkipsHiddenDirs(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"a.txt":         "needle\n",
		".git/x.txt":    "needle\n",
		".hidden/y.txt": "needle\n",
	})
	_, out, _ := runAg(t, dir, "needle")
	if strings.Contains(out, ".git") || strings.Contains(out, ".hidden") {
		t.Errorf("hidden paths leaked: %q", out)
	}
	if !strings.Contains(out, "a.txt") {
		t.Errorf("expected a.txt: %q", out)
	}
}

func TestHiddenFlagIncludesDotfiles(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"a.txt":         "needle\n",
		".hidden/y.txt": "needle\n",
	})
	_, out, _ := runAg(t, dir, "--hidden", "needle")
	if !strings.Contains(out, ".hidden") {
		t.Errorf("--hidden should include dotdirs: %q", out)
	}
}

func TestGitignoreHonored(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		".gitignore":   "build/\n*.tmp\n",
		"a.txt":        "needle\n",
		"build/x.txt":  "needle\n",
		"scratch.tmp":  "needle\n",
		"sub/keep.txt": "needle\n",
	})
	_, out, _ := runAg(t, dir, "needle")
	if strings.Contains(out, "build/") {
		t.Errorf("build/ leaked: %q", out)
	}
	if strings.Contains(out, "scratch.tmp") {
		t.Errorf(".tmp leaked: %q", out)
	}
	for _, want := range []string{"a.txt", "sub/keep.txt"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
}

func TestNoIgnoreFlag(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		".gitignore":  "build/\n",
		"build/x.txt": "needle\n",
	})
	_, out, _ := runAg(t, dir, "-U", "needle")
	if !strings.Contains(out, "build/x.txt") {
		t.Errorf("-U should disable .gitignore: %q", out)
	}
}

func TestBinarySkippedByDefault(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"a.txt": "needle\n",
	})
	// 8K of zeros + needle.
	bin := make([]byte, 8192)
	bin = append(bin, []byte("needle\n")...)
	os.WriteFile(filepath.Join(dir, "blob.bin"), bin, 0o644)

	_, out, _ := runAg(t, dir, "needle")
	if strings.Contains(out, "blob.bin") {
		t.Errorf("binary file leaked: %q", out)
	}
	if !strings.Contains(out, "a.txt") {
		t.Errorf("a.txt missing: %q", out)
	}
}

func TestAllTypesIncludesBinary(t *testing.T) {
	dir := t.TempDir()
	bin := make([]byte, 8192)
	bin = append(bin, []byte("needle\n")...)
	os.WriteFile(filepath.Join(dir, "blob.bin"), bin, 0o644)
	_, out, _ := runAg(t, dir, "-a", "needle")
	if !strings.Contains(out, "blob.bin") {
		t.Errorf("-a should include binary files: %q", out)
	}
}

func TestLiteralMode(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"a.txt": "a.b\nazb\n",
	})
	_, out, _ := runAg(t, dir, "-Q", "a.b")
	if !strings.Contains(out, "a.b") {
		t.Errorf("expected a.b literal match: %q", out)
	}
	if strings.Contains(out, "azb") {
		t.Errorf("literal mode should not match azb: %q", out)
	}
}

func TestFilesWithMatches(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"hit.txt":  "needle\n",
		"miss.txt": "x\n",
	})
	_, out, _ := runAg(t, dir, "-l", "needle")
	if !strings.Contains(out, "hit.txt") {
		t.Errorf("hit.txt missing: %q", out)
	}
	if strings.Contains(out, "miss.txt") {
		t.Errorf("miss.txt leaked: %q", out)
	}
}

func TestCount(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"a.txt": "needle\nneedle\nx\n",
	})
	_, out, _ := runAg(t, dir, "-c", "needle")
	if !strings.Contains(out, "a.txt:2") {
		t.Errorf("expected 'a.txt:2': %q", out)
	}
}

func TestFileSearchRegex(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"a.go":  "needle\n",
		"a.txt": "needle\n",
	})
	_, out, _ := runAg(t, dir, "-G", `\.go$`, "needle")
	if !strings.Contains(out, "a.go") {
		t.Errorf("a.go missing: %q", out)
	}
	if strings.Contains(out, "a.txt") {
		t.Errorf("a.txt should be filtered: %q", out)
	}
}

func TestMaxDepth(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"top.txt":            "needle\n",
		"sub/mid.txt":        "needle\n",
		"sub/inner/deep.txt": "needle\n",
	})
	_, out, _ := runAg(t, dir, "--depth=1", "needle")
	if !strings.Contains(out, "top.txt") {
		t.Errorf("top.txt missing: %q", out)
	}
	if strings.Contains(out, "deep.txt") {
		t.Errorf("deep.txt should be excluded by depth: %q", out)
	}
}

func TestNoMatchExit1(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"a.txt": "x\n",
	})
	exit, _, _ := runAg(t, dir, "needle")
	if exit != 1 {
		t.Errorf("exit=%d want 1", exit)
	}
}

func TestExtraIgnoreFlag(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"a.txt": "needle\n",
		"b.log": "needle\n",
	})
	_, out, _ := runAg(t, dir, "--ignore=*.log", "needle")
	if strings.Contains(out, "b.log") {
		t.Errorf("--ignore *.log leaked: %q", out)
	}
	if !strings.Contains(out, "a.txt") {
		t.Errorf("a.txt missing: %q", out)
	}
}

// TestColorAlwaysEmitsANSI: with --color=always we expect bold-green
// filenames, bold-yellow line numbers, bold-red match highlights.
func TestColorAlwaysEmitsANSI(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"a.txt": "alpha beta\n",
	})
	_, out, _ := runAg(t, dir, "--color=always", "alpha")
	for _, want := range []string{
		"\x1b[1;32m",   // filename green
		"\x1b[1;33m",   // line number yellow
		"\x1b[1;31m",   // match red
		"\x1b[0m",      // reset
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected escape %q in %q", want, out)
		}
	}
}

// TestColorAutoOffWhenPiped: stdout is captured (not a TTY), so auto
// should produce no ANSI sequences.
func TestColorAutoOffWhenPiped(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"a.txt": "alpha\n",
	})
	_, out, _ := runAg(t, dir, "alpha")
	if strings.Contains(out, "\x1b[") {
		t.Errorf("auto color should be off off-TTY: %q", out)
	}
}

// TestColorNeverOverridesAlways: '--color=never' wins.
func TestColorNeverOverrides(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"a.txt": "alpha\n",
	})
	_, out, _ := runAg(t, dir, "--color=never", "alpha")
	if strings.Contains(out, "\x1b[") {
		t.Errorf("--color=never should suppress: %q", out)
	}
}

// TestNoColorEnv honors the NO_COLOR convention for auto mode.
func TestNoColorEnv(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"a.txt": "alpha\n",
	})
	t.Setenv("NO_COLOR", "1")
	// --color=always still wins (explicit user choice).
	_, out, _ := runAg(t, dir, "--color=always", "alpha")
	if !strings.Contains(out, "\x1b[1;32m") {
		t.Errorf("--color=always should win over NO_COLOR: %q", out)
	}
	// But auto should bow to NO_COLOR.
	_, out, _ = runAg(t, dir, "alpha")
	if strings.Contains(out, "\x1b[") {
		t.Errorf("NO_COLOR should suppress auto color: %q", out)
	}
}

// TestColorContextLinesNotHighlighted: context lines shouldn't have the
// match highlight (they didn't match), only filename + line number get
// tinted.
func TestColorContextLinesNotHighlighted(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"a.txt": "before\nMATCH\nafter\n",
	})
	_, out, _ := runAg(t, dir, "--color=always", "-C", "1", "MATCH")
	// Match line has the red escape; context lines shouldn't.
	if !strings.Contains(out, "\x1b[1;31mMATCH\x1b[0m") {
		t.Errorf("expected MATCH to be highlighted: %q", out)
	}
	// Surrounding context lines should still be plain — search for
	// 'before' followed by no escape before the line ends.
	if strings.Contains(out, "\x1b[1;31mbefore") || strings.Contains(out, "\x1b[1;31mafter") {
		t.Errorf("context lines should not be highlighted: %q", out)
	}
}

func TestContext(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"a.txt": "a\nb\nMATCH\nd\ne\n",
	})
	_, out, _ := runAg(t, dir, "-C", "1", "MATCH")
	if !strings.Contains(out, "b") || !strings.Contains(out, "d") {
		t.Errorf("missing context lines: %q", out)
	}
}
