package vi

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Mode is the editor's input mode.
type Mode int

const (
	ModeNormal Mode = iota
	ModeInsert
	ModeCommand // ":..."
	ModeSearch  // "/..." or "?..."
)

// ErrQuit is returned from Key when the user requested exit (:q, :wq, ZZ).
var ErrQuit = errors.New("vi: quit")

// Editor is the in-memory state machine. It is intentionally headless:
// callers feed Key events and read state; the terminal driver (in
// terminal.go) is one such caller.
type Editor struct {
	buf   *Buffer
	row   int // cursor row, 0-indexed
	col   int // cursor col within line (byte offset)
	mode  Mode
	file  string
	dirty bool

	// Pending operator: 'd', 'y', 'c', or 'g' (waiting for a second 'g').
	// After an operator we expect a motion or a doubled letter
	// (dd / yy / cc).
	pending byte

	// Pending count for motions / operators (the leading "5" in "5j").
	// When an operator gets queued we move count into opCount and let
	// the *next* count (for the motion) accumulate freshly. Total count
	// is opCount * motionCount.
	count   int
	opCount int

	// Replace pending (after 'r' we wait for one rune).
	awaitingReplace bool

	// : / / / ? input buffers.
	cmdline    string
	cmdlinePfx byte // ':' or '/' or '?' to know which mode is active
	msg        string

	// Last search.
	lastPattern string
	lastForward bool

	// Yank register: "" (default) and named a-z.
	registers map[byte]register

	// Undo / redo.
	undo []snapshot
	redo []snapshot
	// insertCoveredByUndo: we want a single undo snapshot to span an
	// entire insert-mode session. Reset when entering Insert (in
	// keyNormal), set on the first push during Insert.
	insertCoveredByUndo bool

	// Viewport.
	topRow int
	rows   int
	cols   int

	// Quit signal piped out of the dispatcher.
	quit bool
}

type register struct {
	text   string
	isLine bool // dd / yy yank whole lines
}

type snapshot struct {
	lines []string
	row   int
	col   int
}

// NewEditor returns a fresh editor with one empty line.
func NewEditor() *Editor {
	return &Editor{
		buf:       NewBuffer(),
		mode:      ModeNormal,
		registers: map[byte]register{},
		rows:      24,
		cols:      80,
	}
}

// Open reads path into the buffer. If path doesn't exist the buffer is
// empty and we remember the filename for :w.
func (e *Editor) Open(path string) error {
	e.file = path
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			e.buf = NewBuffer()
			return nil
		}
		return err
	}
	e.buf = FromBytes(data)
	return nil
}

// Save writes the buffer back to e.file. With path != "", save-as.
func (e *Editor) Save(path string) error {
	target := path
	if target == "" {
		target = e.file
	}
	if target == "" {
		return errors.New("no file name (use :w PATH)")
	}
	if err := os.WriteFile(target, e.buf.Bytes(), 0o644); err != nil {
		return err
	}
	if path != "" {
		e.file = path
	}
	e.dirty = false
	return nil
}

// SetSize updates the viewport dimensions. The terminal driver calls
// this on resize.
func (e *Editor) SetSize(rows, cols int) {
	if rows < 3 {
		rows = 3
	}
	if cols < 10 {
		cols = 10
	}
	e.rows = rows
	e.cols = cols
}

// Mode returns the current mode (for tests / status line).
func (e *Editor) Mode() Mode { return e.mode }

// Cursor returns (row, col).
func (e *Editor) Cursor() (int, int) { return e.row, e.col }

// Lines returns a snapshot of the buffer contents.
func (e *Editor) Lines() []string { return e.buf.Lines() }

// LineCount is the number of lines in the buffer (always >= 1).
func (e *Editor) LineCount() int { return e.buf.LineCount() }

// Line returns row r's content (or "" if out of range).
func (e *Editor) Line(r int) string { return e.buf.Line(r) }

// Message returns the most recent status message (e.g. "1 line yanked",
// or an error from a :command).
func (e *Editor) Message() string { return e.msg }

// Cmdline returns the in-progress :/ / / ? line.
func (e *Editor) Cmdline() (prefix byte, text string) {
	return e.cmdlinePfx, e.cmdline
}

// Dirty reports whether the buffer has unsaved changes.
func (e *Editor) Dirty() bool { return e.dirty }

// File returns the current filename ("" if none).
func (e *Editor) File() string { return e.file }

// TopRow returns the first visible row in the viewport.
func (e *Editor) TopRow() int { return e.topRow }

// Snapshot captures the buffer + cursor for undo.
func (e *Editor) takeSnapshot() snapshot {
	return snapshot{
		lines: e.buf.Lines(),
		row:   e.row,
		col:   e.col,
	}
}

func (e *Editor) restoreSnapshot(s snapshot) {
	e.buf.SetLines(s.lines)
	e.row = clamp(s.row, 0, e.buf.LineCount()-1)
	e.col = clampCol(s.col, e.buf.Line(e.row), e.mode == ModeInsert)
}

// pushUndo saves a snapshot before an edit.
func (e *Editor) pushUndo() {
	e.undo = append(e.undo, e.takeSnapshot())
	e.redo = nil // any new edit invalidates redo
	e.dirty = true
}

// undoOnce reverts to the last snapshot.
func (e *Editor) undoOnce() {
	if len(e.undo) == 0 {
		e.msg = "Already at oldest change"
		return
	}
	cur := e.takeSnapshot()
	last := e.undo[len(e.undo)-1]
	e.undo = e.undo[:len(e.undo)-1]
	e.redo = append(e.redo, cur)
	e.restoreSnapshot(last)
	e.msg = "1 change"
}

// redoOnce replays an undone change.
func (e *Editor) redoOnce() {
	if len(e.redo) == 0 {
		e.msg = "Already at newest change"
		return
	}
	cur := e.takeSnapshot()
	r := e.redo[len(e.redo)-1]
	e.redo = e.redo[:len(e.redo)-1]
	e.undo = append(e.undo, cur)
	e.restoreSnapshot(r)
	e.msg = "1 change"
}

// clamp / clampCol bounded helpers.
func clamp(v, lo, hi int) int {
	if hi < lo {
		hi = lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// clampCol limits col to the line length. Insert mode allows one
// position past the last character (so you can append at end-of-line).
func clampCol(col int, line string, insert bool) int {
	max := len(line)
	if !insert && max > 0 {
		max--
	}
	if max < 0 {
		max = 0
	}
	if col < 0 {
		col = 0
	}
	if col > max {
		col = max
	}
	return col
}

// fixCursor clamps the cursor after edits.
func (e *Editor) fixCursor() {
	if e.row < 0 {
		e.row = 0
	}
	if e.row >= e.buf.LineCount() {
		e.row = e.buf.LineCount() - 1
	}
	e.col = clampCol(e.col, e.buf.Line(e.row), e.mode == ModeInsert)
}

// ensureCursorInView shifts topRow so e.row is visible.
func (e *Editor) ensureCursorInView() {
	textRows := e.rows - 2 // status + cmdline
	if textRows < 1 {
		textRows = 1
	}
	if e.row < e.topRow {
		e.topRow = e.row
	}
	if e.row >= e.topRow+textRows {
		e.topRow = e.row - textRows + 1
	}
	if e.topRow < 0 {
		e.topRow = 0
	}
}

// regexpFromPattern compiles a search pattern. Falls back to literal on
// invalid regex (matches less-surprising vim behaviour for code tokens
// like "(" without escaping).
func regexpFromPattern(p string) *regexp.Regexp {
	if p == "" {
		return nil
	}
	if re, err := regexp.Compile(p); err == nil {
		return re
	}
	return regexp.MustCompile(regexp.QuoteMeta(p))
}

// debugf is a placeholder hook for diagnostic output during tests.
// Currently unused; kept so future debug needs don't restructure the
// package.
func debugf(format string, args ...any) {
	_ = fmt.Sprintf(format, args...)
}

var _ = strings.Repeat // reserve for syntax/search code added later
