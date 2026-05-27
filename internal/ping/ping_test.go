package ping

import (
	"io"
	"os"
	"strings"
	"testing"
)

func runPing(t *testing.T, args ...string) (int, string, string) {
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

func TestParseArgs(t *testing.T) {
	o, err := parseArgs([]string{"-c", "3", "-W", "2", "-i", "0.5", "-s", "32", "example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if o.count != 3 || o.size != 32 || o.host != "example.com" {
		t.Errorf("parse wrong: %+v", o)
	}
}

func TestMissingHost(t *testing.T) {
	exit, _, er := runPing(t, "-c", "1")
	if exit == 0 {
		t.Errorf("expected error on missing host")
	}
	if !strings.Contains(er, "host required") {
		t.Errorf("expected diagnostic, got %s", er)
	}
}

func TestInvalidCount(t *testing.T) {
	_, err := parseArgs([]string{"-c", "abc", "x"})
	if err == nil {
		t.Errorf("expected error")
	}
}

// TestLocalPing tries to ping 127.0.0.1 once. On CI without ICMP
// permissions, opening the socket will fail and we exit 2. We accept
// either successful ping (recv==1, exit=0) or the privileges-denied
// path (exit=2 with a diagnostic) — the goal is just that the tool
// doesn't crash on a typical sandbox.
func TestLocalPing(t *testing.T) {
	exit, out, er := runPing(t, "-c", "1", "-W", "2", "127.0.0.1")
	switch exit {
	case 0:
		if !strings.Contains(out, "bytes from") {
			t.Errorf("expected at least one reply line: %q", out)
		}
		if !strings.Contains(out, "packets transmitted") {
			t.Errorf("expected summary, got %q", out)
		}
	case 1, 2:
		// no reply or privilege error — acceptable on locked-down CI
		if !strings.Contains(er, "ping") && !strings.Contains(out, "loss") {
			t.Errorf("unexpected failure shape: stderr=%q stdout=%q", er, out)
		}
	default:
		t.Errorf("unexpected exit %d (stderr=%s)", exit, er)
	}
}
