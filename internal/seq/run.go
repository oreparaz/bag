package seq

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
)

type options struct {
	separator   string
	equalWidth  bool
	format      string
	numbers     []string
}

func parseArgs(argv []string) (*options, error) {
	o := &options{separator: "\n"}
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if a == "--" {
			o.numbers = append(o.numbers, argv[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "--") {
			name := a[2:]
			val := ""
			hasEq := false
			if eq := strings.IndexByte(name, '='); eq >= 0 {
				val = name[eq+1:]
				name = name[:eq]
				hasEq = true
			}
			switch name {
			case "separator":
				if !hasEq {
					if i+1 >= len(argv) {
						return nil, errors.New("--separator requires an argument")
					}
					i++
					val = argv[i]
				}
				o.separator = val
			case "equal-width":
				o.equalWidth = true
			case "format":
				if !hasEq {
					if i+1 >= len(argv) {
						return nil, errors.New("--format requires an argument")
					}
					i++
					val = argv[i]
				}
				o.format = val
			default:
				// Ignore unknown long flags.
			}
			continue
		}
		// Short flag arguments: -s SEP, -f FMT, -w.
		// A bare "-" with a number after is a negative number, not a flag.
		if strings.HasPrefix(a, "-") && a != "-" && len(a) > 1 && !looksLikeNumber(a) {
			for j := 1; j < len(a); j++ {
				switch a[j] {
				case 's':
					var val string
					if j+1 < len(a) {
						val = a[j+1:]
						j = len(a)
					} else {
						if i+1 >= len(argv) {
							return nil, errors.New("-s requires an argument")
						}
						i++
						val = argv[i]
					}
					o.separator = val
				case 'w':
					o.equalWidth = true
				case 'f':
					var val string
					if j+1 < len(a) {
						val = a[j+1:]
						j = len(a)
					} else {
						if i+1 >= len(argv) {
							return nil, errors.New("-f requires an argument")
						}
						i++
						val = argv[i]
					}
					o.format = val
				default:
					// Ignore unknown short flags.
				}
			}
			continue
		}
		o.numbers = append(o.numbers, a)
	}
	if len(o.numbers) == 0 {
		return nil, errors.New("missing operand")
	}
	if len(o.numbers) > 3 {
		return nil, errors.New("too many operands (max 3)")
	}
	return o, nil
}

// looksLikeNumber distinguishes a negative-number argument from a flag
// cluster. We accept optional leading +/-, then digits/dot/e.
func looksLikeNumber(s string) bool {
	if len(s) < 2 {
		return false
	}
	if s[0] != '-' && s[0] != '+' {
		return false
	}
	for _, c := range s[1:] {
		if !(c >= '0' && c <= '9') && c != '.' && c != 'e' && c != 'E' && c != '+' && c != '-' {
			return false
		}
	}
	return true
}

func run(argv []string) int {
	o, err := parseArgs(argv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "seq: %v\n", err)
		return 1
	}
	first, inc, last, err := resolveTriple(o.numbers)
	if err != nil {
		fmt.Fprintf(os.Stderr, "seq: %v\n", err)
		return 1
	}
	if inc == 0 {
		fmt.Fprintln(os.Stderr, "seq: increment must be non-zero")
		return 1
	}

	// Determine printf format and pad width.
	printfFmt := o.format
	width := 0
	if o.equalWidth {
		width = maxPrintedWidth(first, inc, last, isInteger(first) && isInteger(inc) && isInteger(last))
	}
	if printfFmt == "" {
		// Pick %g for floats, %d for integers — matches gnu enough.
		if isInteger(first) && isInteger(inc) && isInteger(last) {
			if width > 0 {
				printfFmt = fmt.Sprintf("%%0%dd", width)
			} else {
				printfFmt = "%d"
			}
		} else {
			prec := maxFractionDigits(first, inc, last)
			if width > 0 {
				printfFmt = fmt.Sprintf("%%0%d.%df", width, prec)
			} else {
				printfFmt = fmt.Sprintf("%%.%df", prec)
			}
		}
	}

	w := os.Stdout
	emitOne := func(v float64) {
		if isIntegerFmt(printfFmt) {
			fmt.Fprintf(w, printfFmt, int64(v))
		} else {
			fmt.Fprintf(w, printfFmt, v)
		}
	}

	// Step direction: positive inc requires last >= first; negative
	// inc requires last <= first. Otherwise produce no output, exit 0.
	if inc > 0 {
		first2 := first
		count := 0
		for first2 <= last+1e-12 {
			if count > 0 {
				fmt.Fprint(w, o.separator)
			}
			emitOne(first2)
			first2 += inc
			count++
		}
		if count > 0 {
			fmt.Fprintln(w)
		}
	} else {
		first2 := first
		count := 0
		for first2 >= last-1e-12 {
			if count > 0 {
				fmt.Fprint(w, o.separator)
			}
			emitOne(first2)
			first2 += inc
			count++
		}
		if count > 0 {
			fmt.Fprintln(w)
		}
	}
	return 0
}

func resolveTriple(nums []string) (first, inc, last float64, err error) {
	parse := func(s string) (float64, error) {
		v, e := strconv.ParseFloat(s, 64)
		if e != nil {
			return 0, fmt.Errorf("invalid floating point number: %q", s)
		}
		return v, nil
	}
	switch len(nums) {
	case 1:
		l, e := parse(nums[0])
		if e != nil {
			return 0, 0, 0, e
		}
		return 1, 1, l, nil
	case 2:
		f, e := parse(nums[0])
		if e != nil {
			return 0, 0, 0, e
		}
		l, e := parse(nums[1])
		if e != nil {
			return 0, 0, 0, e
		}
		return f, 1, l, nil
	case 3:
		f, e := parse(nums[0])
		if e != nil {
			return 0, 0, 0, e
		}
		ic, e := parse(nums[1])
		if e != nil {
			return 0, 0, 0, e
		}
		l, e := parse(nums[2])
		if e != nil {
			return 0, 0, 0, e
		}
		return f, ic, l, nil
	}
	return 0, 0, 0, errors.New("missing operand")
}

func isInteger(v float64) bool { return math.Trunc(v) == v && !math.IsInf(v, 0) }

func maxFractionDigits(vals ...float64) int {
	max := 0
	for _, v := range vals {
		s := strconv.FormatFloat(v, 'f', -1, 64)
		if i := strings.Index(s, "."); i >= 0 {
			n := len(s) - i - 1
			if n > max {
				max = n
			}
		}
	}
	return max
}

func maxPrintedWidth(first, inc, last float64, intMode bool) int {
	candidates := []float64{first, last}
	// If inc < 0, the smallest value in the sequence is `last`; else first.
	w := 0
	for _, v := range candidates {
		var s string
		if intMode {
			s = strconv.FormatInt(int64(v), 10)
		} else {
			s = strconv.FormatFloat(v, 'f', -1, 64)
		}
		// Width counts the leading minus too in gnu seq -w.
		if len(s) > w {
			w = len(s)
		}
	}
	return w
}

func isIntegerFmt(f string) bool {
	return strings.HasSuffix(f, "d")
}
