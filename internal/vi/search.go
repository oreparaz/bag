package vi

// searchAgain advances the cursor to the next match of e.lastPattern.
// forward=true searches downward; false searches upward. The match
// becomes the new cursor position. With no prior pattern (and no
// in-progress search) we silently no-op.
func (e *Editor) searchAgain(forward bool) {
	if e.lastPattern == "" {
		e.msg = "No previous regular expression"
		return
	}
	re := regexpFromPattern(e.lastPattern)
	if re == nil {
		e.msg = "Invalid pattern"
		return
	}

	row := e.row
	col := e.col

	if forward {
		// Skip the current match by advancing one byte.
		col++
		for {
			line := e.buf.Line(row)
			if col > len(line) {
				row++
				col = 0
				if row >= e.buf.LineCount() {
					row = 0
				}
				if row == e.row {
					break
				}
				continue
			}
			loc := re.FindStringIndex(line[col:])
			if loc != nil {
				e.row = row
				e.col = col + loc[0]
				return
			}
			row++
			col = 0
			if row >= e.buf.LineCount() {
				// Wrap to top.
				row = 0
				e.msg = "search hit BOTTOM, continuing at TOP"
			}
			if row == e.row && col >= e.col {
				break
			}
		}
		e.msg = "Pattern not found: " + e.lastPattern
		return
	}

	// Backward search: try current line up to col, then walk upward.
	for i := 0; i < e.buf.LineCount()+1; i++ {
		line := e.buf.Line(row)
		// Find all matches in line and pick the last one whose end is
		// at-or-before col (when on the original row), or just the last
		// (when on a previous row).
		matches := re.FindAllStringIndex(line, -1)
		var pick []int
		if row == e.row {
			for _, m := range matches {
				if m[0] < col {
					pick = m
				}
			}
		} else if len(matches) > 0 {
			pick = matches[len(matches)-1]
		}
		if pick != nil {
			e.row = row
			e.col = pick[0]
			return
		}
		row--
		col = 1 << 30
		if row < 0 {
			row = e.buf.LineCount() - 1
			e.msg = "search hit TOP, continuing at BOTTOM"
		}
	}
	e.msg = "Pattern not found: " + e.lastPattern
}
