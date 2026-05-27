package tr

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"
)

type options struct {
	delete     bool
	squeeze    bool
	complement bool
	truncate   bool // -t truncate SET1 to length of SET2
	set1       string
	set2       string
}

func parseArgs(argv []string) (*options, error) {
	o := &options{}
	var positional []string
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if a == "--" {
			positional = append(positional, argv[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "--") {
			switch a[2:] {
			case "delete":
				o.delete = true
			case "squeeze-repeats":
				o.squeeze = true
			case "complement":
				o.complement = true
			case "truncate-set1":
				o.truncate = true
			default:
				// Ignore unknown long flags.
			}
			continue
		}
		// Short flag clusters. A leading '-' followed by a set char is
		// the start of a SET, not a flag — but tr's SETs are explicit
		// positional args, so we only treat dash-prefixed args of length
		// 1..N where every char is a known short flag as flags.
		if strings.HasPrefix(a, "-") && a != "-" && len(a) > 1 && isAllShortFlags(a) {
			for j := 1; j < len(a); j++ {
				switch a[j] {
				case 'd':
					o.delete = true
				case 's':
					o.squeeze = true
				case 'c', 'C':
					o.complement = true
				case 't':
					o.truncate = true
				default:
				}
			}
			continue
		}
		positional = append(positional, a)
	}
	if len(positional) == 0 {
		return nil, errors.New("missing operand")
	}
	o.set1 = positional[0]
	if len(positional) >= 2 {
		o.set2 = positional[1]
	}
	return o, nil
}

func isAllShortFlags(s string) bool {
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case 'd', 's', 'c', 'C', 't':
		default:
			return false
		}
	}
	return true
}

func run(argv []string) int {
	o, err := parseArgs(argv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tr: %v\n", err)
		return 1
	}

	set1, err := expandSet(o.set1)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tr: %v\n", err)
		return 1
	}
	var set2 []byte
	if o.set2 != "" {
		set2, err = expandSet(o.set2)
		if err != nil {
			fmt.Fprintf(os.Stderr, "tr: %v\n", err)
			return 1
		}
	}

	// Validate flag combinations.
	switch {
	case o.delete && o.squeeze && o.set2 == "":
		return 1
	case o.delete && o.set2 != "" && !o.squeeze:
		fmt.Fprintln(os.Stderr, "tr: extra operand after SET1 when -d is set without -s")
		return 1
	case !o.delete && !o.squeeze && o.set2 == "":
		fmt.Fprintln(os.Stderr, "tr: missing operand SET2 (need -d or -s)")
		return 1
	}

	in := bufio.NewReader(os.Stdin)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	// Pre-build a translation table for the common SET1→SET2 case.
	var table [256]byte
	var inSet1 [256]bool
	for i := 0; i < 256; i++ {
		table[i] = byte(i)
	}
	for _, b := range set1 {
		inSet1[b] = true
	}
	// In -c mode the working set is the complement.
	use := func(b byte) bool { return inSet1[b] }
	if o.complement {
		use = func(b byte) bool { return !inSet1[b] }
	}

	if !o.delete && o.set2 != "" {
		// Build the translation pairing.
		s1 := set1
		s2 := set2
		// If -t, truncate s1 to len(s2). Otherwise pad s2 by repeating
		// its last byte (gnu's behavior for unequal lengths).
		if o.truncate {
			if len(s1) > len(s2) {
				s1 = s1[:len(s2)]
			}
		} else if len(s2) > 0 && len(s2) < len(s1) {
			last := s2[len(s2)-1]
			padded := make([]byte, len(s1))
			copy(padded, s2)
			for k := len(s2); k < len(s1); k++ {
				padded[k] = last
			}
			s2 = padded
		}
		// In -c mode, every byte NOT in SET1 maps to the LAST byte of SET2.
		if o.complement && len(s2) > 0 {
			lastTo := s2[len(s2)-1]
			for i := 0; i < 256; i++ {
				if !inSet1[byte(i)] {
					table[i] = lastTo
				}
			}
		} else {
			for k := 0; k < len(s1) && k < len(s2); k++ {
				table[s1[k]] = s2[k]
			}
		}
	}

	// Squeeze tracking: -s squeezes runs of bytes that are in SET2 (when
	// translating) or in SET1 (when -d or stand-alone -s).
	squeezeSet := &inSet1
	if !o.delete && o.set2 != "" {
		// Compute set2 membership.
		var inSet2 [256]bool
		for _, b := range set2 {
			inSet2[b] = true
		}
		squeezeSet = &inSet2
	}

	buf := make([]byte, 32*1024)
	prev := byte(0)
	havePrev := false
	for {
		n, err := in.Read(buf)
		if n > 0 {
			for i := 0; i < n; i++ {
				b := buf[i]
				if o.delete && use(b) {
					continue
				}
				outByte := b
				if !o.delete && o.set2 != "" {
					outByte = table[b]
				}
				if o.squeeze && havePrev && outByte == prev && squeezeSet[outByte] {
					continue
				}
				out.WriteByte(outByte)
				prev = outByte
				havePrev = true
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			fmt.Fprintf(os.Stderr, "tr: %v\n", err)
			return 1
		}
	}
	return 0
}

// expandSet turns a SET string with ranges, escapes and character
// classes into the flat list of bytes it stands for.
func expandSet(s string) ([]byte, error) {
	var out []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		// Character class [:alpha:] etc.
		if c == '[' && i+1 < len(s) && s[i+1] == ':' {
			end := strings.Index(s[i:], ":]")
			if end < 0 {
				return nil, fmt.Errorf("unterminated character class in %q", s)
			}
			name := s[i+2 : i+end]
			cls, err := expandClass(name)
			if err != nil {
				return nil, err
			}
			out = append(out, cls...)
			i += end + 1
			continue
		}
		// Escape sequence.
		if c == '\\' && i+1 < len(s) {
			esc, advance, err := readEscape(s, i)
			if err != nil {
				return nil, err
			}
			out = append(out, esc)
			i += advance
			continue
		}
		// Range a-z.
		if i+2 < len(s) && s[i+1] == '-' && s[i+2] != '\\' {
			lo := c
			hi := s[i+2]
			if lo > hi {
				return nil, fmt.Errorf("bad range %c-%c", lo, hi)
			}
			for k := int(lo); k <= int(hi); k++ {
				out = append(out, byte(k))
			}
			i += 2
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

// readEscape returns the byte represented by an escape starting at s[i]
// (where s[i] == '\\'), plus how many extra characters past i it
// consumed (so the outer loop adds that to i after incrementing once).
func readEscape(s string, i int) (byte, int, error) {
	if i+1 >= len(s) {
		return '\\', 0, nil
	}
	c := s[i+1]
	switch c {
	case 'a':
		return '\a', 1, nil
	case 'b':
		return '\b', 1, nil
	case 'f':
		return '\f', 1, nil
	case 'n':
		return '\n', 1, nil
	case 'r':
		return '\r', 1, nil
	case 't':
		return '\t', 1, nil
	case 'v':
		return '\v', 1, nil
	case '\\':
		return '\\', 1, nil
	case '/':
		return '/', 1, nil
	}
	// Octal: \OOO (1-3 digits).
	if c >= '0' && c <= '7' {
		end := i + 2
		v := int(c - '0')
		for end < len(s) && end-i < 4 && s[end] >= '0' && s[end] <= '7' {
			v = v*8 + int(s[end]-'0')
			end++
		}
		if v > 255 {
			return 0, 0, fmt.Errorf("octal escape out of range: \\%o", v)
		}
		return byte(v), end - i - 1, nil
	}
	// Unknown escape: pass through verbatim.
	return c, 1, nil
}

func expandClass(name string) ([]byte, error) {
	var out []byte
	check := func(pred func(rune) bool) {
		for i := 0; i < 256; i++ {
			if pred(rune(i)) {
				out = append(out, byte(i))
			}
		}
	}
	switch name {
	case "alpha":
		check(unicode.IsLetter)
	case "upper":
		check(unicode.IsUpper)
	case "lower":
		check(unicode.IsLower)
	case "digit":
		check(unicode.IsDigit)
	case "alnum":
		check(func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) })
	case "space":
		check(unicode.IsSpace)
	case "blank":
		check(func(r rune) bool { return r == ' ' || r == '\t' })
	case "punct":
		check(func(r rune) bool { return unicode.IsPunct(r) || unicode.IsSymbol(r) })
	case "cntrl":
		check(unicode.IsControl)
	case "print":
		check(unicode.IsPrint)
	case "graph":
		check(func(r rune) bool { return unicode.IsPrint(r) && r != ' ' })
	case "xdigit":
		for _, c := range "0123456789abcdefABCDEF" {
			out = append(out, byte(c))
		}
	default:
		return nil, fmt.Errorf("unknown character class %q", name)
	}
	return out, nil
}
