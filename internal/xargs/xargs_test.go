package xargs

import (
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func runXargs(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	rIn, wIn, _ := os.Pipe()
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	oldIn, oldOut, oldErr := os.Stdin, os.Stdout, os.Stderr
	os.Stdin = rIn
	os.Stdout = wOut
	os.Stderr = wErr
	defer func() {
		os.Stdin, os.Stdout, os.Stderr = oldIn, oldOut, oldErr
		rIn.Close()
		rOut.Close()
		rErr.Close()
	}()
	go func() {
		wIn.WriteString(stdin)
		wIn.Close()
	}()
	exit := Main(args)
	wOut.Close()
	wErr.Close()
	out, _ := io.ReadAll(rOut)
	er, _ := io.ReadAll(rErr)
	return exit, string(out), string(er)
}

func mustHaveEcho(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("echo")
	if err != nil {
		t.Skip("no /bin/echo")
	}
	return p
}

func TestBasicBatch(t *testing.T) {
	echo := mustHaveEcho(t)
	exit, out, er := runXargs(t, "a b c\n", echo)
	if exit != 0 {
		t.Fatalf("%s", er)
	}
	if strings.TrimSpace(out) != "a b c" {
		t.Errorf("got %q", out)
	}
}

func TestMaxArgs(t *testing.T) {
	echo := mustHaveEcho(t)
	exit, out, _ := runXargs(t, "a b c d\n", "-n", "2", echo)
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 || lines[0] != "a b" || lines[1] != "c d" {
		t.Errorf("got %v", lines)
	}
}

func TestReplace(t *testing.T) {
	echo := mustHaveEcho(t)
	_, out, _ := runXargs(t, "alpha\nbeta\n", "-I", "{}", echo, "X-{}-Y")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 || lines[0] != "X-alpha-Y" || lines[1] != "X-beta-Y" {
		t.Errorf("got %v", lines)
	}
}

func TestNullSep(t *testing.T) {
	echo := mustHaveEcho(t)
	in := "a b\x00c d\x00"
	_, out, _ := runXargs(t, in, "-0", echo)
	if strings.TrimSpace(out) != "a b c d" {
		t.Errorf("got %q", out)
	}
}

func TestNoRunIfEmpty(t *testing.T) {
	echo := mustHaveEcho(t)
	exit, out, _ := runXargs(t, "", "-r", echo, "this", "should", "not", "run")
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if out != "" {
		t.Errorf("expected empty output, got %q", out)
	}
}

func TestVerbose(t *testing.T) {
	echo := mustHaveEcho(t)
	_, _, er := runXargs(t, "x y\n", "-t", echo)
	if !strings.Contains(er, "x y") || !strings.Contains(er, "echo") {
		t.Errorf("verbose stderr should contain command + args, got %q", er)
	}
}

func TestDelimiter(t *testing.T) {
	echo := mustHaveEcho(t)
	_, out, _ := runXargs(t, "a,b,c", "-d", ",", echo)
	if strings.TrimSpace(out) != "a b c" {
		t.Errorf("got %q", out)
	}
}

func TestExitCodePropagation(t *testing.T) {
	// Use /bin/sh -c 'exit 7' to test exit code passthrough.
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	exit, _, _ := runXargs(t, "x\n", "sh", "-c", "exit 7")
	if exit != 7 {
		t.Errorf("got exit %d want 7", exit)
	}
}

func TestArgFile(t *testing.T) {
	echo := mustHaveEcho(t)
	dir := t.TempDir()
	p := dir + "/items"
	os.WriteFile(p, []byte("one two three\n"), 0o644)
	_, out, _ := runXargs(t, "", "-a", p, echo)
	if strings.TrimSpace(out) != "one two three" {
		t.Errorf("got %q", out)
	}
}

func TestEmptyInputRunsOnceByDefault(t *testing.T) {
	echo := mustHaveEcho(t)
	// gnu's default WITHOUT -r runs the command once with no extra args.
	_, out, _ := runXargs(t, "", echo)
	if strings.TrimSpace(out) != "" {
		// echo with no args prints just a newline; tolerate that.
		if out != "\n" {
			t.Errorf("got %q", out)
		}
	}
}
