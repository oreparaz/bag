package zipcmd

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runMain(t *testing.T, name string, stdin []byte, args ...string) (int, []byte, []byte) {
	t.Helper()
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout = wOut
	os.Stderr = wErr
	if stdin != nil {
		oldIn := os.Stdin
		rIn, wIn, _ := os.Pipe()
		os.Stdin = rIn
		go func() { wIn.Write(stdin); wIn.Close() }()
		defer func() { os.Stdin = oldIn }()
	}
	exit := MainAs(name, args)
	wOut.Close()
	wErr.Close()
	os.Stdout = oldOut
	os.Stderr = oldErr
	out, _ := io.ReadAll(rOut)
	se, _ := io.ReadAll(rErr)
	return exit, out, se
}

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for path, content := range files {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestZipUnzipRoundTrip(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(old)

	writeTree(t, "src", map[string]string{
		"a.txt":         "AAA\n",
		"sub/inner.txt": "deep\n",
	})

	if exit, _, _ := runMain(t, "zip", nil, "-rq", "out.zip", "src"); exit != 0 {
		t.Fatalf("zip exit=%d", exit)
	}

	os.MkdirAll("dest", 0o755)
	if exit, _, _ := runMain(t, "unzip", nil, "-q", "out.zip", "-d", "dest"); exit != 0 {
		t.Fatalf("unzip exit=%d", exit)
	}

	body, err := os.ReadFile("dest/src/a.txt")
	if err != nil || string(body) != "AAA\n" {
		t.Errorf("a.txt=%q err=%v", body, err)
	}
	body, err = os.ReadFile("dest/src/sub/inner.txt")
	if err != nil || string(body) != "deep\n" {
		t.Errorf("inner.txt=%q err=%v", body, err)
	}
}

func TestZipPipe(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(old)

	os.WriteFile("hello.txt", []byte("hello\n"), 0o644)
	if exit, _, _ := runMain(t, "zip", nil, "-q", "out.zip", "hello.txt"); exit != 0 {
		t.Fatalf("zip exit=%d", exit)
	}
	exit, out, _ := runMain(t, "unzip", nil, "-p", "out.zip")
	if exit != 0 {
		t.Fatalf("unzip -p exit=%d", exit)
	}
	if string(out) != "hello\n" {
		t.Errorf("got %q", out)
	}
}

func TestZipList(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(old)

	os.WriteFile("a", []byte("A"), 0o644)
	os.WriteFile("b", []byte("BB"), 0o644)
	if exit, _, _ := runMain(t, "zip", nil, "-q", "out.zip", "a", "b"); exit != 0 {
		t.Fatalf("zip exit=%d", exit)
	}
	exit, out, _ := runMain(t, "unzip", nil, "-l", "out.zip")
	if exit != 0 {
		t.Fatalf("list exit=%d", exit)
	}
	for _, want := range []string{"a", "b", "Length", "files"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("missing %q in listing %q", want, out)
		}
	}
}

func TestZipJunk(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(old)

	os.MkdirAll("a/b/c", 0o755)
	os.WriteFile("a/b/c/inside.txt", []byte("X\n"), 0o644)

	if exit, _, _ := runMain(t, "zip", nil, "-jq", "out.zip", "a/b/c/inside.txt"); exit != 0 {
		t.Fatalf("zip exit=%d", exit)
	}
	zr, err := zip.OpenReader("out.zip")
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	if len(zr.File) != 1 {
		t.Fatalf("expected 1 entry; got %d", len(zr.File))
	}
	if zr.File[0].Name != "inside.txt" {
		t.Errorf("expected basename only; got %q", zr.File[0].Name)
	}
}

func TestZipRefuseEvilEntry(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(old)

	// Hand-craft an evil zip with "../escape.txt".
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("../escape.txt")
	w.Write([]byte("boom\n"))
	zw.Close()
	os.WriteFile("evil.zip", buf.Bytes(), 0o644)

	exit, _, stderr := runMain(t, "unzip", nil, "evil.zip")
	if exit == 0 {
		t.Errorf("exit=0; expected refusal")
	}
	if !bytes.Contains(stderr, []byte("refusing")) {
		t.Errorf("expected 'refusing' in stderr: %s", stderr)
	}
}
