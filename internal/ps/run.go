package ps

import (
	"fmt"
	"os"
	"os/user"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

type options struct {
	bsdAll   bool // a
	bsdUser  bool // u
	bsdX     bool // x
	sysvE    bool // -e
	sysvF    bool // -f
	pids     []int
	userFilt string
	ppidFilt int
}

func parseArgs(argv []string) (*options, error) {
	o := &options{ppidFilt: -1}
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if a == "--" {
			break
		}
		// BSD-style cluster (no leading dash): a, u, x, ax, aux.
		if !strings.HasPrefix(a, "-") {
			for j := 0; j < len(a); j++ {
				switch a[j] {
				case 'a':
					o.bsdAll = true
				case 'u':
					o.bsdUser = true
				case 'x':
					o.bsdX = true
				}
			}
			continue
		}
		// Long flags.
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
			case "ppid":
				if !hasEq {
					if i+1 >= len(argv) {
						return nil, fmt.Errorf("--ppid requires an argument")
					}
					i++
					val = argv[i]
				}
				n, err := strconv.Atoi(val)
				if err != nil {
					return nil, fmt.Errorf("invalid --ppid: %q", val)
				}
				o.ppidFilt = n
			}
			continue
		}
		// Short flags (with dash): -e, -f, -p PIDS, -u USER.
		for j := 1; j < len(a); j++ {
			switch a[j] {
			case 'e':
				o.sysvE = true
			case 'f':
				o.sysvF = true
			case 'p':
				var val string
				if j+1 < len(a) {
					val = a[j+1:]
					j = len(a)
				} else {
					if i+1 >= len(argv) {
						return nil, fmt.Errorf("-p requires PIDS")
					}
					i++
					val = argv[i]
				}
				for _, s := range strings.Split(val, ",") {
					n, err := strconv.Atoi(strings.TrimSpace(s))
					if err == nil {
						o.pids = append(o.pids, n)
					}
				}
			case 'u':
				var val string
				if j+1 < len(a) {
					val = a[j+1:]
					j = len(a)
				} else {
					if i+1 >= len(argv) {
						return nil, fmt.Errorf("-u requires USER")
					}
					i++
					val = argv[i]
				}
				o.userFilt = val
			}
		}
	}
	return o, nil
}

func run(argv []string) int {
	if runtime.GOOS != "linux" {
		fmt.Fprintln(os.Stderr, "ps: bag's ps is currently supported on Linux only (reads /proc)")
		return 1
	}
	o, err := parseArgs(argv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ps: %v\n", err)
		return 1
	}
	procs, err := readAllProcs()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ps: %v\n", err)
		return 1
	}
	filtered := filter(procs, o)
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].pid < filtered[j].pid })
	emit(filtered, o)
	return 0
}

type proc struct {
	pid     int
	ppid    int
	uid     int
	user    string
	state   string
	cmdline string
	comm    string
	rssKB   uint64
	vmKB    uint64
}

func filter(in []proc, o *options) []proc {
	var out []proc
	for _, p := range in {
		if len(o.pids) > 0 && !containsInt(o.pids, p.pid) {
			continue
		}
		if o.userFilt != "" && !userMatches(o.userFilt, p) {
			continue
		}
		if o.ppidFilt >= 0 && p.ppid != o.ppidFilt {
			continue
		}
		out = append(out, p)
	}
	return out
}

func containsInt(xs []int, v int) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func userMatches(want string, p proc) bool {
	if n, err := strconv.Atoi(want); err == nil {
		return n == p.uid
	}
	return p.user == want
}

func emit(procs []proc, o *options) {
	switch {
	case o.bsdUser:
		// "aux" — USER PID %CPU %MEM VSZ RSS TT STAT START TIME COMMAND
		fmt.Fprintf(os.Stdout, "%-10s %5s %5s %5s %7s %7s %s\n",
			"USER", "PID", "%CPU", "%MEM", "VSZ", "RSS", "COMMAND")
		for _, p := range procs {
			fmt.Fprintf(os.Stdout, "%-10s %5d %5s %5s %7d %7d %s\n",
				truncate(p.user, 10), p.pid, "0.0", "0.0",
				p.vmKB, p.rssKB, p.cmdline)
		}
	case o.sysvF || o.sysvE:
		// -ef — UID PID PPID C STIME TTY TIME CMD
		fmt.Fprintf(os.Stdout, "%-10s %5s %5s %s\n", "UID", "PID", "PPID", "CMD")
		for _, p := range procs {
			fmt.Fprintf(os.Stdout, "%-10s %5d %5d %s\n",
				truncate(p.user, 10), p.pid, p.ppid, p.cmdline)
		}
	default:
		fmt.Fprintf(os.Stdout, "%5s %s\n", "PID", "CMD")
		for _, p := range procs {
			fmt.Fprintf(os.Stdout, "%5d %s\n", p.pid, p.comm)
		}
	}
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func readAllProcs() ([]proc, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	var out []proc
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		p, err := readOneProc(pid)
		if err != nil {
			// Process may have exited between readdir and readOneProc.
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

func readOneProc(pid int) (proc, error) {
	p := proc{pid: pid, user: "?", state: "?"}
	root := fmt.Sprintf("/proc/%d", pid)

	if status, err := os.ReadFile(root + "/status"); err == nil {
		for _, line := range strings.Split(string(status), "\n") {
			if i := strings.Index(line, ":"); i > 0 {
				key := line[:i]
				val := strings.TrimSpace(line[i+1:])
				switch key {
				case "PPid":
					p.ppid, _ = strconv.Atoi(val)
				case "Uid":
					parts := strings.Fields(val)
					if len(parts) > 0 {
						p.uid, _ = strconv.Atoi(parts[0])
					}
				case "State":
					p.state = val
				case "VmRSS":
					p.rssKB = parseKB(val)
				case "VmSize":
					p.vmKB = parseKB(val)
				}
			}
		}
	}
	if u, err := user.LookupId(strconv.Itoa(p.uid)); err == nil {
		p.user = u.Username
	} else {
		p.user = strconv.Itoa(p.uid)
	}

	if cmd, err := os.ReadFile(root + "/cmdline"); err == nil && len(cmd) > 0 {
		// cmdline uses NUL separators.
		clean := strings.TrimRight(strings.ReplaceAll(string(cmd), "\x00", " "), " ")
		p.cmdline = clean
	}
	if comm, err := os.ReadFile(root + "/comm"); err == nil {
		p.comm = strings.TrimRight(string(comm), "\n")
	}
	if p.cmdline == "" {
		p.cmdline = "[" + p.comm + "]"
	}
	return p, nil
}

func parseKB(s string) uint64 {
	parts := strings.Fields(s)
	if len(parts) == 0 {
		return 0
	}
	v, _ := strconv.ParseUint(parts[0], 10, 64)
	return v
}
