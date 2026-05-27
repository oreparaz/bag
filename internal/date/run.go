package date

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type options struct {
	utc       bool
	rfc       bool
	iso       string // "", "date", "hours", "minutes", "seconds"
	dateStr   string
	refFile   string
	format    string
}

func parseArgs(argv []string) (*options, error) {
	o := &options{}
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if a == "--" {
			continue
		}
		if strings.HasPrefix(a, "+") {
			o.format = a[1:]
			continue
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
			case "utc", "universal":
				o.utc = true
			case "rfc-email", "rfc-2822":
				o.rfc = true
			case "iso-8601":
				if hasEq {
					o.iso = val
				} else {
					o.iso = "date"
				}
			case "date":
				if !hasEq {
					if i+1 >= len(argv) {
						return nil, errors.New("--date requires an argument")
					}
					i++
					val = argv[i]
				}
				o.dateStr = val
			case "reference":
				if !hasEq {
					if i+1 >= len(argv) {
						return nil, errors.New("--reference requires an argument")
					}
					i++
					val = argv[i]
				}
				o.refFile = val
			default:
				// Ignore unknown long flags.
			}
			continue
		}
		if strings.HasPrefix(a, "-") && a != "-" && len(a) > 1 {
			for j := 1; j < len(a); j++ {
				switch a[j] {
				case 'u':
					o.utc = true
				case 'R':
					o.rfc = true
				case 'I':
					o.iso = "date"
				case 'd':
					if j+1 < len(a) {
						o.dateStr = a[j+1:]
						j = len(a)
					} else {
						if i+1 >= len(argv) {
							return nil, errors.New("-d requires an argument")
						}
						i++
						o.dateStr = argv[i]
					}
				case 'r':
					if j+1 < len(a) {
						o.refFile = a[j+1:]
						j = len(a)
					} else {
						if i+1 >= len(argv) {
							return nil, errors.New("-r requires an argument")
						}
						i++
						o.refFile = argv[i]
					}
				default:
					// Ignore unknown short flags.
				}
			}
			continue
		}
		// Unknown positional argv: GNU treats it as the format if no +
		// prefix was given to the FIRST arg; we just ignore for safety.
	}
	return o, nil
}

func run(argv []string) int {
	o, err := parseArgs(argv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "date: %v\n", err)
		return 1
	}
	t, err := resolveTime(o)
	if err != nil {
		fmt.Fprintf(os.Stderr, "date: %v\n", err)
		return 1
	}
	if o.utc {
		t = t.UTC()
	}
	out := format(t, o)
	fmt.Fprintln(os.Stdout, out)
	return 0
}

func resolveTime(o *options) (time.Time, error) {
	switch {
	case o.refFile != "":
		st, err := os.Stat(o.refFile)
		if err != nil {
			return time.Time{}, err
		}
		return st.ModTime(), nil
	case o.dateStr != "":
		return parseDateString(o.dateStr)
	}
	return time.Now(), nil
}

// parseDateString handles the most common --date forms. We don't
// reimplement gnu's natural-language parser ("yesterday", "next
// Tuesday"); we accept RFC 3339 / 5322, "@unix-seconds", and the
// strftime-like formats we emit ourselves.
func parseDateString(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "@") {
		secs, err := strconv.ParseInt(s[1:], 10, 64)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid epoch %q: %w", s, err)
		}
		return time.Unix(secs, 0), nil
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02",
		time.RFC1123Z,
		time.RFC1123,
		time.RFC822Z,
		time.RFC822,
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized date: %q", s)
}

func format(t time.Time, o *options) string {
	switch {
	case o.rfc:
		return t.Format(time.RFC1123Z)
	case o.iso != "":
		return formatISO(t, o.iso)
	case o.format != "":
		return strftime(t, o.format)
	}
	// Default GNU "date" output: "Mon Jan  2 15:04:05 TZ 2006"
	return t.Format("Mon Jan  2 15:04:05 MST 2006")
}

func formatISO(t time.Time, prec string) string {
	switch prec {
	case "", "date":
		return t.Format("2006-01-02")
	case "hours":
		return t.Format("2006-01-02T15-07:00")
	case "minutes":
		return t.Format("2006-01-02T15:04-07:00")
	case "seconds":
		return t.Format("2006-01-02T15:04:05-07:00")
	case "ns":
		return t.Format("2006-01-02T15:04:05.000000000-07:00")
	}
	return t.Format("2006-01-02")
}

// strftime implements the subset of strftime conversions documented
// in the package doc. Unrecognized %X sequences are passed through
// verbatim.
func strftime(t time.Time, fmtStr string) string {
	var b strings.Builder
	for i := 0; i < len(fmtStr); i++ {
		c := fmtStr[i]
		if c != '%' || i+1 >= len(fmtStr) {
			b.WriteByte(c)
			continue
		}
		i++
		switch fmtStr[i] {
		case 'a':
			b.WriteString(t.Format("Mon"))
		case 'A':
			b.WriteString(t.Format("Monday"))
		case 'b', 'h':
			b.WriteString(t.Format("Jan"))
		case 'B':
			b.WriteString(t.Format("January"))
		case 'c':
			b.WriteString(t.Format("Mon Jan  2 15:04:05 2006"))
		case 'C':
			b.WriteString(fmt.Sprintf("%02d", t.Year()/100))
		case 'd':
			b.WriteString(fmt.Sprintf("%02d", t.Day()))
		case 'D':
			b.WriteString(t.Format("01/02/06"))
		case 'e':
			b.WriteString(fmt.Sprintf("%2d", t.Day()))
		case 'F':
			b.WriteString(t.Format("2006-01-02"))
		case 'H':
			b.WriteString(fmt.Sprintf("%02d", t.Hour()))
		case 'I':
			h := t.Hour() % 12
			if h == 0 {
				h = 12
			}
			b.WriteString(fmt.Sprintf("%02d", h))
		case 'j':
			b.WriteString(fmt.Sprintf("%03d", t.YearDay()))
		case 'm':
			b.WriteString(fmt.Sprintf("%02d", int(t.Month())))
		case 'M':
			b.WriteString(fmt.Sprintf("%02d", t.Minute()))
		case 'n':
			b.WriteByte('\n')
		case 'N':
			b.WriteString(fmt.Sprintf("%09d", t.Nanosecond()))
		case 'p':
			if t.Hour() < 12 {
				b.WriteString("AM")
			} else {
				b.WriteString("PM")
			}
		case 'r':
			h := t.Hour() % 12
			if h == 0 {
				h = 12
			}
			ampm := "AM"
			if t.Hour() >= 12 {
				ampm = "PM"
			}
			b.WriteString(fmt.Sprintf("%02d:%02d:%02d %s", h, t.Minute(), t.Second(), ampm))
		case 'R':
			b.WriteString(t.Format("15:04"))
		case 's':
			b.WriteString(strconv.FormatInt(t.Unix(), 10))
		case 'S':
			b.WriteString(fmt.Sprintf("%02d", t.Second()))
		case 't':
			b.WriteByte('\t')
		case 'T':
			b.WriteString(t.Format("15:04:05"))
		case 'u':
			w := int(t.Weekday())
			if w == 0 {
				w = 7
			}
			b.WriteString(strconv.Itoa(w))
		case 'w':
			b.WriteString(strconv.Itoa(int(t.Weekday())))
		case 'x':
			b.WriteString(t.Format("01/02/06"))
		case 'X':
			b.WriteString(t.Format("15:04:05"))
		case 'y':
			b.WriteString(fmt.Sprintf("%02d", t.Year()%100))
		case 'Y':
			b.WriteString(strconv.Itoa(t.Year()))
		case 'z':
			b.WriteString(t.Format("-0700"))
		case 'Z':
			b.WriteString(t.Format("MST"))
		case '%':
			b.WriteByte('%')
		default:
			// Pass through unknown sequences verbatim.
			b.WriteByte('%')
			b.WriteByte(fmtStr[i])
		}
	}
	return b.String()
}
