package tarcmd

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func runTar(t *testing.T, stdin []byte, args ...string) (int, []byte, []byte) {
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
	exit := Main(args)
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

func TestCreateAndList(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(old)

	writeTree(t, "src", map[string]string{
		"a.txt":     "AAA\n",
		"sub/b.txt": "BBB\n",
	})

	exit, _, _ := runTar(t, nil, "-cf", "out.tar", "src")
	if exit != 0 {
		t.Fatalf("create exit=%d", exit)
	}
	exit, out, _ := runTar(t, nil, "-tf", "out.tar")
	if exit != 0 {
		t.Fatalf("list exit=%d", exit)
	}
	got := string(out)
	for _, want := range []string{"src/", "src/a.txt", "src/sub/b.txt"} {
		if !bytes.Contains([]byte(got), []byte(want)) {
			t.Errorf("missing %q in listing %q", want, got)
		}
	}
}

func TestRoundTripDir(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(old)

	writeTree(t, "src", map[string]string{
		"a.txt":         "AAA\n",
		"sub/deep/b.txt": "BBB\n",
	})

	if exit, _, _ := runTar(t, nil, "-cf", "out.tar", "src"); exit != 0 {
		t.Fatalf("create exit=%d", exit)
	}
	os.MkdirAll("dest", 0o755)
	if exit, _, _ := runTar(t, nil, "-xf", "out.tar", "-C", "dest"); exit != 0 {
		t.Fatalf("extract exit=%d", exit)
	}

	body, err := os.ReadFile("dest/src/a.txt")
	if err != nil || string(body) != "AAA\n" {
		t.Errorf("a.txt: %s err=%v", body, err)
	}
	body, err = os.ReadFile("dest/src/sub/deep/b.txt")
	if err != nil || string(body) != "BBB\n" {
		t.Errorf("b.txt: %s err=%v", body, err)
	}
}

func TestRoundTripGzipped(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(old)

	writeTree(t, "src", map[string]string{
		"hello.txt": "hello world\n",
	})
	if exit, _, _ := runTar(t, nil, "-czf", "out.tgz", "src"); exit != 0 {
		t.Fatalf("create exit=%d", exit)
	}
	os.MkdirAll("dest", 0o755)
	if exit, _, _ := runTar(t, nil, "-xf", "out.tgz", "-C", "dest", "-a"); exit != 0 {
		t.Fatalf("extract exit=%d", exit)
	}
	body, _ := os.ReadFile("dest/src/hello.txt")
	if string(body) != "hello world\n" {
		t.Errorf("got %q", body)
	}
}

func TestRoundTripZstd(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(old)
	writeTree(t, "src", map[string]string{"x": "X\n"})
	if exit, _, _ := runTar(t, nil, "-cf", "out.tzst", "--zstd", "src"); exit != 0 {
		t.Fatalf("create exit=%d", exit)
	}
	os.MkdirAll("dest", 0o755)
	if exit, _, _ := runTar(t, nil, "-xf", "out.tzst", "--zstd", "-C", "dest"); exit != 0 {
		t.Fatalf("extract exit=%d", exit)
	}
	body, _ := os.ReadFile("dest/src/x")
	if string(body) != "X\n" {
		t.Errorf("got %q", body)
	}
}

func TestStripComponents(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(old)

	writeTree(t, "src/a/b", map[string]string{
		"deep.txt": "deep\n",
	})
	if exit, _, _ := runTar(t, nil, "-cf", "out.tar", "src"); exit != 0 {
		t.Fatalf("create exit=%d", exit)
	}
	os.MkdirAll("dest", 0o755)
	if exit, _, _ := runTar(t, nil, "-xf", "out.tar", "--strip-components=2", "-C", "dest"); exit != 0 {
		t.Fatalf("extract exit=%d", exit)
	}
	body, err := os.ReadFile("dest/b/deep.txt")
	if err != nil {
		t.Fatalf("expected dest/b/deep.txt: %v", err)
	}
	if string(body) != "deep\n" {
		t.Errorf("got %q", body)
	}
}

func TestRefuseAbsoluteOrParent(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(old)

	// Hand-craft an evil archive.
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	tw.WriteHeader(&tar.Header{Name: "../escape.txt", Size: 5, Mode: 0o644})
	tw.Write([]byte("boom\n"))
	tw.Close()

	os.WriteFile("evil.tar", buf.Bytes(), 0o644)
	exit, _, stderr := runTar(t, nil, "-xf", "evil.tar")
	if exit == 0 {
		t.Errorf("exit=0; expected refusal")
	}
	if !bytes.Contains(stderr, []byte("refusing")) {
		t.Errorf("expected 'refusing' in stderr: %s", stderr)
	}
}

func TestExclude(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(old)

	writeTree(t, "src", map[string]string{
		"keep.txt":   "keep\n",
		"skip.tmp":   "skip\n",
		"sub/x.txt":  "x\n",
	})
	exit, _, _ := runTar(t, nil, "-cf", "out.tar", "--exclude=*.tmp", "src")
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	exit, out, _ := runTar(t, nil, "-tf", "out.tar")
	if exit != 0 {
		t.Fatalf("list exit=%d", exit)
	}
	if bytes.Contains(out, []byte("skip.tmp")) {
		t.Errorf("excluded file present: %s", out)
	}
}
