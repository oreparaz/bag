package wget

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oreparaz/bag/internal/httpx"
)

// run is the wget entry point. Must not call os.Exit.
func run(args []string) int {
	opts, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wget: %v\n", err)
		return exitParse
	}
	if opts.printHelp {
		printHelp(os.Stdout)
		return 0
	}
	if opts.printVersion {
		fmt.Println("GNU Wget 1.21.4 (bag) -- bag drop-in")
		return 0
	}

	urls, err := collectURLs(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wget: %v\n", err)
		return exitFileIO
	}

	app, err := newApp(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wget: %v\n", err)
		return exitGeneric
	}
	defer app.close()

	exit := exitOK
	if opts.Recursive {
		for _, u := range urls {
			if code := app.recurse(u, opts.Level); code != 0 && exit == 0 {
				exit = code
			}
		}
		return exit
	}

	for _, raw := range urls {
		if code := app.fetch(raw); code != 0 && exit == 0 {
			exit = code
		}
	}
	return exit
}

func collectURLs(o *options) ([]string, error) {
	urls := append([]string{}, o.URLs...)
	if o.InputFile != "" {
		f, err := os.Open(o.InputFile)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			urls = append(urls, line)
		}
		if err := sc.Err(); err != nil {
			return nil, err
		}
	}
	return urls, nil
}

type app struct {
	opts   *options
	client *http.Client
	logW   io.Writer
	closer func()
}

func newApp(o *options) (*app, error) {
	tr, err := httpx.Build(httpx.Options{
		Insecure:       o.NoCheckCert,
		ConnectTimeout: chooseTimeout(o.ConnectTimeout, o.Timeout),
		Proxy:          o.Proxy,
		NoProxyEnv:     o.NoProxy,
		IPFamily:       ipFamily(o),
		CACertFile:     o.CACertFile,
	})
	if err != nil {
		return nil, err
	}

	c := &http.Client{
		Transport: tr,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= o.MaxRedirect {
				return fmt.Errorf("%d redirects exceeded", o.MaxRedirect)
			}
			// Strip credentials when the redirect crosses to a different
			// host. This prevents a malicious server returning Location:
			// https://attacker/ from harvesting our --user/--password
			// (set as Authorization) or any --header=Cookie value.
			if len(via) > 0 && req.URL.Host != via[0].URL.Host {
				req.Header.Del("Authorization")
				req.Header.Del("Cookie")
			}
			return nil
		},
	}

	a := &app{opts: o, client: c, logW: os.Stderr, closer: func() {}}
	if o.LogFile != "" || o.AppendLog != "" {
		flag := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
		path := o.LogFile
		if o.AppendLog != "" {
			flag = os.O_WRONLY | os.O_CREATE | os.O_APPEND
			path = o.AppendLog
		}
		f, err := os.OpenFile(path, flag, 0o644)
		if err != nil {
			return nil, err
		}
		a.logW = f
		a.closer = func() { f.Close() }
	}
	return a, nil
}

func (a *app) close() { a.closer() }

func chooseTimeout(specific, fallback time.Duration) time.Duration {
	if specific > 0 {
		return specific
	}
	return fallback
}

func ipFamily(o *options) string {
	switch {
	case o.IPv4Only && !o.IPv6Only:
		return "4"
	case o.IPv6Only && !o.IPv4Only:
		return "6"
	}
	return ""
}

// fetch downloads one URL.
func (a *app) fetch(raw string) int {
	target, err := normalizeURL(raw)
	if err != nil {
		fmt.Fprintf(a.logW, "wget: %v\n", err)
		return exitParse
	}
	if !isHTTPScheme(target.Scheme) {
		fmt.Fprintf(a.logW, "wget: unsupported scheme %q\n", target.Scheme)
		return exitProtocol
	}

	outPath, useStdout, err := a.outputPath(target)
	if err != nil {
		fmt.Fprintf(a.logW, "wget: %v\n", err)
		return exitFileIO
	}

	tries := a.opts.Tries
	if tries < 1 {
		tries = 1
	}
	var lastCode int
	for attempt := 1; attempt <= tries; attempt++ {
		if attempt > 1 {
			delay := a.opts.WaitRetry
			if delay == 0 {
				delay = backoff(attempt)
			}
			time.Sleep(delay)
		}
		code, retry := a.doOnce(target, outPath, useStdout)
		lastCode = code
		if !retry {
			return code
		}
	}
	return lastCode
}

func (a *app) doOnce(target *url.URL, outPath string, useStdout bool) (int, bool) {
	ctx, cancel := requestContext(a.opts.Timeout, a.opts.ReadTimeout)
	defer cancel()

	method := a.opts.Method
	if method == "" {
		method = "GET"
	}
	var body io.Reader
	if a.opts.PostDataSet {
		body = strings.NewReader(a.opts.PostData)
	}
	req, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		fmt.Fprintf(a.logW, "wget: %v\n", err)
		return exitGeneric, false
	}
	if a.opts.PostDataSet && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	setWgetDefaults(req, a.opts)
	for _, h := range a.opts.Headers {
		applyHeader(req, h)
	}
	if a.opts.User != "" {
		req.SetBasicAuth(a.opts.User, a.opts.Password)
	}

	// Continue support: HEAD first to find existing size.
	if a.opts.Continue && !useStdout {
		if existing, err := os.Stat(outPath); err == nil {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", existing.Size()))
		}
	}

	if !a.opts.Quiet {
		fmt.Fprintf(a.logW, "--%s--  %s\n", time.Now().Format("2006-01-02 15:04:05"), target.String())
	}

	resp, err := a.client.Do(req)
	if err != nil {
		code := classify(err)
		fmt.Fprintf(a.logW, "wget: %v\n", err)
		return code, isRetryable(code)
	}
	defer resp.Body.Close()

	if a.opts.ServerResponse {
		fmt.Fprintf(a.logW, "  HTTP/%d.%d %s\n", resp.ProtoMajor, resp.ProtoMinor, resp.Status)
		for k, vs := range resp.Header {
			for _, v := range vs {
				fmt.Fprintf(a.logW, "  %s: %s\n", k, v)
			}
		}
	}

	if resp.StatusCode >= 400 {
		fmt.Fprintf(a.logW, "%d %s\n", resp.StatusCode, http.StatusText(resp.StatusCode))
		retry := resp.StatusCode == 429 || resp.StatusCode == 408 || resp.StatusCode >= 500
		switch {
		case resp.StatusCode == 401 || resp.StatusCode == 403:
			return exitAuthFail, retry
		default:
			return exitServerErr, retry
		}
	}

	// Content-Disposition handling overrides default filename.
	if a.opts.ContentDisposition && !useStdout && !a.opts.OutputDocumentSet {
		if name := dispositionFilename(resp.Header.Get("Content-Disposition")); name != "" {
			outPath = a.applyDirPrefix(name)
		}
	}

	w, finish, err := a.openOutput(outPath, useStdout, resp.StatusCode == http.StatusPartialContent)
	if err != nil {
		fmt.Fprintf(a.logW, "wget: %v\n", err)
		return exitFileIO, false
	}
	defer finish()

	if _, err := io.Copy(w, resp.Body); err != nil {
		fmt.Fprintf(a.logW, "wget: %v\n", err)
		return exitNetwork, true
	}

	if !a.opts.Quiet {
		fmt.Fprintf(a.logW, "%s saved\n", outPath)
	}
	return exitOK, false
}

// setWgetDefaults installs the headers wget always sends. Matching them
// exactly keeps server-side logs and middleware indistinguishable from
// real wget.
func setWgetDefaults(req *http.Request, o *options) {
	ua := o.UserAgent
	if ua == "" {
		ua = "Wget/1.21.4"
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("Connection", "Keep-Alive")
	if o.Referer != "" {
		req.Header.Set("Referer", o.Referer)
	}
}

func applyHeader(req *http.Request, h string) {
	if i := strings.IndexByte(h, ':'); i >= 0 {
		name := strings.TrimSpace(h[:i])
		val := strings.TrimSpace(h[i+1:])
		if strings.EqualFold(name, "Host") {
			req.Host = val
			return
		}
		if val == "" {
			req.Header.Del(name)
			return
		}
		req.Header.Set(name, val)
	}
}

func backoff(attempt int) time.Duration {
	d := time.Duration(1<<uint(attempt-1)) * time.Second
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	return d
}

func isRetryable(code int) bool {
	return code == exitNetwork
}

func requestContext(total, read time.Duration) (context.Context, context.CancelFunc) {
	d := total
	if d == 0 {
		d = read
	}
	if d > 0 {
		return context.WithTimeout(context.Background(), d)
	}
	return context.WithCancel(context.Background())
}

// outputPath decides where the body for target should be written.
//
// Rules (matching wget):
//   - If -O was given:
//       - "-" or "" empty: stdout
//       - otherwise: that exact path. -O concatenates all responses.
//   - Else:
//       - basename(URL.Path), or "index.html" if empty
//       - prefixed with -P if set
//       - if --no-clobber and exists, return error to skip
//       - otherwise add ".N" suffix to avoid clobber (default behavior)
func (a *app) outputPath(u *url.URL) (string, bool, error) {
	if a.opts.OutputDocumentSet {
		if a.opts.OutputDocument == "" || a.opts.OutputDocument == "-" {
			return "", true, nil
		}
		return a.opts.OutputDocument, false, nil
	}

	name := filepath.Base(u.Path)
	if name == "" || name == "/" || name == "." {
		name = "index.html"
	}
	full := a.applyDirPrefix(name)

	if a.opts.NoClobber {
		if _, err := os.Stat(full); err == nil {
			return "", false, fmt.Errorf("file %q already exists -- not overwriting", full)
		}
		return full, false, nil
	}

	// Default wget: don't clobber, but add .1, .2, ...
	if _, err := os.Stat(full); err == nil && !a.opts.Continue {
		for i := 1; i < 10000; i++ {
			cand := fmt.Sprintf("%s.%d", full, i)
			if _, err := os.Stat(cand); errors.Is(err, os.ErrNotExist) {
				return cand, false, nil
			}
		}
	}
	return full, false, nil
}

func (a *app) applyDirPrefix(name string) string {
	if a.opts.DirPrefix == "" {
		return name
	}
	return filepath.Join(a.opts.DirPrefix, name)
}

func (a *app) openOutput(path string, useStdout, partial bool) (io.Writer, func(), error) {
	if useStdout {
		return os.Stdout, func() {}, nil
	}
	if a.opts.DirPrefix != "" {
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
	}
	flag := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if a.opts.Continue && partial {
		flag = os.O_WRONLY | os.O_APPEND
	}
	if a.opts.OutputDocumentSet {
		// -O concatenates across multiple URLs: append after first write.
		if _, err := os.Stat(path); err == nil && a.opts.OutputDocument != "/dev/null" {
			flag = os.O_WRONLY | os.O_APPEND
		}
	}
	f, err := os.OpenFile(path, flag, 0o644)
	if err != nil {
		return nil, nil, err
	}
	return f, func() { _ = f.Close() }, nil
}

// dispositionFilename pulls "filename" out of a Content-Disposition header.
// We accept both the simple `filename=name` and the RFC 5987 `filename*=`
// extended form (UTF-8 only).
func dispositionFilename(h string) string {
	if h == "" {
		return ""
	}
	parts := strings.Split(h, ";")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "filename*=") {
			v := strings.TrimPrefix(p, "filename*=")
			// "UTF-8''<encoded>"
			if i := strings.Index(v, "''"); i >= 0 {
				return filepath.Base(v[i+2:])
			}
			return filepath.Base(v)
		}
		if strings.HasPrefix(p, "filename=") {
			v := strings.TrimPrefix(p, "filename=")
			v = strings.Trim(v, `"`)
			return filepath.Base(v)
		}
	}
	return ""
}

func printHelp(w io.Writer) {
	const help = `Usage: wget [OPTION]... [URL]...
Common options:
  -O, --output-document=FILE     write documents to FILE ('-' for stdout)
  -o, --output-file=FILE         log messages to FILE
  -P, --directory-prefix=PREFIX  save files to PREFIX/...
  -i, --input-file=FILE          read URLs from FILE
  -q, --quiet                    quiet mode (no output)
  -v, --verbose                  be verbose (default)
  -nv, --no-verbose              turn off verbose, still not quiet
  -t, --tries=N                  set number of retries to N (default 20)
  -T, --timeout=SECS             set all timeouts to SECS
      --connect-timeout=SECS     set connect-only timeout
      --read-timeout=SECS        set read-only timeout
  -c, --continue                 resume getting a partially-downloaded file
  -nc, --no-clobber              skip downloads that would overwrite
      --max-redirect=N           cap redirects (default 20)
  -U, --user-agent=AGENT         identify as AGENT to the server
      --user=USER                set HTTP user
      --password=PASS            set HTTP password
      --no-check-certificate     don't validate server certificate
      --ca-certificate=FILE      file with the bundle of CA's
  -r, --recursive                follow links recursively (shallow)
  -l, --level=N                  recursion depth (default 5)
  -np, --no-parent               don't ascend to parent directory
  -S, --server-response          print server response
  -4, --inet4-only               connect only to IPv4
  -6, --inet6-only               connect only to IPv6
      --content-disposition      honor Content-Disposition filename
      --header=STRING            send STRING among the headers

For full documentation see https://github.com/oreparaz/bag
`
	io.WriteString(w, strings.TrimLeft(help, "\n"))
}
