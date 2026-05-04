// Package conformance is the cross-binary test harness.
//
// Each Case describes a single shell-equivalent invocation (e.g.
// `curl -sL <URL>`). The runner runs the case under both the real tool
// (curl / wget on PATH, or pointed to via BAG_REAL_CURL / BAG_REAL_WGET)
// and bag's drop-in replacement, and applies a set of expectations to
// the diff.
//
// What we compare:
//   - stdout bytes (sometimes via JSON-aware comparison so dynamic
//     fields like timestamps and remote IP are skipped)
//   - exit code (sometimes "must match exactly", sometimes "both 0
//     OR both non-zero")
//   - files the tool wrote into the per-case temp dir
//
// What we don't compare:
//   - stderr text (curl/wget version-to-version variation makes this a
//     losing battle); we do assert presence of structural elements
//     like "> " / "< " trace prefixes for -v.
//   - User-Agent: bag uses "curl/8.0.0 (bag)" or "Wget/1.21.4 (bag)" —
//     test cases that echo headers explicitly use --header overrides.
package conformance

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"

	"github.com/oreparaz/bag/test/testserver"
)

// Tool selects which side of the conformance suite a case belongs to.
type Tool string

const (
	ToolCurl Tool = "curl"
	ToolWget Tool = "wget"
)

// Env is what each case's Args function receives.
type Env struct {
	HTTP    string // base URL of the HTTP test server
	HTTPS   string // base URL of the HTTPS test server
	CAPath  string // PEM file with the test CA root
	TempDir string // per-case scratch directory
}

// Case is one conformance test.
type Case struct {
	Name string
	Tool Tool

	// Args returns the argv (without argv[0]) to pass to both the real
	// tool and bag's. SkipReason can be returned to skip the case.
	Args func(env Env) (args []string, skip string)

	// ExpectExitMatch: both implementations must exit with the same code.
	// Default: true.
	ExpectExitMatch *bool

	// ExpectExit asserts a specific exit code if non-nil.
	ExpectExit *int

	// CompareStdout: byte-for-byte stdout equality. Default true unless
	// CompareJSON or CompareFile is set.
	CompareStdout *bool

	// CompareJSON parses both stdouts as JSON and compares with the
	// listed top-level fields removed (used to ignore dynamic fields
	// like X-Amzn-Trace-Id, origin IP, …).
	CompareJSON       bool
	JSONIgnoreFields  []string
	JSONIgnoreHeaders []string // ignored sub-keys under "headers"

	// CompareFile compares this file under TempDir. Empty means none.
	CompareFile string

	// Stable: if false, real curl/wget output may be inherently
	// non-deterministic for this case (e.g. progress on stderr); we
	// run the assertion only on bag.
	Stable *bool
}

func ptr[T any](v T) *T { return &v }

// Run executes the corpus. realBin and bagBin are absolute or PATH-resolvable
// paths.
func Run(t *testing.T, cases []Case, realBin, bagBin string) {
	t.Helper()

	srv, err := testserver.Start()
	if err != nil {
		t.Fatalf("test server: %v", err)
	}
	defer srv.Close()

	caDir := t.TempDir()
	caPath := filepath.Join(caDir, "ca.pem")
	if err := os.WriteFile(caPath, srv.CACertPEM, 0o644); err != nil {
		t.Fatalf("write CA: %v", err)
	}

	for _, c := range cases {
		c := c
		t.Run(string(c.Tool)+"/"+c.Name, func(t *testing.T) {
			runOne(t, c, realBin, bagBin, srv, caPath)
		})
	}
}

func runOne(t *testing.T, c Case, realBin, bagBin string, srv *testserver.Servers, caPath string) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	envA := Env{HTTP: srv.HTTP.URL, HTTPS: srv.HTTPS.URL, CAPath: caPath, TempDir: dirA}
	envB := Env{HTTP: srv.HTTP.URL, HTTPS: srv.HTTPS.URL, CAPath: caPath, TempDir: dirB}

	argsA, skipA := c.Args(envA)
	argsB, skipB := c.Args(envB)
	if skipA != "" || skipB != "" {
		t.Skipf("skipped: %s%s", skipA, skipB)
	}

	resA := runCmd(t, realBin, argsA, dirA)
	resB := runCmd(t, bagBin, argsB, dirB)

	expectExitMatch := true
	if c.ExpectExitMatch != nil {
		expectExitMatch = *c.ExpectExitMatch
	}
	if expectExitMatch {
		if resA.exit != resB.exit {
			t.Errorf("exit mismatch: real=%d bag=%d\nreal stderr:\n%s\nbag stderr:\n%s",
				resA.exit, resB.exit, truncate(resA.stderr, 4000), truncate(resB.stderr, 4000))
		}
	}
	if c.ExpectExit != nil {
		if resB.exit != *c.ExpectExit {
			t.Errorf("bag exit=%d want %d (real=%d)", resB.exit, *c.ExpectExit, resA.exit)
		}
	}

	cmp := true
	if c.CompareStdout != nil {
		cmp = *c.CompareStdout
	}
	if c.CompareJSON || c.CompareFile != "" {
		cmp = false
	}
	if cmp {
		if !bytes.Equal(resA.stdout, resB.stdout) {
			t.Errorf("stdout mismatch:\nreal len=%d sha=%s\nbag  len=%d sha=%s\nreal stdout (first 4k):\n%s\n---\nbag stdout (first 4k):\n%s",
				len(resA.stdout), sumHex(resA.stdout),
				len(resB.stdout), sumHex(resB.stdout),
				truncate(resA.stdout, 4000), truncate(resB.stdout, 4000))
		}
	}
	if c.CompareJSON {
		if err := compareJSON(resA.stdout, resB.stdout, c.JSONIgnoreFields, c.JSONIgnoreHeaders); err != nil {
			t.Errorf("JSON mismatch: %v\nreal: %s\nbag : %s", err,
				truncate(resA.stdout, 1000), truncate(resB.stdout, 1000))
		}
	}
	if c.CompareFile != "" {
		a, errA := os.ReadFile(filepath.Join(dirA, c.CompareFile))
		b, errB := os.ReadFile(filepath.Join(dirB, c.CompareFile))
		if errA != nil || errB != nil {
			t.Errorf("read CompareFile %q: real=%v bag=%v", c.CompareFile, errA, errB)
			return
		}
		if !bytes.Equal(a, b) {
			t.Errorf("CompareFile %q mismatch (real=%dB bag=%dB)", c.CompareFile, len(a), len(b))
		}
	}
}

type cmdResult struct {
	stdout []byte
	stderr []byte
	exit   int
}

func runCmd(t *testing.T, bin string, args []string, cwd string) cmdResult {
	t.Helper()
	ctx, cancel := newTimeout()
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = cwd
	var so, se bytes.Buffer
	cmd.Stdout = &so
	cmd.Stderr = &se
	// Provide an isolated env: we rely on PATH for bash/dns but otherwise
	// nothing leaks in (no proxies, no curlrc).
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + cwd,
		"NO_PROXY=*",
	}
	err := cmd.Run()
	exit := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		exit = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("running %s %v: %v", bin, args, err)
	}
	return cmdResult{stdout: so.Bytes(), stderr: se.Bytes(), exit: exit}
}

func compareJSON(a, b []byte, ignoreFields, ignoreHeaders []string) error {
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		return fmt.Errorf("real not JSON: %w", err)
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		return fmt.Errorf("bag not JSON: %w", err)
	}
	scrub(av, ignoreFields, ignoreHeaders)
	scrub(bv, ignoreFields, ignoreHeaders)
	if !deepEq(av, bv) {
		ja, _ := json.MarshalIndent(av, "", "  ")
		jb, _ := json.MarshalIndent(bv, "", "  ")
		return fmt.Errorf("JSON differs:\n--- real ---\n%s\n--- bag ---\n%s", ja, jb)
	}
	return nil
}

// defaultIgnoredHeaders are protocol-level headers we always strip before
// JSON comparison. Distros and tool versions vary on these — Fedora's
// wget2 sends "Connection: keep-alive" while Ubuntu's classic wget
// sends "Connection: Keep-Alive"; wget2 advertises compression even
// without --compressed; etc. The conformance tests assert behavior, not
// byte-exact wire protocol details.
var defaultIgnoredHeaders = []string{
	"Accept-Encoding",
	"Connection",
	"Te",
}

func scrub(v any, fields, headers []string) {
	m, ok := v.(map[string]any)
	if !ok {
		return
	}
	for _, f := range fields {
		delete(m, f)
	}
	if hh, ok := m["headers"].(map[string]any); ok {
		for _, h := range defaultIgnoredHeaders {
			delete(hh, h)
		}
		for _, h := range headers {
			delete(hh, h)
		}
	}
}

func deepEq(a, b any) bool {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return bytes.Equal(ja, jb)
}

func sumHex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:8])
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + fmt.Sprintf("\n... (%d more bytes)", len(b)-n)
}

// listFiles returns relative paths of files under root, sorted.
func listFiles(root string) []string {
	var out []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		out = append(out, rel)
		return nil
	})
	sort.Strings(out)
	return out
}

