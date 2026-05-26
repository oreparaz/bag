package curl

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// run is the top-level entry. It must not call os.Exit.
func run(args []string) int {
	opts, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "curl: %v\n", err)
		fmt.Fprintln(os.Stderr, "curl: try 'curl --help' for more information")
		return exitFailedInit
	}
	if opts.printHelp {
		printHelp(os.Stdout)
		return 0
	}
	if opts.printVersion {
		fmt.Println("curl 8.0.0 (bag) -- bag drop-in")
		return 0
	}

	app := newApp(opts)
	defer app.Close()

	exit := exitOK
	for idx, raw := range opts.URLs {
		code := app.transferOne(idx, raw)
		if code != 0 && exit == 0 {
			exit = code
		}
	}
	return exit
}

// app is the per-invocation state shared across multiple URLs.
type app struct {
	opts   *options
	client *http.Client
	jar    *cookieJar
	stdout io.Writer
	stderr io.Writer
}

func newApp(o *options) *app {
	a := &app{opts: o, stdout: os.Stdout, stderr: os.Stderr}
	a.jar = newCookieJar()
	if o.CookieIn != "" {
		// Scope inline -b cookies to the first URL's host so a redirect
		// to an attacker-controlled host doesn't receive them.
		var origin *url.URL
		if len(o.URLs) > 0 {
			if u, err := normalizeURL(o.URLs[0]); err == nil {
				origin = u
			}
		}
		_ = a.jar.LoadInput(o.CookieIn, origin)
	}
	a.client = a.buildClient()
	return a
}

func (a *app) Close() {
	if a.opts.CookieJar != "" {
		_ = a.jar.SaveJar(a.opts.CookieJar)
	}
}

// transferOne handles a single URL. Returns the curl exit code for it.
func (a *app) transferOne(idx int, raw string) int {
	target, err := normalizeURL(raw)
	if err != nil {
		fmt.Fprintf(a.stderr, "curl: (3) URL using bad/illegal format or missing URL\n")
		return exitURLMalformed
	}
	if !isHTTPScheme(target.Scheme) {
		fmt.Fprintf(a.stderr, "curl: (1) Protocol %q not supported\n", target.Scheme)
		return exitUnsupportedURL
	}

	outPath, isStdout := a.outputForIndex(idx, target)

	method, body, contentType, err := a.buildBody()
	if err != nil {
		fmt.Fprintf(a.stderr, "curl: %v\n", err)
		return exitReadError
	}

	// Snapshot the body bytes so retries can re-send it. Without this the
	// reader is drained by attempt 1 and retry attempts silently send an
	// empty body.
	var bodyBytes []byte
	if body != nil {
		bodyBytes, err = io.ReadAll(body)
		if err != nil {
			fmt.Fprintf(a.stderr, "curl: %v\n", err)
			return exitReadError
		}
	}

	// -G turns -d into query string.
	if a.opts.Get && len(a.opts.Data) > 0 {
		qs, err := buildQueryFromData(a.opts.Data)
		if err != nil {
			fmt.Fprintf(a.stderr, "curl: %v\n", err)
			return exitReadError
		}
		if target.RawQuery == "" {
			target.RawQuery = qs
		} else {
			target.RawQuery += "&" + qs
		}
		bodyBytes = nil
		contentType = ""
		method = "GET"
	}
	_ = body // body is replaced per-attempt below

	if a.opts.HeadOnly && method == "" {
		method = "HEAD"
		// curl -I: implicit -i.
		a.opts.IncludeHeaders = true
	} else if a.opts.HeadOnly {
		// -I implies -i (header dump) even when -X overrides the method.
		a.opts.IncludeHeaders = true
	}

	// Compute resume offset for -C / -C -. With -C - we read the existing
	// output file's size; with -C N we use the literal byte offset. The
	// offset becomes a Range header so the server only sends bytes >= off,
	// instead of the previous behaviour where O_APPEND silently turned the
	// full response into duplicate data on disk.
	var resumeOffset int64
	if (a.opts.Continue != 0 || a.opts.ContinueAuto) && !isStdout {
		if a.opts.ContinueAuto {
			if fi, err := os.Stat(outPath); err == nil {
				resumeOffset = fi.Size()
			}
		} else {
			resumeOffset = a.opts.Continue
		}
	}

	url := target.String()
	attempts := a.opts.Retry + 1
	if attempts < 1 {
		attempts = 1
	}

	var lastCode int
	deadline := time.Time{}
	if a.opts.RetryMaxTime > 0 {
		deadline = time.Now().Add(a.opts.RetryMaxTime)
	}

	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			delay := a.opts.RetryDelay
			if !a.opts.retryDelaySet {
				delay = backoff(attempt)
			}
			time.Sleep(delay)
			if !deadline.IsZero() && time.Now().After(deadline) {
				break
			}
		}
		ctx, cancel := requestContext(a.opts.MaxTime)
		var attemptBody io.Reader
		if bodyBytes != nil {
			attemptBody = bytes.NewReader(bodyBytes)
		}
		code, retry := a.doOnce(ctx, method, url, attemptBody, contentType, outPath, isStdout, resumeOffset)
		cancel()
		lastCode = code
		if !retry {
			return code
		}
	}
	return lastCode
}

// doOnce performs a single attempt (no retry, but redirects handled by
// the http.Client). Returns the curl exit code and whether the failure
// is retryable per --retry semantics.
func (a *app) doOnce(ctx context.Context, method, url string, body io.Reader, ct, outPath string, isStdout bool, resumeOffset int64) (int, bool) {
	req, err := buildRequest(ctx, method, url, body, ct, a.opts, a.jar)
	if err != nil {
		fmt.Fprintf(a.stderr, "curl: %v\n", err)
		return exitFailedInit, false
	}
	if resumeOffset > 0 && req.Header.Get("Range") == "" {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", resumeOffset))
	}

	if a.opts.Verbose {
		writeVerboseRequest(a.stderr, req, body != nil)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		var redErr tooManyRedirects
		if errors.As(err, &redErr) {
			fmt.Fprintf(a.stderr, "curl: (47) Maximum (%d) redirects followed\n", a.opts.MaxRedirs)
			return exitTooManyRedirs, false
		}
		code := classify(err)
		if !a.opts.Silent || a.opts.ShowError {
			fmt.Fprintf(a.stderr, "curl: (%d) %v\n", code, sanitizeErr(err))
		}
		return code, isRetryable(code)
	}
	defer resp.Body.Close()

	a.jar.StoreFromResponse(resp)

	if a.opts.Verbose {
		writeVerboseResponse(a.stderr, resp)
	}

	if a.opts.FailOnError && resp.StatusCode >= 400 {
		// curl -f: discard body, return 22.
		_, _ = io.Copy(io.Discard, resp.Body)
		fmt.Fprintf(a.stderr, "curl: (22) The requested URL returned error: %d\n", resp.StatusCode)
		retry := resp.StatusCode >= 500 || resp.StatusCode == 408 || resp.StatusCode == 429
		return exitHTTPReturned, retry
	}

	w, closer, err := a.openOutput(outPath, isStdout, resp)
	if err != nil {
		fmt.Fprintf(a.stderr, "curl: %v\n", err)
		return exitWriteError, false
	}
	defer closer()

	if a.opts.IncludeHeaders && !isStdout {
		writeStatusAndHeaders(w, resp)
	} else if a.opts.IncludeHeaders {
		writeStatusAndHeaders(w, resp)
	}

	body2 := decodeBody(resp, a.opts.Compressed)
	n, err := io.Copy(w, body2)
	if err != nil {
		fmt.Fprintf(a.stderr, "curl: (%d) %v\n", exitRecvError, sanitizeErr(err))
		return exitRecvError, true
	}

	if a.opts.WriteOut != "" {
		// curl writes -w output to stdout (regardless of -o / file output),
		// not stderr. Tests of -w live alongside -o /dev/null.
		writeWriteOut(a.stdout, a.opts.WriteOut, resp, n)
	}
	return exitOK, false
}

// isRetryable: --retry retries transient network/server failures only.
func isRetryable(code int) bool {
	switch code {
	case exitCouldntResolve, exitCouldntConnect, exitOperationTimeout,
		exitRecvError, exitSendError:
		return true
	}
	return false
}

func backoff(attempt int) time.Duration {
	d := time.Duration(1<<attempt) * time.Second
	if d > 10*time.Minute {
		d = 10 * time.Minute
	}
	return d
}

func requestContext(maxTime time.Duration) (context.Context, context.CancelFunc) {
	if maxTime > 0 {
		return context.WithTimeout(context.Background(), maxTime)
	}
	return context.WithCancel(context.Background())
}

// sanitizeErr trims redundant "Get \"url\":" prefix net/http adds.
func sanitizeErr(err error) string {
	s := err.Error()
	for _, p := range []string{"Get ", "Post ", "Head ", "Put ", "Delete "} {
		if strings.HasPrefix(s, p) {
			if i := strings.Index(s, "\": "); i > 0 {
				return s[i+3:]
			}
		}
	}
	return s
}

func writeStatusAndHeaders(w io.Writer, resp *http.Response) {
	fmt.Fprintf(w, "HTTP/%d.%d %s\r\n", resp.ProtoMajor, resp.ProtoMinor, resp.Status)
	resp.Header.Write(w)
	fmt.Fprint(w, "\r\n")
}
