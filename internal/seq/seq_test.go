package seq

import (
	"io"
	"os"
	"strings"
	"testing"
)

func runSeq(t *testing.T, args ...string) (int, string, string) {
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

func TestOneArgLast(t *testing.T) {
	_, out, _ := runSeq(t, "5")
	if out != "1\n2\n3\n4\n5\n" {
		t.Errorf("got %q", out)
	}
}

func TestTwoArgsFirstLast(t *testing.T) {
	_, out, _ := runSeq(t, "3", "5")
	if out != "3\n4\n5\n" {
		t.Errorf("got %q", out)
	}
}

func TestThreeArgsStep(t *testing.T) {
	_, out, _ := runSeq(t, "1", "2", "9")
	if out != "1\n3\n5\n7\n9\n" {
		t.Errorf("got %q", out)
	}
}

func TestNegativeStep(t *testing.T) {
	_, out, _ := runSeq(t, "5", "-1", "1")
	if out != "5\n4\n3\n2\n1\n" {
		t.Errorf("got %q", out)
	}
}

func TestSeparator(t *testing.T) {
	_, out, _ := runSeq(t, "-s", ",", "1", "3")
	if strings.TrimRight(out, "\n") != "1,2,3" {
		t.Errorf("got %q", out)
	}
}

func TestEqualWidth(t *testing.T) {
	_, out, _ := runSeq(t, "-w", "8", "12")
	if out != "08\n09\n10\n11\n12\n" {
		t.Errorf("got %q", out)
	}
}

func TestFloatStep(t *testing.T) {
	_, out, _ := runSeq(t, "0", "0.5", "2")
	want := "0.0\n0.5\n1.0\n1.5\n2.0\n"
	if out != want {
		t.Errorf("got %q want %q", out, want)
	}
}

func TestEmptyRange(t *testing.T) {
	exit, out, _ := runSeq(t, "5", "1") // first > last with default step +1
	if exit != 0 {
		t.Errorf("exit=%d", exit)
	}
	if out != "" {
		t.Errorf("expected empty, got %q", out)
	}
}

func TestZeroIncrementRejected(t *testing.T) {
	exit, _, er := runSeq(t, "1", "0", "5")
	if exit == 0 {
		t.Errorf("expected error with step=0")
	}
	if !strings.Contains(er, "non-zero") {
		t.Errorf("expected diagnostic, got %s", er)
	}
}

func TestMissingOperand(t *testing.T) {
	exit, _, _ := runSeq(t)
	if exit == 0 {
		t.Errorf("expected error with no args")
	}
}

func TestExplicitFormat(t *testing.T) {
	_, out, _ := runSeq(t, "-f", "%03d", "1", "3")
	if out != "001\n002\n003\n" {
		t.Errorf("got %q", out)
	}
}

func TestNegativeLast(t *testing.T) {
	// "seq 1 -1 -3" should be 1, 0, -1, -2, -3
	_, out, _ := runSeq(t, "1", "-1", "-3")
	if out != "1\n0\n-1\n-2\n-3\n" {
		t.Errorf("got %q", out)
	}
}
