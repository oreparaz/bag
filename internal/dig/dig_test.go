package dig

import (
	"io"
	"net"
	"os"
	"strings"
	"testing"
)

func runDig(t *testing.T, args ...string) (int, string, string) {
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

// netAvailable is a soft check — most CI runners have outbound DNS,
// but if we're somehow offline we skip the live-lookup tests.
func netAvailable(t *testing.T) bool {
	t.Helper()
	_, err := net.LookupHost("localhost")
	return err == nil
}

func TestParseArgs(t *testing.T) {
	o, err := parseArgs([]string{"@1.1.1.1", "+short", "example.com", "AAAA"})
	if err != nil {
		t.Fatal(err)
	}
	if o.server != "1.1.1.1" || o.queryT != "AAAA" || o.name != "example.com" || !o.short {
		t.Errorf("parse wrong: %+v", o)
	}
}

func TestParseReverse(t *testing.T) {
	o, err := parseArgs([]string{"-x", "1.1.1.1"})
	if err != nil {
		t.Fatal(err)
	}
	if o.queryT != "PTR" || o.name != "1.1.1.1" {
		t.Errorf("got %+v", o)
	}
}

func TestParseMissingName(t *testing.T) {
	_, err := parseArgs([]string{"+short"})
	if err == nil {
		t.Errorf("expected error on missing name")
	}
}

func TestLookupLocalhost(t *testing.T) {
	if !netAvailable(t) {
		t.Skip("no DNS resolution")
	}
	exit, out, er := runDig(t, "+short", "localhost", "A")
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%s", exit, er)
	}
	if !strings.Contains(out, "127.0.0.1") && !strings.Contains(out, "::1") {
		t.Errorf("expected loopback IP, got %q", out)
	}
}

func TestDefaultTypeIsA(t *testing.T) {
	o, _ := parseArgs([]string{"example.com"})
	if o.queryT != "A" {
		t.Errorf("default type = %q want A", o.queryT)
	}
}

func TestUnsupportedType(t *testing.T) {
	exit, _, er := runDig(t, "example.com", "ZONE")
	if exit == 0 {
		t.Errorf("expected error for unsupported type")
	}
	if !strings.Contains(er, "unsupported") {
		t.Errorf("expected diagnostic, got %s", er)
	}
}

func TestShortFormatIsPlain(t *testing.T) {
	if !netAvailable(t) {
		t.Skip("no DNS resolution")
	}
	_, out, _ := runDig(t, "+short", "localhost")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for _, l := range lines {
		if strings.Contains(l, ";") || strings.Contains(l, "QUESTION") {
			t.Errorf("+short leaked verbose line: %q", l)
		}
	}
}
