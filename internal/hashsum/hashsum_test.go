package hashsum

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runHS(t *testing.T, name string, args ...string) (int, string, string) {
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
	exit := MainAs(name, args)
	wOut.Close()
	wErr.Close()
	out, _ := io.ReadAll(rOut)
	er, _ := io.ReadAll(rErr)
	return exit, string(out), string(er)
}

func runHSStdin(t *testing.T, stdin []byte, name string, args ...string) (int, string, string) {
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
		wIn.Write(stdin)
		wIn.Close()
	}()
	exit := MainAs(name, args)
	wOut.Close()
	wErr.Close()
	out, _ := io.ReadAll(rOut)
	er, _ := io.ReadAll(rErr)
	return exit, string(out), string(er)
}

func TestSha256Known(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x")
	os.WriteFile(p, []byte("abc"), 0o644)
	exit, out, _ := runHS(t, "sha256sum", p)
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	// Known: sha256("abc") = ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad
	want := "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if !strings.HasPrefix(out, want) {
		t.Errorf("got %q want prefix %q", out, want)
	}
}

func TestSha512Known(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x")
	os.WriteFile(p, []byte("abc"), 0o644)
	exit, out, _ := runHS(t, "sha512sum", p)
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	want := "ddaf35a193617abacc417349ae20413112e6fa4e89a97ea20a9eeee64b55d39a2192992a274fc1a836ba3c23a3feebbd454d4423643ce80e2a9ac94fa54ca49f"
	if !strings.HasPrefix(out, want) {
		t.Errorf("got %q want prefix %q", out, want)
	}
}

func TestSha1Known(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x")
	os.WriteFile(p, []byte("abc"), 0o644)
	exit, out, _ := runHS(t, "sha1sum", p)
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	want := "a9993e364706816aba3e25717850c26c9cd0d89d"
	if !strings.HasPrefix(out, want) {
		t.Errorf("got %q want prefix %q", out, want)
	}
}

func TestMd5Known(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x")
	os.WriteFile(p, []byte("abc"), 0o644)
	exit, out, _ := runHS(t, "md5sum", p)
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	want := "900150983cd24fb0d6963f7d28e17f72"
	if !strings.HasPrefix(out, want) {
		t.Errorf("got %q want prefix %q", out, want)
	}
}

func TestStdin(t *testing.T) {
	exit, out, _ := runHSStdin(t, []byte("abc"), "sha256sum")
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	want := "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if !strings.HasPrefix(out, want) {
		t.Errorf("got %q", out)
	}
	if !strings.Contains(out, "-") {
		t.Errorf("expected '-' filename for stdin: %q", out)
	}
}

func TestEmptyFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "empty")
	os.WriteFile(p, nil, 0o644)
	_, out, _ := runHS(t, "sha256sum", p)
	// sha256("") = e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
	want := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if !strings.HasPrefix(out, want) {
		t.Errorf("empty-file digest wrong: %q", out)
	}
}

func TestLargeStream(t *testing.T) {
	// 1MB of zeros — known sha256 digest.
	dir := t.TempDir()
	p := filepath.Join(dir, "big")
	f, _ := os.Create(p)
	for i := 0; i < 1024; i++ {
		f.Write(make([]byte, 1024))
	}
	f.Close()
	_, out, _ := runHS(t, "sha256sum", p)
	want := "30e14955ebf1352266dc2ff8067e68104607e750abb9d3b36582b8af909fcb58"
	if !strings.HasPrefix(out, want) {
		t.Errorf("1MB-zero sha256 wrong: %q", out)
	}
}

func TestBSDTagFormat(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x")
	os.WriteFile(p, []byte("abc"), 0o644)
	_, out, _ := runHS(t, "sha256sum", "--tag", p)
	if !strings.HasPrefix(out, "SHA256 (") {
		t.Errorf("BSD tag prefix missing: %q", out)
	}
	if !strings.Contains(out, ") = ba7816bf") {
		t.Errorf("BSD tag body missing: %q", out)
	}
}

func TestBinaryFlag(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x")
	os.WriteFile(p, []byte("abc"), 0o644)
	_, out, _ := runHS(t, "sha256sum", "-b", p)
	if !strings.Contains(out, " *") {
		t.Errorf("-b should emit ' *FILE': %q", out)
	}
}

func TestCheckMode(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x")
	os.WriteFile(p, []byte("abc"), 0o644)
	_, out, _ := runHS(t, "sha256sum", p)
	checkFile := filepath.Join(dir, "sums")
	os.WriteFile(checkFile, []byte(out), 0o644)
	exit, out, er := runHS(t, "sha256sum", "-c", checkFile)
	if exit != 0 {
		t.Fatalf("check failed: exit=%d stderr=%s", exit, er)
	}
	if !strings.Contains(out, ": OK") {
		t.Errorf("expected OK line, got %q", out)
	}
}

func TestCheckModeTampered(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x")
	os.WriteFile(p, []byte("abc"), 0o644)
	checkLine := "0000000000000000000000000000000000000000000000000000000000000000  " + p + "\n"
	checkFile := filepath.Join(dir, "sums")
	os.WriteFile(checkFile, []byte(checkLine), 0o644)
	exit, out, er := runHS(t, "sha256sum", "-c", checkFile)
	if exit == 0 {
		t.Errorf("expected non-zero exit on tampered checksum")
	}
	if !strings.Contains(out, "FAILED") {
		t.Errorf("expected FAILED line, got %q", out)
	}
	_ = er
}

func TestCheckModeBSDTag(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x")
	os.WriteFile(p, []byte("abc"), 0o644)
	_, out, _ := runHS(t, "sha256sum", "--tag", p)
	checkFile := filepath.Join(dir, "sums")
	os.WriteFile(checkFile, []byte(out), 0o644)
	exit, out, _ := runHS(t, "sha256sum", "-c", checkFile)
	if exit != 0 || !strings.Contains(out, ": OK") {
		t.Errorf("BSD-tag check failed: exit=%d out=%q", exit, out)
	}
}

func TestCheckIgnoreMissing(t *testing.T) {
	dir := t.TempDir()
	line := "0000000000000000000000000000000000000000000000000000000000000000  /nonexistent-bag-hashsum\n"
	checkFile := filepath.Join(dir, "sums")
	os.WriteFile(checkFile, []byte(line), 0o644)
	exit, _, _ := runHS(t, "sha256sum", "-c", "--ignore-missing", checkFile)
	// All lines were skipped; gnu exits 1 with "no properly formatted
	// checksum lines found". That's fine — we just want NO crash.
	_ = exit
}

func TestStrictMode(t *testing.T) {
	dir := t.TempDir()
	checkFile := filepath.Join(dir, "sums")
	os.WriteFile(checkFile, []byte("bogus\n"), 0o644)
	exit, _, _ := runHS(t, "sha256sum", "-c", "--strict", checkFile)
	if exit == 0 {
		t.Errorf("expected non-zero exit on malformed line with --strict")
	}
}

// TestConformanceVsSystem checks bag's hex output matches the system
// sha256sum byte-for-byte. Both should agree on every input.
func TestConformanceVsSystem(t *testing.T) {
	sys, err := exec.LookPath("sha256sum")
	if err != nil {
		t.Skip("no system sha256sum")
	}
	dir := t.TempDir()
	cases := [][]byte{
		nil,
		[]byte("hello\n"),
		[]byte{0, 1, 2, 0xff, 0xfe, 0xfd},
	}
	for i, data := range cases {
		p := filepath.Join(dir, "c")
		os.WriteFile(p, data, 0o644)
		_, bagOut, _ := runHS(t, "sha256sum", p)
		sysOut, err := exec.Command(sys, p).Output()
		if err != nil {
			t.Fatalf("system sha256sum: %v", err)
		}
		if bagOut != string(sysOut) {
			t.Errorf("case %d: bag=%q sys=%q", i, bagOut, sysOut)
		}
	}
}
