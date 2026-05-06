//go:build unix

package vi

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/term"
)

// runTUI is the actual interactive loop. It puts the controlling
// terminal into raw mode, hooks SIGWINCH, and pumps keys through
// Editor.Key until Editor returns ErrQuit.
func runTUI(e *Editor) int {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		fmt.Fprintln(os.Stderr, "vi: stdin is not a TTY")
		return 1
	}
	st, err := term.MakeRaw(fd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "vi: makeRaw: %v\n", err)
		return 1
	}
	defer term.Restore(fd, st)

	// Switch to the alt screen and hide cursor on exit-restore.
	io.WriteString(os.Stdout, "\x1b[?1049h\x1b[?25h")
	defer io.WriteString(os.Stdout, "\x1b[?1049l")

	w, h, err := term.GetSize(fd)
	if err != nil {
		w, h = 80, 24
	}
	e.SetSize(h, w)

	// Watch for resize.
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)

	go func() {
		for range winch {
			if w, h, err := term.GetSize(fd); err == nil {
				e.SetSize(h, w)
			}
		}
	}()

	render(e, os.Stdout)

	parser := newKeyParser(bufio.NewReaderSize(os.Stdin, 64))
	for {
		k, err := parser.next()
		if err != nil {
			return 1
		}
		if err := e.Key(k); err != nil {
			if err == ErrQuit {
				return 0
			}
			return 1
		}
		render(e, os.Stdout)
	}
}

// keyParser converts raw input bytes into Key events. ESC sequences are
// handled with a small lookahead: after a bare ESC we peek for a '[' to
// decide whether the user pressed plain ESC (back to Normal mode) or a
// CSI-prefixed key.
type keyParser struct {
	r *bufio.Reader
}

func newKeyParser(r *bufio.Reader) *keyParser { return &keyParser{r: r} }

func (p *keyParser) next() (Key, error) {
	b, err := p.r.ReadByte()
	if err != nil {
		return Key{}, err
	}

	switch {
	case b == 0x1b: // ESC
		return p.parseEscape()
	case b == 0x0d || b == 0x0a:
		return CodeKey(KeyEnter), nil
	case b == 0x09:
		return CodeKey(KeyTab), nil
	case b == 0x7f || b == 0x08:
		return CodeKey(KeyBackspace), nil
	case b < 0x20:
		// Ctrl-letter: 0x01='A' ... 0x1a='Z'.
		return CtrlKey(rune(b + 'a' - 1)), nil
	}
	// Single-byte printable. UTF-8 sequences come through as multiple
	// reads for now; we treat each leading byte as its own rune. Vi
	// editing of multibyte text isn't part of our 80% target.
	return RuneKey(rune(b)), nil
}

// parseEscape handles ESC + lookahead.
func (p *keyParser) parseEscape() (Key, error) {
	// Try a non-blocking peek: if there's another byte ready it's a
	// CSI; otherwise it's a bare ESC. bufio.Reader doesn't expose
	// "any-bytes-buffered" cleanly; we use Buffered() as a proxy.
	if p.r.Buffered() == 0 {
		// Tiny grace: we can't poll the kernel without additional
		// dependencies, so we trust the buffer state. If a terminal
		// emits ESC and the CSI bytes in separate writes within ~1ms,
		// we may misread bare-ESC. In practice modern terminals batch.
		return CodeKey(KeyEsc), nil
	}
	b, err := p.r.ReadByte()
	if err != nil {
		return CodeKey(KeyEsc), nil
	}
	if b != '[' && b != 'O' {
		// ESC + something else: meta-key. Treat ESC as final.
		_ = p.r.UnreadByte()
		return CodeKey(KeyEsc), nil
	}
	// CSI sequence: read until a "final byte" (0x40..0x7e).
	var seq []byte
	for {
		c, err := p.r.ReadByte()
		if err != nil {
			break
		}
		seq = append(seq, c)
		if c >= 0x40 && c <= 0x7e {
			break
		}
	}
	return parseCSI(seq), nil
}

func parseCSI(seq []byte) Key {
	if len(seq) == 0 {
		return CodeKey(KeyEsc)
	}
	final := seq[len(seq)-1]
	body := string(seq[:len(seq)-1])
	switch final {
	case 'A':
		return CodeKey(KeyArrowUp)
	case 'B':
		return CodeKey(KeyArrowDown)
	case 'C':
		return CodeKey(KeyArrowRight)
	case 'D':
		return CodeKey(KeyArrowLeft)
	case 'H':
		return CodeKey(KeyHome)
	case 'F':
		return CodeKey(KeyEnd)
	case '~':
		switch body {
		case "1", "7":
			return CodeKey(KeyHome)
		case "3":
			return CodeKey(KeyDelete)
		case "4", "8":
			return CodeKey(KeyEnd)
		case "5":
			return CodeKey(KeyPageUp)
		case "6":
			return CodeKey(KeyPageDown)
		}
	}
	return CodeKey(KeyEsc)
}

// --- rendering --------------------------------------------------------

const (
	ansiReset       = "\x1b[0m"
	ansiClearScreen = "\x1b[2J"
	ansiHome        = "\x1b[H"
	ansiClearLine   = "\x1b[K"
	ansiHideCursor  = "\x1b[?25l"
	ansiShowCursor  = "\x1b[?25h"
	ansiReverse     = "\x1b[7m"
)

// colorFor maps a SpanKind to an ANSI sequence.
func colorFor(k SpanKind) string {
	switch k {
	case SpanComment:
		return "\x1b[2;36m" // dim cyan
	case SpanString:
		return "\x1b[32m" // green
	case SpanKeyword:
		return "\x1b[1;33m" // bold yellow
	case SpanNumber:
		return "\x1b[35m" // magenta
	}
	return ""
}

// render draws the editor onto w. It always paints a full screen frame,
// which is fine for "small vi" sizes and avoids subtle redraw bugs.
func render(e *Editor, w io.Writer) {
	bw := bufio.NewWriter(w)
	defer bw.Flush()

	bw.WriteString(ansiHideCursor)
	bw.WriteString(ansiHome)

	syntax := pickSyntax(e.File())
	textRows := e.rows - 2
	if textRows < 1 {
		textRows = 1
	}

	for r := 0; r < textRows; r++ {
		row := e.topRow + r
		bw.WriteString(fmt.Sprintf("\x1b[%d;1H", r+1))
		bw.WriteString(ansiClearLine)
		if row >= e.buf.LineCount() {
			bw.WriteString("~")
			continue
		}
		line := e.buf.Line(row)
		spans := syntax.Tokenize(line)
		writeColoredLine(bw, line, spans, e.cols)
	}

	// Status line.
	bw.WriteString(fmt.Sprintf("\x1b[%d;1H", e.rows-1))
	bw.WriteString(ansiClearLine)
	bw.WriteString(ansiReverse)
	bw.WriteString(statusLine(e, e.cols))
	bw.WriteString(ansiReset)

	// Command line / message.
	bw.WriteString(fmt.Sprintf("\x1b[%d;1H", e.rows))
	bw.WriteString(ansiClearLine)
	switch e.mode {
	case ModeCommand:
		bw.WriteByte(':')
		bw.WriteString(e.cmdline)
	case ModeSearch:
		bw.WriteByte(e.cmdlinePfx)
		bw.WriteString(e.cmdline)
	default:
		bw.WriteString(e.msg)
	}

	// Place the visible cursor.
	row := e.row - e.topRow + 1
	col := e.col + 1
	if e.mode == ModeCommand || e.mode == ModeSearch {
		row = e.rows
		col = len(e.cmdline) + 2
	}
	bw.WriteString(fmt.Sprintf("\x1b[%d;%dH", row, col))
	bw.WriteString(ansiShowCursor)
}

// writeColoredLine writes a single buffer line with span colors,
// truncated to maxCols visible columns.
func writeColoredLine(bw *bufio.Writer, line string, spans []Span, maxCols int) {
	if len(line) > maxCols {
		line = line[:maxCols]
	}
	cur := 0
	for _, s := range spans {
		if s.Start >= len(line) {
			break
		}
		end := s.End
		if end > len(line) {
			end = len(line)
		}
		if s.Start > cur {
			bw.WriteString(line[cur:s.Start])
		}
		if c := colorFor(s.Kind); c != "" {
			bw.WriteString(c)
			bw.WriteString(line[s.Start:end])
			bw.WriteString(ansiReset)
		} else {
			bw.WriteString(line[s.Start:end])
		}
		cur = end
	}
	if cur < len(line) {
		bw.WriteString(line[cur:])
	}
}

// statusLine builds a vim-like status line.
func statusLine(e *Editor, width int) string {
	left := e.File()
	if left == "" {
		left = "[No Name]"
	}
	if e.dirty {
		left += " [+]"
	}
	mode := "NORMAL"
	switch e.mode {
	case ModeInsert:
		mode = "INSERT"
	case ModeCommand:
		mode = "CMD"
	case ModeSearch:
		mode = "SEARCH"
	}
	right := fmt.Sprintf(" %s  %d:%d ", mode, e.row+1, e.col+1)
	gap := width - len(left) - len(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}
