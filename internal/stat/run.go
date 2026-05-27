package stat

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type options struct {
	format      string
	dereference bool
	terse       bool
	fsMode      bool
	files       []string
}

func parseArgs(argv []string) (*options, error) {
	o := &options{}
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if a == "--" {
			o.files = append(o.files, argv[i+1:]...)
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
			case "format":
				if !hasEq {
					if i+1 >= len(argv) {
						return nil, errors.New("--format requires an argument")
					}
					i++
					val = argv[i]
				}
				o.format = val
			case "dereference":
				o.dereference = true
			case "terse":
				o.terse = true
			case "file-system":
				o.fsMode = true
			default:
				// Ignore unknown long flags.
			}
			continue
		}
		if strings.HasPrefix(a, "-") && a != "-" && len(a) > 1 {
			for j := 1; j < len(a); j++ {
				switch a[j] {
				case 'L':
					o.dereference = true
				case 't':
					o.terse = true
				case 'f':
					o.fsMode = true
				case 'c':
					var val string
					if j+1 < len(a) {
						val = a[j+1:]
						j = len(a)
					} else {
						if i+1 >= len(argv) {
							return nil, errors.New("-c requires an argument")
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
		o.files = append(o.files, a)
	}
	if len(o.files) == 0 {
		return nil, errors.New("missing operand")
	}
	return o, nil
}

func run(argv []string) int {
	o, err := parseArgs(argv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "stat: %v\n", err)
		return 1
	}
	exit := 0
	for _, p := range o.files {
		if err := statOne(p, o); err != nil {
			fmt.Fprintf(os.Stderr, "stat: cannot stat '%s': %v\n", p, err)
			exit = 1
		}
	}
	return exit
}

func statOne(p string, o *options) error {
	var info fs.FileInfo
	var err error
	if o.dereference {
		info, err = os.Stat(p)
	} else {
		info, err = os.Lstat(p)
	}
	if err != nil {
		return err
	}
	st, _ := info.Sys().(*syscall.Stat_t)
	if st == nil {
		// Unsupported platform; emit a minimal report.
		fmt.Fprintf(os.Stdout, "  File: %s\n  Size: %d\n", p, info.Size())
		return nil
	}
	if o.format != "" {
		fmt.Fprintln(os.Stdout, expandFormat(o.format, p, info, st))
		return nil
	}
	if o.terse {
		fmt.Fprintln(os.Stdout, terseLine(p, info, st))
		return nil
	}
	fmt.Fprint(os.Stdout, defaultBlock(p, info, st))
	return nil
}

func expandFormat(fmtStr, path string, info fs.FileInfo, st *syscall.Stat_t) string {
	var b strings.Builder
	for i := 0; i < len(fmtStr); i++ {
		c := fmtStr[i]
		if c != '%' || i+1 >= len(fmtStr) {
			// Escape sequences: \n, \t...
			if c == '\\' && i+1 < len(fmtStr) {
				switch fmtStr[i+1] {
				case 'n':
					b.WriteByte('\n')
				case 't':
					b.WriteByte('\t')
				case '\\':
					b.WriteByte('\\')
				default:
					b.WriteByte(c)
					b.WriteByte(fmtStr[i+1])
				}
				i++
				continue
			}
			b.WriteByte(c)
			continue
		}
		i++
		switch fmtStr[i] {
		case 'a':
			fmt.Fprintf(&b, "%o", info.Mode().Perm())
		case 'A':
			b.WriteString(modeLetters(info.Mode()))
		case 'b':
			fmt.Fprintf(&b, "%d", st.Blocks)
		case 'B':
			fmt.Fprintf(&b, "%d", 512) // OpenBSD/Linux uses 512-byte units
		case 'd':
			fmt.Fprintf(&b, "%d", st.Dev)
		case 'D':
			fmt.Fprintf(&b, "%x", st.Dev)
		case 'f':
			fmt.Fprintf(&b, "%x", uint32(info.Mode())&0xFFFF)
		case 'F':
			b.WriteString(fileTypeStr(info))
		case 'g':
			fmt.Fprintf(&b, "%d", st.Gid)
		case 'G':
			b.WriteString(gidName(st.Gid))
		case 'h':
			fmt.Fprintf(&b, "%d", st.Nlink)
		case 'i':
			fmt.Fprintf(&b, "%d", st.Ino)
		case 'n':
			b.WriteString(path)
		case 'N':
			if info.Mode()&fs.ModeSymlink != 0 {
				target, _ := os.Readlink(path)
				fmt.Fprintf(&b, "'%s' -> '%s'", path, target)
			} else {
				fmt.Fprintf(&b, "'%s'", path)
			}
		case 'o':
			fmt.Fprintf(&b, "%d", st.Blksize)
		case 's':
			fmt.Fprintf(&b, "%d", info.Size())
		case 't':
			fmt.Fprintf(&b, "%x", uint32(st.Rdev>>8))
		case 'T':
			fmt.Fprintf(&b, "%x", uint32(st.Rdev&0xff))
		case 'u':
			fmt.Fprintf(&b, "%d", st.Uid)
		case 'U':
			b.WriteString(uidName(st.Uid))
		case 'x':
			b.WriteString(formatStatTime(accessTime(st)))
		case 'X':
			b.WriteString(strconv.FormatInt(accessTime(st).Unix(), 10))
		case 'y':
			b.WriteString(formatStatTime(info.ModTime()))
		case 'Y':
			b.WriteString(strconv.FormatInt(info.ModTime().Unix(), 10))
		case 'z':
			b.WriteString(formatStatTime(changeTime(st)))
		case 'Z':
			b.WriteString(strconv.FormatInt(changeTime(st).Unix(), 10))
		case '%':
			b.WriteByte('%')
		default:
			b.WriteByte('%')
			b.WriteByte(fmtStr[i])
		}
	}
	return b.String()
}

func defaultBlock(path string, info fs.FileInfo, st *syscall.Stat_t) string {
	var b strings.Builder
	target := ""
	if info.Mode()&fs.ModeSymlink != 0 {
		t, _ := os.Readlink(path)
		target = " -> " + t
	}
	fmt.Fprintf(&b, "  File: %s%s\n", path, target)
	fmt.Fprintf(&b, "  Size: %d         \tBlocks: %d        IO Block: %d   %s\n",
		info.Size(), st.Blocks, st.Blksize, fileTypeStr(info))
	fmt.Fprintf(&b, "Device: %xh/%dd\tInode: %d   Links: %d\n",
		st.Dev, st.Dev, st.Ino, st.Nlink)
	fmt.Fprintf(&b, "Access: (%04o/%s)  Uid: (%5d/%-8s)   Gid: (%5d/%-8s)\n",
		uint32(info.Mode().Perm()), modeLetters(info.Mode()),
		st.Uid, uidName(st.Uid), st.Gid, gidName(st.Gid))
	fmt.Fprintf(&b, "Access: %s\n", formatStatTime(accessTime(st)))
	fmt.Fprintf(&b, "Modify: %s\n", formatStatTime(info.ModTime()))
	fmt.Fprintf(&b, "Change: %s\n", formatStatTime(changeTime(st)))
	fmt.Fprintf(&b, " Birth: -\n")
	return b.String()
}

func terseLine(path string, info fs.FileInfo, st *syscall.Stat_t) string {
	return fmt.Sprintf("%s %d %d %x %d %d %x %d %d %x %x %d %d %d %d %s",
		path, info.Size(), st.Blocks, uint32(info.Mode())&0xFFFF,
		st.Uid, st.Gid, st.Dev, st.Ino, st.Nlink,
		uint32(st.Rdev>>8), uint32(st.Rdev&0xff),
		accessTime(st).Unix(), info.ModTime().Unix(),
		changeTime(st).Unix(), st.Blksize,
		"-",
	)
}

func modeLetters(m fs.FileMode) string {
	var b [10]byte
	switch {
	case m&fs.ModeDir != 0:
		b[0] = 'd'
	case m&fs.ModeSymlink != 0:
		b[0] = 'l'
	case m&fs.ModeNamedPipe != 0:
		b[0] = 'p'
	case m&fs.ModeSocket != 0:
		b[0] = 's'
	case m&fs.ModeDevice != 0:
		if m&fs.ModeCharDevice != 0 {
			b[0] = 'c'
		} else {
			b[0] = 'b'
		}
	default:
		b[0] = '-'
	}
	perms := m.Perm()
	for i, bit := range []fs.FileMode{0o400, 0o200, 0o100, 0o040, 0o020, 0o010, 0o004, 0o002, 0o001} {
		if perms&bit != 0 {
			b[i+1] = "rwxrwxrwx"[i]
		} else {
			b[i+1] = '-'
		}
	}
	return string(b[:])
}

func fileTypeStr(info fs.FileInfo) string {
	m := info.Mode()
	switch {
	case m.IsDir():
		return "directory"
	case m&fs.ModeSymlink != 0:
		return "symbolic link"
	case m&fs.ModeNamedPipe != 0:
		return "fifo"
	case m&fs.ModeSocket != 0:
		return "socket"
	case m&fs.ModeDevice != 0:
		if m&fs.ModeCharDevice != 0 {
			return "character special file"
		}
		return "block special file"
	case m.IsRegular():
		if info.Size() == 0 {
			return "regular empty file"
		}
		return "regular file"
	}
	return "unknown"
}

func formatStatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02 15:04:05.000000000 -0700")
}

func uidName(uid uint32) string {
	if u, err := user.LookupId(strconv.FormatUint(uint64(uid), 10)); err == nil {
		return u.Username
	}
	return "UNKNOWN"
}

func gidName(gid uint32) string {
	if g, err := user.LookupGroupId(strconv.FormatUint(uint64(gid), 10)); err == nil {
		return g.Name
	}
	return "UNKNOWN"
}

// resolvePath is a tiny convenience kept for future format extensions
// that need a canonical path.
func resolvePath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

var _ = resolvePath
