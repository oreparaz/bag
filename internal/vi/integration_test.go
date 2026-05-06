package vi

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEditSaveRoundTrip drives a full session: open a file, edit it,
// save, reopen, verify on-disk content matches.
func TestEditSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "hello.go")
	original := "package main\n\nfunc main() {\n}\n"
	if err := os.WriteFile(src, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	e := NewEditor()
	if err := e.Open(src); err != nil {
		t.Fatal(err)
	}

	// Go to line 3, after the {, open a new line, write a body,
	// quit-with-write.
	send(t, e, "3G")
	send(t, e, "o\tprintln(\"hi\")<esc>")
	send(t, e, ":wq<cr>")

	body, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n"
	if string(body) != want {
		t.Errorf("got %q want %q", body, want)
	}
}

// TestSaveAs covers ':w PATH'.
func TestSaveAs(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "out.txt")

	e := NewEditor()
	send(t, e, "iHELLO<esc>")
	if err := e.Save(target); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "HELLO\n" {
		t.Errorf("got %q", body)
	}
}

// TestQuitWithoutSaveRefuses asserts :q on a dirty buffer is refused.
func TestQuitWithoutSaveRefuses(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "f.txt")
	os.WriteFile(src, []byte("orig\n"), 0o644)
	e := NewEditor()
	if err := e.Open(src); err != nil {
		t.Fatal(err)
	}
	send(t, e, "ix<esc>")
	send(t, e, ":q<cr>")
	if !strings.Contains(e.Message(), "No write") {
		t.Errorf("expected refusal, got %q", e.Message())
	}
}

// TestUndoSequence: each `o<text><esc>` is one undo unit (matches vim);
// three u's step back through the three insert sessions.
func TestUndoSequence(t *testing.T) {
	e := NewEditor()
	send(t, e, "ialpha<esc>obeta<esc>ogamma<esc>")
	if strings.Join(e.Lines(), "|") != "alpha|beta|gamma" {
		t.Fatalf("setup: %v", e.Lines())
	}
	send(t, e, "u")
	if strings.Join(e.Lines(), "|") != "alpha|beta" {
		t.Errorf("after u: %v", e.Lines())
	}
	send(t, e, "u")
	if strings.Join(e.Lines(), "|") != "alpha" {
		t.Errorf("after uu: %v", e.Lines())
	}
	send(t, e, "u")
	if strings.Join(e.Lines(), "|") != "" {
		t.Errorf("after uuu: %v", e.Lines())
	}
}

// TestRegisterPaste copies a line and pastes it after another line.
func TestRegisterPaste(t *testing.T) {
	e := NewEditor()
	send(t, e, "ialpha<esc>")
	send(t, e, "yy")
	send(t, e, "obeta<esc>")
	send(t, e, "p")
	if strings.Join(e.Lines(), "|") != "alpha|beta|alpha" {
		t.Errorf("got %v", e.Lines())
	}
}

// TestSearchAndReplace mimics a "find next, replace" workflow using
// only built-in commands.
func TestSearchHighlight(t *testing.T) {
	e := NewEditor()
	mustOpen(t, e, "alpha\nbeta\ngamma\n")
	send(t, e, "/beta<cr>")
	r, c := e.Cursor()
	if r != 1 || c != 0 {
		t.Errorf("cursor at (%d,%d) want (1,0)", r, c)
	}
}

// TestAppendThenInsertHasOneUndoStep covers the bug where every keystroke
// in insert mode would create its own undo snapshot. A single insert
// session must collapse to one snapshot.
func TestAppendThenInsertHasOneUndoStep(t *testing.T) {
	e := NewEditor()
	send(t, e, "iabc<esc>")
	// Now exactly one undo should restore the buffer to empty.
	send(t, e, "u")
	if e.Line(0) != "" {
		t.Errorf("after one undo got %q", e.Line(0))
	}
}

// TestOpenNonExistent: the editor opens an empty buffer and remembers
// the filename so :w writes it out.
func TestOpenNonExistent(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "new.txt")
	e := NewEditor()
	if err := e.Open(target); err != nil {
		t.Fatal(err)
	}
	if e.LineCount() != 1 || e.Line(0) != "" {
		t.Errorf("expected empty buffer, got %v", e.Lines())
	}
	send(t, e, "ihello<esc>:w<cr>")
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "hello\n" {
		t.Errorf("got %q", body)
	}
}

// TestErrQuitFromCommandLine confirms the dispatcher returns ErrQuit.
func TestErrQuitFromCommandLine(t *testing.T) {
	e := NewEditor()
	// Insert nothing, just :q
	err := e.Key(RuneKey(':'))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range "q\n" {
		var k Key
		if r == '\n' {
			k = CodeKey(KeyEnter)
		} else {
			k = RuneKey(r)
		}
		err = e.Key(k)
	}
	if !errors.Is(err, ErrQuit) {
		t.Errorf("want ErrQuit, got %v", err)
	}
}
