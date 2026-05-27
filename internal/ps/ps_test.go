package ps

import (
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func runPS(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout = wOut
	os.Stderr = wErr
	defer func() {
		os.Stdout, os.Stderr = oldOut, oldErr
		rOut.Close()
		rErr.Close()
	}()
	exit := Main(args)
	wOut.Close()
	wErr.Close()
	out, _ := io.ReadAll(rOut)
	er, _ := io.ReadAll(rErr)
	return exit, string(out), string(er)
}

func skipNonLinux(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("ps requires /proc; Linux only")
	}
}

func TestDefaultListing(t *testing.T) {
	skipNonLinux(t)
	exit, out, er := runPS(t)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%s", exit, er)
	}
	if !strings.Contains(out, "PID") || !strings.Contains(out, "CMD") {
		t.Errorf("expected header, got %q", out)
	}
}

func TestFilterPID(t *testing.T) {
	skipNonLinux(t)
	mypid := os.Getpid()
	_, out, _ := runPS(t, "-p", strconv.Itoa(mypid))
	if !strings.Contains(out, strconv.Itoa(mypid)) {
		t.Errorf("expected our PID %d in output: %q", mypid, out)
	}
}

func TestAUXFormat(t *testing.T) {
	skipNonLinux(t)
	_, out, _ := runPS(t, "aux")
	if !strings.Contains(out, "USER") || !strings.Contains(out, "RSS") {
		t.Errorf("aux format header missing: %q", out)
	}
}

func TestEFFormat(t *testing.T) {
	skipNonLinux(t)
	_, out, _ := runPS(t, "-ef")
	if !strings.Contains(out, "UID") || !strings.Contains(out, "PPID") {
		t.Errorf("-ef format header missing: %q", out)
	}
}

func TestParseShortFlags(t *testing.T) {
	o, err := parseArgs([]string{"aux"})
	if err != nil {
		t.Fatal(err)
	}
	if !o.bsdAll || !o.bsdUser || !o.bsdX {
		t.Errorf("aux flags not all set: %+v", o)
	}
}

func TestParseDashFlags(t *testing.T) {
	o, err := parseArgs([]string{"-ef"})
	if err != nil {
		t.Fatal(err)
	}
	if !o.sysvE || !o.sysvF {
		t.Errorf("-ef flags not set: %+v", o)
	}
}

func TestPidsFilter(t *testing.T) {
	o, err := parseArgs([]string{"-p", "1,2,3"})
	if err != nil {
		t.Fatal(err)
	}
	if len(o.pids) != 3 {
		t.Errorf("got %v", o.pids)
	}
}

func TestUserFilter(t *testing.T) {
	o, err := parseArgs([]string{"-u", "root"})
	if err != nil {
		t.Fatal(err)
	}
	if o.userFilt != "root" {
		t.Errorf("got %q", o.userFilt)
	}
}
