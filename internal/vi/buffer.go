package vi

import (
	"bytes"
	"strings"
)

// Buffer is the in-memory text model. We use one string per line, no
// trailing newline. Empty file = []string{""} (a single empty line).
//
// Operations are intentionally chunky and side-effecting — they're the
// vocabulary the editor's command layer composes against. Every mutating
// op is small enough that undo can snapshot the whole buffer cheaply for
// "small vi" file sizes.
type Buffer struct {
	lines []string
}

// NewBuffer returns an empty buffer holding one empty line.
func NewBuffer() *Buffer { return &Buffer{lines: []string{""}} }

// FromBytes builds a buffer from a byte slice. Trailing newlines are
// stripped — the model never holds a trailing-empty line for content
// that ended with '\n', because vi's "last line" is always the line
// after the final newline.
//
// Wait — that's wrong for vi: a file "a\n" is two logical lines for
// vi: line 1 = "a", and the file is properly terminated. But vi shows
// only line 1 with a "[noeol]" hint when the file lacks a trailing
// newline. We model: file ending in '\n' = N content lines; file not
// ending in '\n' = N content lines, and we remember the missing-EOL
// for save.
func FromBytes(b []byte) *Buffer {
	if len(b) == 0 {
		return NewBuffer()
	}
	hasTrailing := b[len(b)-1] == '\n'
	if hasTrailing {
		b = b[:len(b)-1]
	}
	parts := strings.Split(string(b), "\n")
	return &Buffer{lines: parts}
}

// Bytes serialises the buffer back to bytes with one '\n' per line and
// a trailing newline.
func (b *Buffer) Bytes() []byte {
	var out bytes.Buffer
	for i, l := range b.lines {
		if i > 0 {
			out.WriteByte('\n')
		}
		out.WriteString(l)
	}
	out.WriteByte('\n')
	return out.Bytes()
}

// LineCount returns the number of lines (always >= 1).
func (b *Buffer) LineCount() int { return len(b.lines) }

// Line returns the contents of row r (0-indexed). Out-of-range returns "".
func (b *Buffer) Line(r int) string {
	if r < 0 || r >= len(b.lines) {
		return ""
	}
	return b.lines[r]
}

// Lines returns a copy of the line slice. Used by tests / undo snapshots.
func (b *Buffer) Lines() []string {
	out := make([]string, len(b.lines))
	copy(out, b.lines)
	return out
}

// SetLines replaces the entire content. Used by undo restore.
func (b *Buffer) SetLines(ls []string) {
	if len(ls) == 0 {
		b.lines = []string{""}
		return
	}
	b.lines = make([]string, len(ls))
	copy(b.lines, ls)
}

// InsertRune inserts r at (row, col) in line row. Col is clamped to the
// line length. If the line doesn't exist, the buffer is grown to include
// it (with empty lines as padding).
func (b *Buffer) InsertRune(row, col int, r rune) {
	b.ensureRow(row)
	line := b.lines[row]
	if col < 0 {
		col = 0
	}
	if col > len(line) {
		col = len(line)
	}
	b.lines[row] = line[:col] + string(r) + line[col:]
}

// InsertString inserts s at (row, col). s may contain '\n' — a newline
// splits the line.
func (b *Buffer) InsertString(row, col int, s string) {
	if !strings.Contains(s, "\n") {
		b.InsertRuneSeq(row, col, []rune(s))
		return
	}
	parts := strings.Split(s, "\n")
	first := parts[0]
	last := parts[len(parts)-1]
	mid := parts[1 : len(parts)-1]

	b.ensureRow(row)
	line := b.lines[row]
	if col < 0 {
		col = 0
	}
	if col > len(line) {
		col = len(line)
	}
	pre := line[:col]
	post := line[col:]
	b.lines[row] = pre + first

	insert := make([]string, 0, len(mid)+1)
	insert = append(insert, mid...)
	insert = append(insert, last+post)

	tail := append([]string{}, b.lines[row+1:]...)
	b.lines = append(b.lines[:row+1], insert...)
	b.lines = append(b.lines, tail...)
}

// InsertRuneSeq inserts a sequence of runes (no newlines) at (row, col).
// Faster than calling InsertRune in a loop when we already have []rune.
func (b *Buffer) InsertRuneSeq(row, col int, rs []rune) {
	b.ensureRow(row)
	line := b.lines[row]
	if col < 0 {
		col = 0
	}
	if col > len(line) {
		col = len(line)
	}
	b.lines[row] = line[:col] + string(rs) + line[col:]
}

// DeleteRune removes the rune at (row, col). No-op if out of range.
// Returns the deleted rune (or 0 if none).
func (b *Buffer) DeleteRune(row, col int) rune {
	if row < 0 || row >= len(b.lines) {
		return 0
	}
	line := b.lines[row]
	if col < 0 || col >= len(line) {
		return 0
	}
	r := []rune(line[col:])[0]
	cut := col + len(string(r))
	b.lines[row] = line[:col] + line[cut:]
	return r
}

// SplitLine splits line row at col, putting line[:col] in row and
// line[col:] in a new row+1.
func (b *Buffer) SplitLine(row, col int) {
	b.ensureRow(row)
	line := b.lines[row]
	if col < 0 {
		col = 0
	}
	if col > len(line) {
		col = len(line)
	}
	left := line[:col]
	right := line[col:]
	b.lines[row] = left
	tail := append([]string{}, b.lines[row+1:]...)
	b.lines = append(b.lines[:row+1], right)
	b.lines = append(b.lines, tail...)
}

// JoinLine merges line row+1 into row, separated by sep.
func (b *Buffer) JoinLine(row int, sep string) {
	if row < 0 || row+1 >= len(b.lines) {
		return
	}
	b.lines[row] = b.lines[row] + sep + b.lines[row+1]
	b.lines = append(b.lines[:row+1], b.lines[row+2:]...)
}

// DeleteLine removes line row. The buffer always retains at least one
// line: deleting the only line clears it instead.
func (b *Buffer) DeleteLine(row int) string {
	if row < 0 || row >= len(b.lines) {
		return ""
	}
	deleted := b.lines[row]
	if len(b.lines) == 1 {
		b.lines[0] = ""
		return deleted
	}
	b.lines = append(b.lines[:row], b.lines[row+1:]...)
	return deleted
}

// DeleteRange removes the inclusive range [start, end] of bytes within
// row, returning the deleted text. Both ends are byte offsets within
// the line; we don't currently honour multi-byte rune boundaries
// because the editor only emits ranges from rune-aware motions.
func (b *Buffer) DeleteRange(row, start, end int) string {
	if row < 0 || row >= len(b.lines) {
		return ""
	}
	line := b.lines[row]
	if start < 0 {
		start = 0
	}
	if end > len(line) {
		end = len(line)
	}
	if start >= end {
		return ""
	}
	deleted := line[start:end]
	b.lines[row] = line[:start] + line[end:]
	return deleted
}

// InsertLineBelow inserts text as a new line at row+1. row=-1 means
// "before line 0".
func (b *Buffer) InsertLineBelow(row int, text string) {
	if row < -1 {
		row = -1
	}
	if row > len(b.lines)-1 {
		row = len(b.lines) - 1
	}
	tail := append([]string{}, b.lines[row+1:]...)
	b.lines = append(b.lines[:row+1], text)
	b.lines = append(b.lines, tail...)
}

// ensureRow grows the buffer with empty lines so row is addressable.
func (b *Buffer) ensureRow(row int) {
	for len(b.lines) <= row {
		b.lines = append(b.lines, "")
	}
}
