package vi

import (
	"fmt"
	"strings"
	"unicode"
)

// Key dispatches a single keystroke. The exact handler depends on the
// current mode. Returns ErrQuit when the user requested exit.
func (e *Editor) Key(k Key) error {
	defer func() {
		e.fixCursor()
		e.ensureCursorInView()
	}()

	switch e.mode {
	case ModeInsert:
		return e.keyInsert(k)
	case ModeCommand, ModeSearch:
		return e.keyLine(k)
	default:
		return e.keyNormal(k)
	}
}

// keyNormal handles keys in Normal mode. Operators (d/y/c) and counts
// are tracked across calls via e.pending and e.count.
func (e *Editor) keyNormal(k Key) error {
	if e.awaitingReplace {
		e.awaitingReplace = false
		if k.Code == KeyEsc {
			return nil
		}
		if k.Rune != 0 {
			e.replaceCharUnderCursor(k.Rune)
		}
		return nil
	}

	// Convert a few special keys to motions.
	if k.Code != 0 {
		switch k.Code {
		case KeyArrowLeft:
			k = RuneKey('h')
		case KeyArrowRight:
			k = RuneKey('l')
		case KeyArrowUp:
			k = RuneKey('k')
		case KeyArrowDown:
			k = RuneKey('j')
		case KeyHome:
			k = RuneKey('0')
		case KeyEnd:
			k = RuneKey('$')
		case KeyPageDown:
			e.scrollPage(1)
			return nil
		case KeyPageUp:
			e.scrollPage(-1)
			return nil
		case KeyEsc:
			e.pending = 0
			e.count = 0
			e.msg = ""
			return nil
		}
	}

	if k.Ctrl {
		switch k.Rune {
		case 'r':
			e.redoOnce()
			return nil
		case 'd':
			e.scrollPage(1)
			return nil
		case 'u':
			e.scrollPage(-1)
			return nil
		}
		return nil
	}

	r := k.Rune
	if r == 0 {
		return nil
	}

	// Counts: digits 1-9, then 0 only after at least one digit.
	if (r >= '1' && r <= '9') || (r == '0' && e.count > 0) {
		e.count = e.count*10 + int(r-'0')
		return nil
	}
	count := e.count
	hadCount := count > 0
	if count == 0 {
		count = 1
	}
	e.count = 0

	// Operator/double-letter pending (d, y, c, g). Handle 'g' here as a
	// double-letter motion; everything else routes through applyOperator.
	if e.pending != 0 {
		if e.pending == 'g' {
			e.pending = 0
			if r == 'g' {
				e.row = 0
				e.col = firstNonBlank(e.buf.Line(0))
				return nil
			}
			// 'g' followed by an unsupported key: silently cancel.
			return nil
		}
		// Total count = opCount * motionCount.
		total := count
		if e.opCount > 0 {
			total = e.opCount * count
		}
		e.opCount = 0
		return e.applyOperator(e.pending, r, total)
	}

	switch r {
	// Movement.
	case 'h':
		e.moveLeft(count)
	case 'l':
		e.moveRight(count)
	case 'j':
		e.moveDown(count)
	case 'k':
		e.moveUp(count)
	case 'w':
		for i := 0; i < count; i++ {
			e.moveWordForward()
		}
	case 'b':
		for i := 0; i < count; i++ {
			e.moveWordBackward()
		}
	case 'e':
		for i := 0; i < count; i++ {
			e.moveWordEnd()
		}
	case '0':
		e.col = 0
	case '^':
		e.col = firstNonBlank(e.buf.Line(e.row))
	case '$':
		e.col = lastCol(e.buf.Line(e.row), false)
	case 'G':
		if hadCount {
			e.row = clamp(count-1, 0, e.buf.LineCount()-1)
		} else {
			e.row = e.buf.LineCount() - 1
		}
		e.col = firstNonBlank(e.buf.Line(e.row))
	case 'g':
		// First 'g' — queue and wait for the next key.
		e.pending = 'g'
		return nil

	// Insertion entry points.
	case 'i':
		e.mode = ModeInsert
		e.insertCoveredByUndo = false
	case 'I':
		e.col = firstNonBlank(e.buf.Line(e.row))
		e.mode = ModeInsert
		e.insertCoveredByUndo = false
	case 'a':
		if len(e.buf.Line(e.row)) > 0 {
			e.col++
		}
		e.mode = ModeInsert
		e.insertCoveredByUndo = false
	case 'A':
		e.col = len(e.buf.Line(e.row))
		e.mode = ModeInsert
		e.insertCoveredByUndo = false
	case 'o':
		e.pushUndo()
		e.buf.InsertLineBelow(e.row, "")
		e.row++
		e.col = 0
		e.mode = ModeInsert
		e.insertCoveredByUndo = true // pushUndo above already covers
	case 'O':
		e.pushUndo()
		e.buf.InsertLineBelow(e.row-1, "")
		e.col = 0
		e.mode = ModeInsert
		e.insertCoveredByUndo = true

	// Single-shot edits.
	case 'x':
		e.pushUndo()
		for i := 0; i < count; i++ {
			d := e.buf.DeleteRune(e.row, e.col)
			if d == 0 {
				break
			}
		}
	case 'X':
		e.pushUndo()
		for i := 0; i < count; i++ {
			if e.col == 0 {
				break
			}
			e.col--
			e.buf.DeleteRune(e.row, e.col)
		}
	case 'r':
		e.awaitingReplace = true
	case 'D':
		e.pushUndo()
		text := e.buf.DeleteRange(e.row, e.col, len(e.buf.Line(e.row)))
		e.registers[0] = register{text: text}
	case 'C':
		e.pushUndo()
		text := e.buf.DeleteRange(e.row, e.col, len(e.buf.Line(e.row)))
		e.registers[0] = register{text: text}
		e.mode = ModeInsert
	case 'Y':
		text := e.buf.Line(e.row)
		e.registers[0] = register{text: text, isLine: true}
		e.msg = "1 line yanked"
	case 'p':
		e.pushUndo()
		e.pasteAfter(e.registers[0])
	case 'P':
		e.pushUndo()
		e.pasteBefore(e.registers[0])
	case 'J':
		if e.row+1 < e.buf.LineCount() {
			e.pushUndo()
			right := e.buf.Line(e.row + 1)
			sep := " "
			// Vim's J trims leading whitespace from the next line first.
			right = strings.TrimLeft(right, " \t")
			if right == "" {
				sep = ""
			}
			joined := e.buf.Line(e.row) + sep + right
			endCol := len(e.buf.Line(e.row))
			e.buf.SetLines(replaceTwoLines(e.buf.Lines(), e.row, joined))
			e.col = endCol
		}

	// Operators that need a motion. Carry the count so 2dw deletes 2
	// words and 2dd / 5dd delete 2 / 5 lines.
	case 'd', 'y', 'c':
		e.pending = byte(r)
		e.opCount = count

	// Undo/redo.
	case 'u':
		e.undoOnce()

	// Mode switches.
	case ':':
		e.mode = ModeCommand
		e.cmdline = ""
		e.cmdlinePfx = ':'
	case '/':
		e.mode = ModeSearch
		e.cmdline = ""
		e.cmdlinePfx = '/'
	case '?':
		e.mode = ModeSearch
		e.cmdline = ""
		e.cmdlinePfx = '?'

	// Search next/prev.
	case 'n':
		e.searchAgain(e.lastForward)
	case 'N':
		e.searchAgain(!e.lastForward)
	}
	return nil
}

func (e *Editor) keyInsert(k Key) error {
	switch {
	case k.Code == KeyEsc:
		// Vim moves the cursor one left when leaving insert mode (unless
		// we're already at col 0).
		e.mode = ModeNormal
		e.insertCoveredByUndo = false
		if e.col > 0 {
			e.col--
		}
		return nil
	case k.Code == KeyEnter || k.Rune == '\n':
		e.pushUndoOnceForInsert()
		e.buf.SplitLine(e.row, e.col)
		e.row++
		e.col = 0
		return nil
	case k.Code == KeyBackspace || k.Rune == 0x7f || k.Rune == 0x08:
		e.pushUndoOnceForInsert()
		if e.col > 0 {
			e.col--
			e.buf.DeleteRune(e.row, e.col)
		} else if e.row > 0 {
			prevLen := len(e.buf.Line(e.row - 1))
			e.buf.JoinLine(e.row-1, "")
			e.row--
			e.col = prevLen
		}
		return nil
	case k.Code == KeyTab || k.Rune == '\t':
		e.pushUndoOnceForInsert()
		e.buf.InsertRune(e.row, e.col, '\t')
		e.col++
		return nil
	case k.Code == KeyDelete:
		e.pushUndoOnceForInsert()
		e.buf.DeleteRune(e.row, e.col)
		return nil
	case k.Code == KeyArrowLeft:
		if e.col > 0 {
			e.col--
		}
		return nil
	case k.Code == KeyArrowRight:
		if e.col < len(e.buf.Line(e.row)) {
			e.col++
		}
		return nil
	case k.Code == KeyArrowUp:
		if e.row > 0 {
			e.row--
		}
		return nil
	case k.Code == KeyArrowDown:
		if e.row+1 < e.buf.LineCount() {
			e.row++
		}
		return nil
	}

	if k.Rune != 0 && (unicode.IsPrint(k.Rune) || k.Rune == '\t') {
		e.pushUndoOnceForInsert()
		e.buf.InsertRune(e.row, e.col, k.Rune)
		e.col++
	}
	return nil
}

// pushUndoOnceForInsert ensures a single undo snapshot covers an entire
// insert-mode session: we snapshot only on the first edit after entering
// Insert mode. The insertCoveredByUndo flag is set on first push and
// reset whenever we transition back to Normal.
func (e *Editor) pushUndoOnceForInsert() {
	if e.insertCoveredByUndo {
		e.dirty = true
		return
	}
	e.pushUndo()
	e.insertCoveredByUndo = true
}

// keyLine handles keys while typing in : / / / ? command line.
func (e *Editor) keyLine(k Key) error {
	switch {
	case k.Code == KeyEsc:
		e.cmdline = ""
		e.cmdlinePfx = 0
		e.mode = ModeNormal
		return nil
	case k.Code == KeyEnter || k.Rune == '\n':
		return e.executeCmdline()
	case k.Code == KeyBackspace || k.Rune == 0x7f || k.Rune == 0x08:
		if len(e.cmdline) == 0 {
			e.mode = ModeNormal
			e.cmdlinePfx = 0
			return nil
		}
		e.cmdline = e.cmdline[:len(e.cmdline)-1]
		return nil
	}
	if k.Rune != 0 && (unicode.IsPrint(k.Rune) || k.Rune == '\t') {
		e.cmdline += string(k.Rune)
	}
	return nil
}

func (e *Editor) executeCmdline() error {
	pfx := e.cmdlinePfx
	body := e.cmdline
	e.cmdline = ""
	e.cmdlinePfx = 0
	e.mode = ModeNormal

	switch pfx {
	case ':':
		return e.runExCommand(body)
	case '/':
		e.lastPattern = body
		e.lastForward = true
		e.searchAgain(true)
		return nil
	case '?':
		e.lastPattern = body
		e.lastForward = false
		e.searchAgain(false)
		return nil
	}
	return nil
}

func (e *Editor) runExCommand(cmd string) error {
	cmd = strings.TrimSpace(cmd)
	switch {
	case cmd == "q":
		if e.dirty {
			e.msg = "E37: No write since last change (use :q!)"
			return nil
		}
		return ErrQuit
	case cmd == "q!":
		return ErrQuit
	case cmd == "w":
		if err := e.Save(""); err != nil {
			e.msg = err.Error()
			return nil
		}
		e.msg = fmt.Sprintf("\"%s\" %d lines written", e.file, e.buf.LineCount())
		return nil
	case strings.HasPrefix(cmd, "w "):
		path := strings.TrimSpace(cmd[2:])
		if err := e.Save(path); err != nil {
			e.msg = err.Error()
			return nil
		}
		e.msg = fmt.Sprintf("\"%s\" %d lines written", path, e.buf.LineCount())
		return nil
	case cmd == "wq" || cmd == "x":
		if err := e.Save(""); err != nil {
			e.msg = err.Error()
			return nil
		}
		return ErrQuit
	}
	e.msg = "E492: Not an editor command: " + cmd
	return nil
}

// applyOperator handles the second key of an operator+motion combo.
// motion may also be a doubled letter (dd, yy, cc) which means
// "operate on the current line".
func (e *Editor) applyOperator(op byte, motion rune, count int) error {
	defer func() { e.pending = 0 }()
	if byte(motion) == op {
		// Doubled: line operation.
		switch op {
		case 'd':
			e.pushUndo()
			text := ""
			for i := 0; i < count; i++ {
				if i > 0 {
					text += "\n"
				}
				text += e.buf.Line(e.row)
				e.buf.DeleteLine(e.row)
			}
			e.registers[0] = register{text: text, isLine: true}
			if e.row >= e.buf.LineCount() {
				e.row = e.buf.LineCount() - 1
			}
			e.msg = fmt.Sprintf("%d lines deleted", count)
		case 'y':
			text := ""
			for i := 0; i < count; i++ {
				if i > 0 {
					text += "\n"
				}
				if e.row+i < e.buf.LineCount() {
					text += e.buf.Line(e.row + i)
				}
			}
			e.registers[0] = register{text: text, isLine: true}
			e.msg = fmt.Sprintf("%d lines yanked", count)
		case 'c':
			e.pushUndo()
			text := e.buf.Line(e.row)
			e.registers[0] = register{text: text, isLine: false}
			ls := e.buf.Lines()
			ls[e.row] = ""
			e.buf.SetLines(ls)
			e.col = 0
			e.mode = ModeInsert
		}
		return nil
	}

	// Operator + motion. We compute the byte range the motion covers,
	// then apply.
	startRow, startCol := e.row, e.col
	for i := 0; i < count; i++ {
		switch motion {
		case 'w':
			e.moveWordForward()
		case 'b':
			e.moveWordBackward()
		case 'e':
			e.moveWordEnd()
		case 'l':
			e.moveRight(1)
		case 'h':
			e.moveLeft(1)
		case '$':
			e.col = len(e.buf.Line(e.row))
		case '0':
			e.col = 0
		case '^':
			e.col = firstNonBlank(e.buf.Line(e.row))
		default:
			// Unknown motion: cancel.
			e.row, e.col = startRow, startCol
			return nil
		}
	}
	endRow, endCol := e.row, e.col
	// For word motions ('w', 'e', 'b'), vim's operator-pending mode
	// stops at end-of-line rather than descending into the next line.
	// Without this clamp `dw` at end-of-line in a multi-line buffer
	// would treat the range as multi-line and delete the next whole
	// line.
	if (motion == 'w' || motion == 'e' || motion == 'b') && endRow != startRow {
		endRow = startRow
		endCol = len(e.buf.Line(startRow))
		e.row, e.col = endRow, endCol
	}
	// Restore cursor for "yank" semantics; "delete"/"change" keep end.
	if startRow == endRow {
		lo, hi := startCol, endCol
		if lo > hi {
			lo, hi = hi, lo
		}
		switch op {
		case 'd':
			e.pushUndo()
			text := e.buf.DeleteRange(startRow, lo, hi)
			e.registers[0] = register{text: text}
			e.row, e.col = startRow, lo
		case 'y':
			text := e.buf.Line(startRow)
			if hi > len(text) {
				hi = len(text)
			}
			e.registers[0] = register{text: text[lo:hi]}
			e.row, e.col = startRow, startCol
			e.msg = "yanked"
		case 'c':
			e.pushUndo()
			text := e.buf.DeleteRange(startRow, lo, hi)
			e.registers[0] = register{text: text}
			e.row, e.col = startRow, lo
			e.mode = ModeInsert
		}
	} else {
		// Multi-line motions — for the 80% target we collapse them to
		// "delete from start to end of file or line" via a simpler
		// strategy: yank/delete whole lines [startRow, endRow].
		lo, hi := startRow, endRow
		if lo > hi {
			lo, hi = hi, lo
		}
		switch op {
		case 'd':
			e.pushUndo()
			var deleted []string
			for i := lo; i <= hi; i++ {
				deleted = append(deleted, e.buf.Line(lo))
				e.buf.DeleteLine(lo)
			}
			e.registers[0] = register{text: strings.Join(deleted, "\n"), isLine: true}
			e.row = clamp(lo, 0, e.buf.LineCount()-1)
			e.col = 0
		case 'y':
			var lines []string
			for i := lo; i <= hi; i++ {
				lines = append(lines, e.buf.Line(i))
			}
			e.registers[0] = register{text: strings.Join(lines, "\n"), isLine: true}
			e.row, e.col = startRow, startCol
		case 'c':
			e.pushUndo()
			for i := lo; i <= hi; i++ {
				e.buf.DeleteLine(lo)
			}
			e.row = clamp(lo, 0, e.buf.LineCount()-1)
			e.col = 0
			e.mode = ModeInsert
		}
	}
	return nil
}

// pasteAfter pastes register r after the cursor (or below the line for
// linewise yanks).
func (e *Editor) pasteAfter(r register) {
	if r.text == "" {
		return
	}
	if r.isLine {
		for i, line := range strings.Split(r.text, "\n") {
			e.buf.InsertLineBelow(e.row+i, line)
		}
		e.row++
		e.col = firstNonBlank(e.buf.Line(e.row))
		return
	}
	// Charwise: insert after cursor column.
	col := e.col
	if len(e.buf.Line(e.row)) > 0 {
		col++
	}
	e.buf.InsertString(e.row, col, r.text)
	// Move cursor to last character of the pasted text.
	e.col = col + len(r.text) - 1
}

// pasteBefore pastes register r before the cursor.
func (e *Editor) pasteBefore(r register) {
	if r.text == "" {
		return
	}
	if r.isLine {
		for i, line := range strings.Split(r.text, "\n") {
			e.buf.InsertLineBelow(e.row+i-1, line)
		}
		e.col = firstNonBlank(e.buf.Line(e.row))
		return
	}
	e.buf.InsertString(e.row, e.col, r.text)
	e.col += len(r.text) - 1
	if e.col < 0 {
		e.col = 0
	}
}

func (e *Editor) replaceCharUnderCursor(r rune) {
	if e.row >= e.buf.LineCount() {
		return
	}
	line := e.buf.Line(e.row)
	if e.col >= len(line) {
		return
	}
	e.pushUndo()
	e.buf.DeleteRune(e.row, e.col)
	e.buf.InsertRune(e.row, e.col, r)
}

func (e *Editor) scrollPage(dir int) {
	step := e.rows - 4
	if step < 1 {
		step = 1
	}
	e.row += step * dir
	e.row = clamp(e.row, 0, e.buf.LineCount()-1)
}

// moveLeft / moveRight / moveDown / moveUp move count cells.
func (e *Editor) moveLeft(count int) {
	for i := 0; i < count && e.col > 0; i++ {
		e.col--
	}
}
func (e *Editor) moveRight(count int) {
	max := lastCol(e.buf.Line(e.row), false)
	for i := 0; i < count && e.col < max; i++ {
		e.col++
	}
}
func (e *Editor) moveDown(count int) {
	for i := 0; i < count && e.row+1 < e.buf.LineCount(); i++ {
		e.row++
	}
}
func (e *Editor) moveUp(count int) {
	for i := 0; i < count && e.row > 0; i++ {
		e.row--
	}
}

// moveWordForward jumps to the start of the next word.
func (e *Editor) moveWordForward() {
	line := e.buf.Line(e.row)
	col := e.col
	// Skip current word characters.
	for col < len(line) && isWordChar(line[col]) {
		col++
	}
	// Skip whitespace / punct.
	for col < len(line) && !isWordChar(line[col]) {
		col++
	}
	if col < len(line) {
		e.col = col
		return
	}
	// End of line: descend.
	if e.row+1 < e.buf.LineCount() {
		e.row++
		next := e.buf.Line(e.row)
		i := 0
		for i < len(next) && !isWordChar(next[i]) {
			i++
		}
		e.col = i
	} else {
		e.col = len(line)
	}
}

func (e *Editor) moveWordBackward() {
	if e.col == 0 {
		if e.row == 0 {
			return
		}
		e.row--
		e.col = len(e.buf.Line(e.row))
	}
	line := e.buf.Line(e.row)
	col := e.col
	if col > 0 {
		col--
	}
	// Skip whitespace.
	for col > 0 && !isWordChar(line[col]) {
		col--
	}
	// Walk to the start of this word.
	for col > 0 && isWordChar(line[col-1]) {
		col--
	}
	e.col = col
}

func (e *Editor) moveWordEnd() {
	line := e.buf.Line(e.row)
	col := e.col
	if col < len(line)-1 {
		col++
	}
	for col < len(line) && !isWordChar(line[col]) {
		col++
	}
	for col+1 < len(line) && isWordChar(line[col+1]) {
		col++
	}
	e.col = col
}

func isWordChar(b byte) bool {
	return (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') ||
		b == '_'
}

func firstNonBlank(line string) int {
	for i := 0; i < len(line); i++ {
		if line[i] != ' ' && line[i] != '\t' {
			return i
		}
	}
	return 0
}

// lastCol returns the highest valid column for normal mode (last char) or
// insert mode (one past last char).
func lastCol(line string, insert bool) int {
	if line == "" {
		return 0
	}
	if insert {
		return len(line)
	}
	return len(line) - 1
}

// replaceTwoLines is a tiny helper for J: replace lines [row, row+1]
// with a single joined line. Returns the new lines slice.
func replaceTwoLines(lines []string, row int, joined string) []string {
	out := make([]string, 0, len(lines)-1)
	out = append(out, lines[:row]...)
	out = append(out, joined)
	if row+2 < len(lines) {
		out = append(out, lines[row+2:]...)
	}
	return out
}
