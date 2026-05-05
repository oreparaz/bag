package conformance

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// TestMain prepares the bag binary once for the whole package.
//
// If BAG_BIN names an existing binary, we use it as-is (Docker / CI use this
// to ship a pre-built binary into a distro image without Go). Otherwise we
// `go build` from the repo root.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "bag-conformance-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "mkdir tmp: %v\n", err)
		os.Exit(1)
	}

	var bag string
	if pre := os.Getenv("BAG_BIN"); pre != "" {
		// Copy into our temp dir so we can hard-link to it as curl / wget.
		bag = filepath.Join(dir, "bag")
		if err := copyFile(pre, bag, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "copy %s: %v\n", pre, err)
			os.RemoveAll(dir)
			os.Exit(1)
		}
	} else {
		bag = filepath.Join(dir, "bag")
		cmd := exec.Command("go", "build", "-o", bag, ".")
		cmd.Dir = repoRoot()
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "go build failed: %v\n%s\n", err, stderr.String())
			os.RemoveAll(dir)
			os.Exit(1)
		}
	}
	sharedBag.path = bag
	sharedBag.dir = dir

	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}

// TestCurlConformance runs the curl corpus against system curl + bag.
func TestCurlConformance(t *testing.T) {
	realBin := resolveTool(t, "BAG_REAL_CURL", "curl")
	bagBin := buildBagCurl(t)
	Run(t, CurlCases(), realBin, bagBin)
}

// TestWgetConformance runs the wget corpus against system wget + bag.
func TestWgetConformance(t *testing.T) {
	realBin := resolveTool(t, "BAG_REAL_WGET", "wget")
	bagBin := buildBagWget(t)
	Run(t, WgetCases(), realBin, bagBin)
}

// TestCatConformance runs the cat corpus against system cat + bag.
func TestCatConformance(t *testing.T) {
	realBin := resolveTool(t, "BAG_REAL_CAT", "cat")
	bagBin := buildBagAs(t, "cat")
	Run(t, CatCases(), realBin, bagBin)
}

// TestHeadConformance runs the head corpus against system head + bag.
func TestHeadConformance(t *testing.T) {
	realBin := resolveTool(t, "BAG_REAL_HEAD", "head")
	bagBin := buildBagAs(t, "head")
	Run(t, HeadCases(), realBin, bagBin)
}

// TestTailConformance runs the tail corpus against system tail + bag.
func TestTailConformance(t *testing.T) {
	realBin := resolveTool(t, "BAG_REAL_TAIL", "tail")
	bagBin := buildBagAs(t, "tail")
	Run(t, TailCases(), realBin, bagBin)
}

// TestBase64Conformance runs the base64 corpus against system base64 + bag.
func TestBase64Conformance(t *testing.T) {
	realBin := resolveTool(t, "BAG_REAL_BASE64", "base64")
	bagBin := buildBagAs(t, "base64")
	Run(t, Base64Cases(), realBin, bagBin)
}

// TestTeeConformance runs the tee corpus against system tee + bag.
func TestTeeConformance(t *testing.T) {
	realBin := resolveTool(t, "BAG_REAL_TEE", "tee")
	bagBin := buildBagAs(t, "tee")
	Run(t, TeeCases(), realBin, bagBin)
}

// TestWCConformance runs the wc corpus against system wc + bag.
func TestWCConformance(t *testing.T) {
	realBin := resolveTool(t, "BAG_REAL_WC", "wc")
	bagBin := buildBagAs(t, "wc")
	Run(t, WCCases(), realBin, bagBin)
}

// TestXXDConformance runs the xxd corpus against system xxd + bag.
// Skipped automatically when xxd isn't installed on the system.
func TestXXDConformance(t *testing.T) {
	realBin := resolveTool(t, "BAG_REAL_XXD", "xxd")
	bagBin := buildBagAs(t, "xxd")
	Run(t, XXDCases(), realBin, bagBin)
}

// TestGrepConformance runs the grep corpus against system grep + bag.
func TestGrepConformance(t *testing.T) {
	realBin := resolveTool(t, "BAG_REAL_GREP", "grep")
	bagBin := buildBagAs(t, "grep")
	Run(t, GrepCases(), realBin, bagBin)
}

// TestSedConformance runs the sed corpus against system sed + bag.
func TestSedConformance(t *testing.T) {
	realBin := resolveTool(t, "BAG_REAL_SED", "sed")
	bagBin := buildBagAs(t, "sed")
	Run(t, SedCases(), realBin, bagBin)
}

// TestCutConformance runs the cut corpus against system cut + bag.
func TestCutConformance(t *testing.T) {
	realBin := resolveTool(t, "BAG_REAL_CUT", "cut")
	bagBin := buildBagAs(t, "cut")
	Run(t, CutCases(), realBin, bagBin)
}

// TestUniqConformance runs the uniq corpus against system uniq + bag.
func TestUniqConformance(t *testing.T) {
	realBin := resolveTool(t, "BAG_REAL_UNIQ", "uniq")
	bagBin := buildBagAs(t, "uniq")
	Run(t, UniqCases(), realBin, bagBin)
}

// TestSortConformance runs the sort corpus against system sort + bag.
func TestSortConformance(t *testing.T) {
	realBin := resolveTool(t, "BAG_REAL_SORT", "sort")
	bagBin := buildBagAs(t, "sort")
	Run(t, SortCases(), realBin, bagBin)
}

// TestHexdumpConformance runs the hexdump corpus against system hexdump + bag.
func TestHexdumpConformance(t *testing.T) {
	realBin := resolveTool(t, "BAG_REAL_HEXDUMP", "hexdump")
	bagBin := buildBagAs(t, "hexdump")
	Run(t, HexdumpCases(), realBin, bagBin)
}

func resolveTool(t *testing.T, env, fallback string) string {
	t.Helper()
	if v := os.Getenv(env); v != "" {
		return v
	}
	p, err := exec.LookPath(fallback)
	if err != nil {
		t.Skipf("system %s not on PATH: %v (set %s to override)", fallback, err, env)
	}
	return p
}

// buildBagCurl builds the bag binary and returns a path that, when invoked,
// dispatches to curl. We rename a copy to "curl" so argv[0] matches.
func buildBagCurl(t *testing.T) string { return buildBagAs(t, "curl") }

func buildBagWget(t *testing.T) string { return buildBagAs(t, "wget") }

var sharedBag struct {
	path string
	dir  string
}

func buildBagAs(t *testing.T, name string) string {
	t.Helper()
	if sharedBag.path == "" {
		t.Fatalf("bag binary not built (TestMain missing?)")
	}
	link := filepath.Join(sharedBag.dir, name)
	if _, err := os.Stat(link); err != nil {
		if err := os.Link(sharedBag.path, link); err != nil {
			t.Fatalf("link %s: %v", name, err)
		}
	}
	return link
}

// repoRoot returns the repo root, computed from this file's location.
func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	// .../test/conformance/conformance_test.go -> ../../
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
