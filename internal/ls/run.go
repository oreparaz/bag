package ls

import (
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type options struct {
	long      bool
	all       bool
	almostAll bool
	one       bool
	human     bool
	sortSize  bool
	sortMtime bool
	reverse   bool
	recursive bool
	directory bool
	classify  bool
	inode     bool
}

func parseArgs(argv []string) (*options, []string, error) {
	o := &options{}
	var paths []string
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if a == "--" {
			paths = append(paths, argv[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "--") {
			name := a[2:]
			if eq := strings.IndexByte(name, '='); eq >= 0 {
				name = name[:eq] // we ignore the value for --color=... etc.
			}
			switch name {
			case "all":
				o.all = true
			case "almost-all":
				o.almostAll = true
			case "human-readable":
				o.human = true
			case "reverse":
				o.reverse = true
			case "recursive":
				o.recursive = true
			case "directory":
				o.directory = true
			case "classify":
				o.classify = true
			case "inode":
				o.inode = true
			case "color":
				// Swallow --color / --color=never|always|auto; we never
				// emit ANSI sequences.
			default:
				// Unknown long flags are silently ignored for compat.
			}
			continue
		}
		if strings.HasPrefix(a, "-") && a != "-" && len(a) > 1 {
			for j := 1; j < len(a); j++ {
				switch a[j] {
				case 'l':
					o.long = true
				case 'a':
					o.all = true
				case 'A':
					o.almostAll = true
				case '1':
					o.one = true
				case 'h':
					o.human = true
				case 'S':
					o.sortSize = true
				case 't':
					o.sortMtime = true
				case 'r':
					o.reverse = true
				case 'R':
					o.recursive = true
				case 'd':
					o.directory = true
				case 'F':
					o.classify = true
				case 'i':
					o.inode = true
				default:
					// Silently ignore unknown short flags.
				}
			}
			continue
		}
		paths = append(paths, a)
	}
	if len(paths) == 0 {
		paths = []string{"."}
	}
	return o, paths, nil
}

func run(argv []string) int {
	o, paths, err := parseArgs(argv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ls: %v\n", err)
		return 2
	}

	// Split into files (printed first, no header) and directories
	// (printed second, with a "name:" header when there's more than
	// one target). Matches GNU ls.
	var files, dirs []string
	exit := 0
	for _, p := range paths {
		info, err := os.Lstat(p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ls: cannot access '%s': %v\n", p, err)
			exit = 2
			continue
		}
		if info.IsDir() && !o.directory {
			dirs = append(dirs, p)
		} else {
			files = append(files, p)
		}
	}

	if len(files) > 0 {
		entries := make([]entry, 0, len(files))
		for _, p := range files {
			e, err := newEntry(p, p)
			if err != nil {
				fmt.Fprintf(os.Stderr, "ls: cannot access '%s': %v\n", p, err)
				exit = 2
				continue
			}
			entries = append(entries, e)
		}
		sortEntries(entries, o)
		printEntries(os.Stdout, entries, o)
	}

	for i, d := range dirs {
		if len(files) > 0 || len(dirs) > 1 || o.recursive {
			if len(files) > 0 || i > 0 {
				fmt.Fprintln(os.Stdout)
			}
			fmt.Fprintf(os.Stdout, "%s:\n", d)
		}
		if err := listDir(os.Stdout, d, o, true); err != nil {
			fmt.Fprintf(os.Stderr, "ls: %v\n", err)
			exit = 2
		}
	}
	return exit
}

// listDir lists one directory. When recursive is true and the option is
// set, each subdirectory is then listed with its own header.
func listDir(w *os.File, dir string, o *options, recurseAllowed bool) error {
	names, err := readDirNames(dir)
	if err != nil {
		return err
	}
	entries := make([]entry, 0, len(names))
	for _, name := range names {
		if !o.all && !o.almostAll && strings.HasPrefix(name, ".") {
			continue
		}
		full := filepath.Join(dir, name)
		e, err := newEntry(full, name)
		if err != nil {
			// Stale symlinks: keep going; emit a lite entry with the
			// raw name so the listing isn't truncated.
			e = entry{name: name, path: full, err: err}
		}
		entries = append(entries, e)
	}
	// -a includes "." and ".." in this position (gnu behavior).
	if o.all && !o.almostAll {
		dotEntries := []string{".", ".."}
		extra := make([]entry, 0, 2)
		for _, n := range dotEntries {
			full := filepath.Join(dir, n)
			e, err := newEntry(full, n)
			if err == nil {
				extra = append(extra, e)
			}
		}
		entries = append(extra, entries...)
	}
	sortEntries(entries, o)

	// In long mode, GNU prints a "total <blocks>" line before the body.
	if o.long {
		var total int64
		for _, e := range entries {
			total += entryBlocks(e)
		}
		fmt.Fprintf(w, "total %d\n", total)
	}
	printEntries(w, entries, o)

	if o.recursive && recurseAllowed {
		for _, e := range entries {
			if e.name == "." || e.name == ".." {
				continue
			}
			if !e.info.IsDir() || isSymlink(e.info) {
				continue
			}
			fmt.Fprintln(w)
			fmt.Fprintf(w, "%s:\n", e.path)
			if err := listDir(w, e.path, o, true); err != nil {
				fmt.Fprintf(os.Stderr, "ls: %v\n", err)
			}
		}
	}
	return nil
}

// readDirNames returns the directory entries in byte-sorted order. We
// don't use os.ReadDir's pre-sort because it locks us to one ordering
// (we re-sort per options below anyway).
func readDirNames(dir string) ([]string, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	names, err := f.Readdirnames(-1)
	if err != nil {
		return nil, err
	}
	return names, nil
}

type entry struct {
	name string
	path string
	info fs.FileInfo
	err  error // when stat failed; entry is still listed by name
}

func newEntry(path, name string) (entry, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return entry{}, err
	}
	return entry{name: name, path: path, info: info}, nil
}

func sortEntries(es []entry, o *options) {
	sort.SliceStable(es, func(i, j int) bool {
		less := lessByOptions(es[i], es[j], o)
		if o.reverse {
			return !less
		}
		return less
	})
}

func lessByOptions(a, b entry, o *options) bool {
	switch {
	case o.sortMtime:
		ai, bi := a.info, b.info
		if ai == nil || bi == nil {
			return a.name < b.name
		}
		if ai.ModTime().Equal(bi.ModTime()) {
			return a.name < b.name
		}
		return ai.ModTime().After(bi.ModTime())
	case o.sortSize:
		ai, bi := a.info, b.info
		if ai == nil || bi == nil {
			return a.name < b.name
		}
		if ai.Size() == bi.Size() {
			return a.name < b.name
		}
		return ai.Size() > bi.Size()
	}
	return a.name < b.name
}

// printEntries writes one entry per line. In long mode we include
// permissions, link count, owner, group, size, mtime, name (and target
// for symlinks).
func printEntries(w *os.File, entries []entry, o *options) {
	if !o.long {
		for _, e := range entries {
			fmt.Fprintln(w, formatShort(e, o))
		}
		return
	}
	// Long mode: compute column widths so things line up.
	widths := longWidths(entries, o)
	for _, e := range entries {
		fmt.Fprintln(w, formatLong(e, o, widths))
	}
}

func formatShort(e entry, o *options) string {
	var b strings.Builder
	if o.inode {
		fmt.Fprintf(&b, "%d ", inodeOf(e.info))
	}
	b.WriteString(e.name)
	if o.classify {
		b.WriteString(classifyChar(e.info))
	}
	return b.String()
}

type lwidths struct {
	links, owner, group, size int
}

func longWidths(entries []entry, o *options) lwidths {
	var w lwidths
	for _, e := range entries {
		if e.info == nil {
			continue
		}
		st := sysStat(e.info)
		w.links = max(w.links, len(strconv.FormatUint(uint64(st.Nlink), 10)))
		w.owner = max(w.owner, len(uidName(st.Uid)))
		w.group = max(w.group, len(gidName(st.Gid)))
		w.size = max(w.size, len(sizeString(e.info, o.human)))
	}
	return w
}

func formatLong(e entry, o *options, w lwidths) string {
	var b strings.Builder
	if o.inode {
		fmt.Fprintf(&b, "%d ", inodeOf(e.info))
	}
	if e.info == nil {
		fmt.Fprintf(&b, "?????????? ? ? ? ? ? %s", e.name)
		return b.String()
	}
	st := sysStat(e.info)
	b.WriteString(modeString(e.info))
	fmt.Fprintf(&b, " %*d", w.links, st.Nlink)
	fmt.Fprintf(&b, " %-*s", w.owner, uidName(st.Uid))
	fmt.Fprintf(&b, " %-*s", w.group, gidName(st.Gid))
	fmt.Fprintf(&b, " %*s", w.size, sizeString(e.info, o.human))
	b.WriteByte(' ')
	b.WriteString(formatTime(e.info.ModTime()))
	b.WriteByte(' ')
	b.WriteString(e.name)
	if o.classify {
		b.WriteString(classifyChar(e.info))
	}
	if isSymlink(e.info) {
		if target, err := os.Readlink(e.path); err == nil {
			b.WriteString(" -> ")
			b.WriteString(target)
		}
	}
	return b.String()
}

// modeString renders a 10-char permission string, matching ls's
// drwxr-xr-x form.
func modeString(info fs.FileInfo) string {
	m := info.Mode()
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
	// setuid / setgid / sticky overlay into the x slots.
	if m&fs.ModeSetuid != 0 {
		if b[3] == 'x' {
			b[3] = 's'
		} else {
			b[3] = 'S'
		}
	}
	if m&fs.ModeSetgid != 0 {
		if b[6] == 'x' {
			b[6] = 's'
		} else {
			b[6] = 'S'
		}
	}
	if m&fs.ModeSticky != 0 {
		if b[9] == 'x' {
			b[9] = 't'
		} else {
			b[9] = 'T'
		}
	}
	return string(b[:])
}

// formatTime mimics GNU: "Jan 02 15:04" for files less than 6 months
// old; "Jan 02  2006" otherwise. We use the local timezone.
func formatTime(t time.Time) string {
	now := time.Now()
	sixMonths := 6 * 30 * 24 * time.Hour
	if now.Sub(t) < sixMonths && t.Before(now.Add(time.Hour)) {
		return t.Format("Jan _2 15:04")
	}
	return t.Format("Jan _2  2006")
}

// sizeString returns the file's size in bytes, or a human-readable
// suffix when -h was passed.
func sizeString(info fs.FileInfo, human bool) string {
	n := info.Size()
	if !human {
		return strconv.FormatInt(n, 10)
	}
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	val := float64(n) / float64(div)
	suf := "KMGTPE"[exp]
	if val >= 10 {
		return fmt.Sprintf("%d%c", int(val+0.5), suf)
	}
	return fmt.Sprintf("%.1f%c", val, suf)
}

// classifyChar returns the -F suffix character (or "") for an entry.
func classifyChar(info fs.FileInfo) string {
	if info == nil {
		return ""
	}
	m := info.Mode()
	switch {
	case m.IsDir():
		return "/"
	case m&fs.ModeSymlink != 0:
		return "@"
	case m&fs.ModeNamedPipe != 0:
		return "|"
	case m&fs.ModeSocket != 0:
		return "="
	case m&0o111 != 0 && m.IsRegular():
		return "*"
	}
	return ""
}

func isSymlink(info fs.FileInfo) bool {
	return info != nil && info.Mode()&fs.ModeSymlink != 0
}

// sysStat returns the platform-specific stat struct. Linux/Darwin both
// expose Uid/Gid/Nlink on the same field names so a single helper works.
func sysStat(info fs.FileInfo) *syscall.Stat_t {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return st
	}
	return &syscall.Stat_t{}
}

func inodeOf(info fs.FileInfo) uint64 {
	if info == nil {
		return 0
	}
	return sysStat(info).Ino
}

// entryBlocks returns the size in 1024-byte units used by an entry, as
// reported by stat (Blocks is in 512-byte sectors).
func entryBlocks(e entry) int64 {
	if e.info == nil {
		return 0
	}
	return int64(sysStat(e.info).Blocks) / 2
}

// uidName / gidName look up the textual name for a uid/gid, falling
// back to the numeric form when the lookup fails (NSS unavailable,
// container with stripped passwd, etc.).
func uidName(uid uint32) string {
	if u, err := user.LookupId(strconv.FormatUint(uint64(uid), 10)); err == nil {
		return u.Username
	}
	return strconv.FormatUint(uint64(uid), 10)
}

func gidName(gid uint32) string {
	if g, err := user.LookupGroupId(strconv.FormatUint(uint64(gid), 10)); err == nil {
		return g.Name
	}
	return strconv.FormatUint(uint64(gid), 10)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
