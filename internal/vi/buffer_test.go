package vi

import (
	"strings"
	"testing"
)

func TestNewBufferIsEmpty(t *testing.T) {
	b := NewBuffer()
	if b.LineCount() != 1 || b.Line(0) != "" {
		t.Errorf("expected one empty line, got %v", b.Lines())
	}
}

func TestFromBytesPreservesContent(t *testing.T) {
	b := FromBytes([]byte("first\nsecond\nthird\n"))
	want := []string{"first", "second", "third"}
	got := b.Lines()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %v want %v", got, want)
	}
}

func TestFromBytesNoTrailingNewline(t *testing.T) {
	b := FromBytes([]byte("a\nb"))
	got := b.Lines()
	if strings.Join(got, "|") != "a|b" {
		t.Errorf("got %v", got)
	}
}

func TestBytesRoundTrip(t *testing.T) {
	original := "foo\nbar\nbaz\n"
	b := FromBytes([]byte(original))
	if string(b.Bytes()) != original {
		t.Errorf("got %q want %q", b.Bytes(), original)
	}
}

func TestInsertRune(t *testing.T) {
	b := FromBytes([]byte("hello\n"))
	b.InsertRune(0, 5, '!')
	if b.Line(0) != "hello!" {
		t.Errorf("got %q", b.Line(0))
	}
}

func TestInsertRuneInMiddle(t *testing.T) {
	b := FromBytes([]byte("hllo\n"))
	b.InsertRune(0, 1, 'e')
	if b.Line(0) != "hello" {
		t.Errorf("got %q", b.Line(0))
	}
}

func TestDeleteRune(t *testing.T) {
	b := FromBytes([]byte("hxello\n"))
	r := b.DeleteRune(0, 1)
	if r != 'x' {
		t.Errorf("got %q", r)
	}
	if b.Line(0) != "hello" {
		t.Errorf("got %q", b.Line(0))
	}
}

func TestSplitLine(t *testing.T) {
	b := FromBytes([]byte("hello world\n"))
	b.SplitLine(0, 5)
	if b.LineCount() != 2 {
		t.Fatalf("expected 2 lines, got %d", b.LineCount())
	}
	if b.Line(0) != "hello" || b.Line(1) != " world" {
		t.Errorf("got %v", b.Lines())
	}
}

func TestJoinLine(t *testing.T) {
	b := FromBytes([]byte("foo\nbar\n"))
	b.JoinLine(0, " ")
	if b.Line(0) != "foo bar" {
		t.Errorf("got %q", b.Line(0))
	}
	if b.LineCount() != 1 {
		t.Errorf("expected 1 line, got %d", b.LineCount())
	}
}

func TestDeleteLine(t *testing.T) {
	b := FromBytes([]byte("a\nb\nc\n"))
	d := b.DeleteLine(1)
	if d != "b" {
		t.Errorf("got deleted %q", d)
	}
	if strings.Join(b.Lines(), "|") != "a|c" {
		t.Errorf("got %v", b.Lines())
	}
}

func TestDeleteOnlyLine(t *testing.T) {
	b := FromBytes([]byte("only\n"))
	b.DeleteLine(0)
	if b.LineCount() != 1 || b.Line(0) != "" {
		t.Errorf("buffer must keep at least one (empty) line; got %v", b.Lines())
	}
}

func TestDeleteRange(t *testing.T) {
	b := FromBytes([]byte("hello world\n"))
	d := b.DeleteRange(0, 0, 6)
	if d != "hello " {
		t.Errorf("got deleted %q", d)
	}
	if b.Line(0) != "world" {
		t.Errorf("got %q", b.Line(0))
	}
}

func TestInsertLineBelow(t *testing.T) {
	b := FromBytes([]byte("a\nb\n"))
	b.InsertLineBelow(0, "MID")
	if strings.Join(b.Lines(), "|") != "a|MID|b" {
		t.Errorf("got %v", b.Lines())
	}
}

func TestInsertLineBelowAtTop(t *testing.T) {
	b := FromBytes([]byte("a\nb\n"))
	b.InsertLineBelow(-1, "PRE")
	if strings.Join(b.Lines(), "|") != "PRE|a|b" {
		t.Errorf("got %v", b.Lines())
	}
}

func TestInsertStringWithNewlines(t *testing.T) {
	b := FromBytes([]byte("hello world\n"))
	b.InsertString(0, 5, "\nNEW\n")
	if strings.Join(b.Lines(), "|") != "hello|NEW| world" {
		t.Errorf("got %v", b.Lines())
	}
}

func TestEnsureRowGrows(t *testing.T) {
	b := NewBuffer()
	b.InsertRune(3, 0, 'x')
	if b.LineCount() != 4 {
		t.Errorf("got %d lines", b.LineCount())
	}
	if b.Line(3) != "x" {
		t.Errorf("got %q", b.Line(3))
	}
}

func TestSetLines(t *testing.T) {
	b := FromBytes([]byte("a\nb\n"))
	b.SetLines([]string{"x", "y", "z"})
	if strings.Join(b.Lines(), "|") != "x|y|z" {
		t.Errorf("got %v", b.Lines())
	}
}

func TestSetLinesEmpty(t *testing.T) {
	b := FromBytes([]byte("a\nb\n"))
	b.SetLines(nil)
	if b.LineCount() != 1 || b.Line(0) != "" {
		t.Errorf("got %v", b.Lines())
	}
}
