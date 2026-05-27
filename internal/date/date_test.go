package date

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func runDate(t *testing.T, args ...string) (int, string, string) {
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

func TestDefaultFormat(t *testing.T) {
	exit, out, _ := runDate(t)
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if len(out) < 20 {
		t.Errorf("default output too short: %q", out)
	}
}

func TestPlusFormatY(t *testing.T) {
	want := time.Now().Format("2006")
	_, out, _ := runDate(t, "+%Y")
	if strings.TrimSpace(out) != want {
		t.Errorf("got %q want %q", out, want)
	}
}

func TestFFormat(t *testing.T) {
	want := time.Now().Format("2006-01-02")
	_, out, _ := runDate(t, "+%F")
	if strings.TrimSpace(out) != want {
		t.Errorf("got %q want %q", out, want)
	}
}

func TestEpochS(t *testing.T) {
	exit, out, _ := runDate(t, "-d", "@1700000000", "+%Y-%m-%d")
	if exit != 0 {
		t.Fatalf("%s", out)
	}
	if strings.TrimSpace(out) != "2023-11-14" && strings.TrimSpace(out) != "2023-11-15" {
		// allow TZ slop around the date boundary
		t.Errorf("got %q want a 2023-11-14/15 date", out)
	}
}

func TestUTC(t *testing.T) {
	exit, out, _ := runDate(t, "-u", "-d", "@1700000000", "+%H:%M:%S")
	if exit != 0 {
		t.Fatalf("%s", out)
	}
	// 1700000000 = 2023-11-14 22:13:20 UTC
	if !strings.HasPrefix(strings.TrimSpace(out), "22:13:20") {
		t.Errorf("got %q want 22:13:20...", out)
	}
}

func TestISODate(t *testing.T) {
	want := time.Now().Format("2006-01-02")
	_, out, _ := runDate(t, "-I")
	if strings.TrimSpace(out) != want {
		t.Errorf("got %q want %q", out, want)
	}
}

func TestRFC(t *testing.T) {
	_, out, _ := runDate(t, "-R", "-d", "@1700000000")
	if !strings.Contains(out, "2023") || !strings.Contains(out, ":") {
		t.Errorf("RFC output unrecognized: %q", out)
	}
}

func TestReferenceFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x")
	os.WriteFile(p, nil, 0o644)
	want := time.Date(2020, 6, 15, 12, 0, 0, 0, time.UTC)
	os.Chtimes(p, want, want)
	_, out, _ := runDate(t, "-r", p, "-u", "+%Y-%m-%d")
	if strings.TrimSpace(out) != "2020-06-15" {
		t.Errorf("got %q want 2020-06-15", out)
	}
}

func TestPercentS(t *testing.T) {
	_, out, _ := runDate(t, "-d", "@1234567890", "+%s")
	if strings.TrimSpace(out) != "1234567890" {
		t.Errorf("got %q want 1234567890", out)
	}
}

func TestUnrecognizedDate(t *testing.T) {
	exit, _, er := runDate(t, "-d", "yesterday")
	if exit == 0 {
		t.Errorf("expected error on unrecognized date")
	}
	if !strings.Contains(er, "unrecognized") {
		t.Errorf("expected diagnostic, got %s", er)
	}
}

func TestParseISO(t *testing.T) {
	_, out, _ := runDate(t, "-d", "2020-01-02T03:04:05Z", "-u", "+%H:%M:%S")
	if strings.TrimSpace(out) != "03:04:05" {
		t.Errorf("got %q", out)
	}
}

func TestPercentSignLiteral(t *testing.T) {
	_, out, _ := runDate(t, "+%%")
	if strings.TrimSpace(out) != "%" {
		t.Errorf("got %q want %%", out)
	}
}
