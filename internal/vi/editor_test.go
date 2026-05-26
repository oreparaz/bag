package vi

import (
	"errors"
	"strings"
	"testing"
)

// keys is a tiny DSL for tests. Each rune is sent as a Key.Rune. A few
// special tokens get translated:
//
//	<esc>   ESC
//	<cr>    Enter
//	<bs>    Backspace
//	<tab>   Tab
//	<up><down><left><right>
//	<C-x>   Ctrl-x for any letter x
//
// Anything else is interpreted as a printable rune.
func send(t *testing.T, e *Editor, seq string) {
	t.Helper()
	i := 0
	for i < len(seq) {
		c := seq[i]
		if c == '<' {
			end := strings.IndexByte(seq[i:], '>')
			if end < 0 {
				t.Fatalf("unterminated key token at %q", seq[i:])
			}
			tok := seq[i+1 : i+end]
			i += end + 1
			switch tok {
			case "esc":
				_ = e.Key(CodeKey(KeyEsc))
			case "cr":
				_ = e.Key(CodeKey(KeyEnter))
			case "bs":
				_ = e.Key(CodeKey(KeyBackspace))
			case "tab":
				_ = e.Key(CodeKey(KeyTab))
			case "del":
				_ = e.Key(CodeKey(KeyDelete))
			case "up":
				_ = e.Key(CodeKey(KeyArrowUp))
			case "down":
				_ = e.Key(CodeKey(KeyArrowDown))
			case "left":
				_ = e.Key(CodeKey(KeyArrowLeft))
			case "right":
				_ = e.Key(CodeKey(KeyArrowRight))
			default:
				if strings.HasPrefix(tok, "C-") && len(tok) == 3 {
					_ = e.Key(CtrlKey(rune(tok[2])))
				} else {
					t.Fatalf("unknown key token %q", tok)
				}
			}
			continue
		}
		_ = e.Key(RuneKey(rune(c)))
		i++
	}
}

func mustOpen(t *testing.T, e *Editor, content string) {
	t.Helper()
	e.buf = FromBytes([]byte(content))
}

// --- Movement ---------------------------------------------------------

func TestMoveBasic(t *testing.T) {
	e := NewEditor()
	mustOpen(t, e, "abc\nde\nfghi\n")
	send(t, e, "lll")
	if r, c := e.Cursor(); r != 0 || c != 2 {
		t.Errorf("after 'lll' got (%d,%d) want (0,2)", r, c)
	}
	send(t, e, "j")
	if r, _ := e.Cursor(); r != 1 {
		t.Errorf("expected row 1, got %d", r)
	}
	// col should clamp to len-1 = 1 on line "de"
	if _, c := e.Cursor(); c != 1 {
		t.Errorf("expected col 1, got %d", c)
	}
	send(t, e, "$")
	if _, c := e.Cursor(); c != 1 {
		t.Errorf("expected col 1 (last char of 'de'), got %d", c)
	}
}

func TestGGandG(t *testing.T) {
	e := NewEditor()
	mustOpen(t, e, "a\nb\nc\nd\n")
	send(t, e, "G")
	if r, _ := e.Cursor(); r != 3 {
		t.Errorf("G should go to last line; got row %d", r)
	}
	send(t, e, "gg")
	if r, _ := e.Cursor(); r != 0 {
		t.Errorf("gg should go to first line; got row %d", r)
	}
}

func TestCount(t *testing.T) {
	e := NewEditor()
	mustOpen(t, e, "a\nb\nc\nd\ne\n")
	send(t, e, "3j")
	if r, _ := e.Cursor(); r != 3 {
		t.Errorf("3j should move 3 down; got row %d", r)
	}
}

// --- Insert mode ------------------------------------------------------

func TestInsertText(t *testing.T) {
	e := NewEditor()
	send(t, e, "ihello world<esc>")
	if e.Line(0) != "hello world" {
		t.Errorf("got %q", e.Line(0))
	}
}

func TestInsertNewline(t *testing.T) {
	e := NewEditor()
	send(t, e, "ifoo<cr>bar<esc>")
	if e.Line(0) != "foo" || e.Line(1) != "bar" {
		t.Errorf("got %v", e.Lines())
	}
}

func TestInsertBackspace(t *testing.T) {
	e := NewEditor()
	send(t, e, "iabc<bs><bs><esc>")
	if e.Line(0) != "a" {
		t.Errorf("got %q", e.Line(0))
	}
}

func TestAppendAtEol(t *testing.T) {
	e := NewEditor()
	mustOpen(t, e, "abc\n")
	send(t, e, "A!<esc>")
	if e.Line(0) != "abc!" {
		t.Errorf("got %q", e.Line(0))
	}
}

func TestOpenLineBelow(t *testing.T) {
	e := NewEditor()
	mustOpen(t, e, "first\nsecond\n")
	send(t, e, "oMID<esc>")
	if strings.Join(e.Lines(), "|") != "first|MID|second" {
		t.Errorf("got %v", e.Lines())
	}
}

// --- Normal-mode edits ------------------------------------------------

func TestDeleteCharX(t *testing.T) {
	e := NewEditor()
	mustOpen(t, e, "hello\n")
	send(t, e, "x")
	if e.Line(0) != "ello" {
		t.Errorf("got %q", e.Line(0))
	}
}

func TestDeleteLineDD(t *testing.T) {
	e := NewEditor()
	mustOpen(t, e, "a\nb\nc\n")
	send(t, e, "jdd")
	if strings.Join(e.Lines(), "|") != "a|c" {
		t.Errorf("got %v", e.Lines())
	}
}

func TestDD2dd(t *testing.T) {
	e := NewEditor()
	mustOpen(t, e, "a\nb\nc\nd\n")
	send(t, e, "2dd")
	if strings.Join(e.Lines(), "|") != "c|d" {
		t.Errorf("got %v", e.Lines())
	}
}

func TestDeleteWordAtEOLDoesNotEatNextLine(t *testing.T) {
	// Pre-fix `dw` on the last word of a line in a multi-line buffer
	// followed `moveWordForward` onto the next line and the operator
	// then deleted both whole lines. After fix, only the last word of
	// the current line is removed.
	e := NewEditor()
	mustOpen(t, e, "abc\ndef\n")
	send(t, e, "dw")
	if strings.Join(e.Lines(), "|") != "|def" {
		t.Errorf("got %v", e.Lines())
	}
}

func TestChangeLineCC(t *testing.T) {
	// cc on the first line must not panic and must clear that line and
	// drop into insert mode so subsequent text replaces it.
	e := NewEditor()
	mustOpen(t, e, "alpha\nbeta\n")
	send(t, e, "ccX")
	if strings.Join(e.Lines(), "|") != "X|beta" {
		t.Errorf("got %v", e.Lines())
	}

	// And cc on a non-first line.
	e = NewEditor()
	mustOpen(t, e, "alpha\nbeta\ngamma\n")
	send(t, e, "jccY")
	if strings.Join(e.Lines(), "|") != "alpha|Y|gamma" {
		t.Errorf("got %v", e.Lines())
	}
}

func TestYankAndPaste(t *testing.T) {
	e := NewEditor()
	mustOpen(t, e, "alpha\nbeta\n")
	send(t, e, "yyp")
	if strings.Join(e.Lines(), "|") != "alpha|alpha|beta" {
		t.Errorf("got %v", e.Lines())
	}
}

func TestDeleteWord(t *testing.T) {
	e := NewEditor()
	mustOpen(t, e, "alpha beta gamma\n")
	send(t, e, "dw")
	if e.Line(0) != "beta gamma" {
		t.Errorf("got %q", e.Line(0))
	}
}

func TestChangeWord(t *testing.T) {
	e := NewEditor()
	mustOpen(t, e, "alpha beta\n")
	send(t, e, "cwomega<esc>")
	if e.Line(0) != "omegabeta" {
		t.Errorf("got %q", e.Line(0))
	}
}

func TestReplaceChar(t *testing.T) {
	e := NewEditor()
	mustOpen(t, e, "abcdef\n")
	send(t, e, "lrX")
	if e.Line(0) != "aXcdef" {
		t.Errorf("got %q", e.Line(0))
	}
}

func TestJoin(t *testing.T) {
	e := NewEditor()
	mustOpen(t, e, "foo\nbar\n")
	send(t, e, "J")
	if e.Line(0) != "foo bar" || e.LineCount() != 1 {
		t.Errorf("got %v", e.Lines())
	}
	if e.LineCount() != 1 {
		t.Errorf("expected 1 line")
	}
}

// --- Undo / redo ------------------------------------------------------

func TestUndoRedo(t *testing.T) {
	e := NewEditor()
	send(t, e, "ihello<esc>")
	if e.Line(0) != "hello" {
		t.Fatalf("setup: %q", e.Line(0))
	}
	send(t, e, "u")
	if e.Line(0) != "" {
		t.Errorf("after undo got %q", e.Line(0))
	}
	send(t, e, "<C-r>")
	if e.Line(0) != "hello" {
		t.Errorf("after redo got %q", e.Line(0))
	}
}

func TestUndoMultipleEdits(t *testing.T) {
	e := NewEditor()
	mustOpen(t, e, "alpha\n")
	send(t, e, "$x")
	if e.Line(0) != "alph" {
		t.Fatalf("after x: %q", e.Line(0))
	}
	send(t, e, "x")
	if e.Line(0) != "alp" {
		t.Fatalf("after second x: %q", e.Line(0))
	}
	send(t, e, "uu")
	if e.Line(0) != "alpha" {
		t.Errorf("after 'uu' got %q", e.Line(0))
	}
}

// --- Search ------------------------------------------------------------

func TestSearchForward(t *testing.T) {
	e := NewEditor()
	mustOpen(t, e, "one\ntwo three\nfour\n")
	send(t, e, "/three<cr>")
	r, c := e.Cursor()
	if r != 1 || c != 4 {
		t.Errorf("got (%d,%d)", r, c)
	}
}

func TestSearchN(t *testing.T) {
	e := NewEditor()
	mustOpen(t, e, "x\nfoo\ny\nfoo\n")
	send(t, e, "/foo<cr>")
	r1, _ := e.Cursor()
	send(t, e, "n")
	r2, _ := e.Cursor()
	if r1 != 1 || r2 != 3 {
		t.Errorf("got rows %d,%d want 1,3", r1, r2)
	}
}

func TestSearchBackward(t *testing.T) {
	e := NewEditor()
	mustOpen(t, e, "foo\nbar\nfoo\nbaz\n")
	send(t, e, "G?foo<cr>")
	r, _ := e.Cursor()
	if r != 2 {
		t.Errorf("got row %d want 2", r)
	}
}

// --- :commands --------------------------------------------------------

func TestQuitDirty(t *testing.T) {
	e := NewEditor()
	send(t, e, "ihello<esc>")
	err := e.Key(RuneKey(':'))
	if err != nil {
		t.Fatal(err)
	}
	send(t, e, "q<cr>")
	if !strings.Contains(e.Message(), "No write") {
		t.Errorf("expected dirty warning, got %q", e.Message())
	}
}

func TestForceQuit(t *testing.T) {
	e := NewEditor()
	send(t, e, "ix<esc>:")
	err := errors.New("placeholder")
	for _, r := range "q!\n" {
		if r == '\n' {
			err = e.Key(CodeKey(KeyEnter))
		} else {
			err = e.Key(RuneKey(r))
		}
	}
	if !errors.Is(err, ErrQuit) {
		t.Errorf("expected ErrQuit, got %v", err)
	}
}

// --- Operator + count -------------------------------------------------

func TestThreeX(t *testing.T) {
	e := NewEditor()
	mustOpen(t, e, "hello\n")
	send(t, e, "3x")
	if e.Line(0) != "lo" {
		t.Errorf("got %q", e.Line(0))
	}
}

// --- Cursor clamping --------------------------------------------------

func TestCursorClampOnShorterLine(t *testing.T) {
	e := NewEditor()
	mustOpen(t, e, "longer line\nshort\n")
	send(t, e, "$")
	if _, c := e.Cursor(); c != 10 {
		t.Fatalf("$ on long line: col %d", c)
	}
	send(t, e, "j")
	if _, c := e.Cursor(); c != 4 {
		t.Errorf("after j: col %d want 4 (last char of 'short')", c)
	}
}

// --- regression: pasteAfter on empty line -----------------------------

func TestPasteOnEmpty(t *testing.T) {
	e := NewEditor()
	mustOpen(t, e, "hello\n\n")
	send(t, e, "yyjp")
	// After yanking line 0 and pasting after empty line 1.
	if strings.Join(e.Lines(), "|") != "hello||hello" {
		t.Errorf("got %v", e.Lines())
	}
}
