package jq

import (
	"io"
	"os"
	"strings"
	"testing"
)

func runJQ(t *testing.T, stdin string, args ...string) (int, string, string) {
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

func TestIdentity(t *testing.T) {
	exit, out, _ := runJQ(t, `{"a":1,"b":2}`, ".")
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if !strings.Contains(out, `"a": 1`) {
		t.Errorf("got %q", out)
	}
}

func TestFieldAccess(t *testing.T) {
	_, out, _ := runJQ(t, `{"name":"alice"}`, ".name")
	if strings.TrimSpace(out) != `"alice"` {
		t.Errorf("got %q", out)
	}
}

func TestRawOutput(t *testing.T) {
	_, out, _ := runJQ(t, `{"name":"alice"}`, "-r", ".name")
	if strings.TrimSpace(out) != "alice" {
		t.Errorf("got %q", out)
	}
}

func TestCompact(t *testing.T) {
	_, out, _ := runJQ(t, `{"a":1,"b":2}`, "-c", ".")
	if strings.TrimSpace(out) != `{"a":1,"b":2}` {
		t.Errorf("got %q", out)
	}
}

func TestArrayIteration(t *testing.T) {
	_, out, _ := runJQ(t, `[{"a":1},{"a":2},{"a":3}]`, "-c", ".[].a")
	want := "1\n2\n3\n"
	if out != want {
		t.Errorf("got %q want %q", out, want)
	}
}

func TestPipe(t *testing.T) {
	_, out, _ := runJQ(t, `{"items":[1,2,3]}`, "-c", ".items | length")
	if strings.TrimSpace(out) != "3" {
		t.Errorf("got %q", out)
	}
}

func TestMap(t *testing.T) {
	_, out, _ := runJQ(t, `[1,2,3]`, "-c", "map(. * 2)")
	if strings.TrimSpace(out) != "[2,4,6]" {
		t.Errorf("got %q", out)
	}
}

func TestSelect(t *testing.T) {
	_, out, _ := runJQ(t, `[{"n":1},{"n":5},{"n":3}]`, "-c", ".[] | select(.n > 2) | .n")
	want := "5\n3\n"
	if out != want {
		t.Errorf("got %q", out)
	}
}

func TestKeys(t *testing.T) {
	_, out, _ := runJQ(t, `{"b":2,"a":1}`, "-c", "keys")
	if strings.TrimSpace(out) != `["a","b"]` {
		t.Errorf("got %q", out)
	}
}

func TestType(t *testing.T) {
	_, out, _ := runJQ(t, `[1,"two",null,{},[]]`, "-c", ".[] | type")
	want := `"number"
"string"
"null"
"object"
"array"
`
	if out != want {
		t.Errorf("got %q want %q", out, want)
	}
}

func TestSlurp(t *testing.T) {
	_, out, _ := runJQ(t, "1\n2\n3\n", "-s", "-c", "add")
	if strings.TrimSpace(out) != "6" {
		t.Errorf("got %q", out)
	}
}

func TestRawInput(t *testing.T) {
	_, out, _ := runJQ(t, "hello\nworld\n", "-R", "-c", ".")
	want := "\"hello\"\n\"world\"\n"
	if out != want {
		t.Errorf("got %q want %q", out, want)
	}
}

func TestNullInput(t *testing.T) {
	_, out, _ := runJQ(t, "", "-n", "-c", "42")
	if strings.TrimSpace(out) != "42" {
		t.Errorf("got %q", out)
	}
}

func TestArg(t *testing.T) {
	_, out, _ := runJQ(t, `{"k":"v"}`, "-c", "--arg", "name", "alice", `{name: $name, k}`)
	if !strings.Contains(out, `"name":"alice"`) {
		t.Errorf("got %q", out)
	}
}

func TestArgjson(t *testing.T) {
	_, out, _ := runJQ(t, "null", "-c", "--argjson", "x", "[1,2,3]", "$x")
	if strings.TrimSpace(out) != "[1,2,3]" {
		t.Errorf("got %q", out)
	}
}

func TestExitCodeFalse(t *testing.T) {
	exit, _, _ := runJQ(t, "false", "-c", ".")
	if exit != 1 {
		t.Errorf("got exit %d want 1 (last truthy = false)", exit)
	}
}

func TestExitCodeTrue(t *testing.T) {
	exit, _, _ := runJQ(t, "true", "-c", ".")
	if exit != 0 {
		t.Errorf("got exit %d want 0", exit)
	}
}

func TestParseError(t *testing.T) {
	exit, _, er := runJQ(t, "{}", "-c", `..."`)
	if exit == 0 {
		t.Errorf("expected non-zero exit on bad filter")
	}
	if er == "" {
		t.Errorf("expected diagnostic")
	}
}
