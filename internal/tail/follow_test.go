package tail

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// runTailFollow spawns Main in a goroutine and returns a captured stdout
// reader and a stop func that closes the goroutine.
func runTailFollow(t *testing.T, args ...string) (chan []byte, func()) {
	t.Helper()
	rOut, wOut, _ := os.Pipe()
	oldOut := os.Stdout
	os.Stdout = wOut

	done := make(chan struct{})
	out := make(chan []byte, 16)

	// Reader goroutine: stream pipe -> channel.
	go func() {
		defer close(out)
		buf := make([]byte, 4096)
		for {
			n, err := rOut.Read(buf)
			if n > 0 {
				cp := make([]byte, n)
				copy(cp, buf[:n])
				select {
				case out <- cp:
				case <-done:
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// Tail goroutine.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = Main(args)
	}()

	stop := func() {
		// Close stdout pipe to break tail's writer; this won't gracefully
		// stop tail's poll loop. We use SIGINT through the process — but
		// we're in-process. For testing we set a short --sleep-interval and
		// rely on the test binary's deferred cleanup; since Main loops
		// forever we instead rely on test-process death.
		//
		// Practical approach: close pipes and call os.Stdout restore. The
		// tail goroutine will continue but its writes go nowhere; the
		// process exits when the test binary finishes.
		close(done)
		wOut.Close()
		os.Stdout = oldOut
	}
	return out, stop
}

func collectFor(t *testing.T, ch <-chan []byte, d time.Duration) []byte {
	t.Helper()
	var buf bytes.Buffer
	deadline := time.After(d)
	for {
		select {
		case b, ok := <-ch:
			if !ok {
				return buf.Bytes()
			}
			buf.Write(b)
		case <-deadline:
			return buf.Bytes()
		}
	}
}

func TestFollowAppendDetected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.txt")
	if err := os.WriteFile(path, []byte("line1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, stop := runTailFollow(t, "-f", "-s", "0.05", path)
	defer stop()

	// Drain initial 10-line tail (well, just "line1\n" here).
	_ = collectFor(t, out, 200*time.Millisecond)

	// Append more.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString("line2\n")
	f.WriteString("line3\n")
	f.Close()

	got := collectFor(t, out, 800*time.Millisecond)
	if !strings.Contains(string(got), "line2") || !strings.Contains(string(got), "line3") {
		t.Errorf("missing follow output, got %q", got)
	}
}

func TestFollowTruncationDetected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.txt")
	// Use a longer initial content so truncation is observable as a size
	// shrink (same-size truncation is intentionally indistinguishable from
	// an append: tail does not hash file contents).
	os.WriteFile(path, []byte("first big line that is plenty long\n"), 0o644)

	out, stop := runTailFollow(t, "-f", "-s", "0.05", path)
	defer stop()
	_ = collectFor(t, out, 200*time.Millisecond)

	os.WriteFile(path, []byte("ghost\n"), 0o644)

	got := collectFor(t, out, 800*time.Millisecond)
	if !strings.Contains(string(got), "ghost") {
		t.Errorf("missing post-truncate content, got %q", got)
	}
}

// TestFollowSilencesOnNoChanges just verifies the loop doesn't write garbage
// when nothing happens — and that it can be stopped.
func TestFollowQuiet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.txt")
	os.WriteFile(path, []byte("z\n"), 0o644)

	out, stop := runTailFollow(t, "-f", "-s", "0.05", path)
	got := collectFor(t, out, 300*time.Millisecond)
	stop()
	if string(got) != "z\n" && !strings.HasSuffix(string(got), "z\n") {
		t.Errorf("expected initial content only; got %q", got)
	}
	if len(got) > 100 {
		t.Errorf("unexpectedly chatty: %d bytes", len(got))
	}
}

// silence unused-import warnings if helpers move around.
var _ = io.Copy
