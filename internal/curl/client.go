package curl

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"github.com/oreparaz/bag/internal/httpx"
)

// tooManyRedirects is wrapped by the redirect policy when -L is set and the
// chain exceeds --max-redirs.
type tooManyRedirects struct{ N int }

func (e tooManyRedirects) Error() string {
	return fmt.Sprintf("stopped after %d redirects", e.N)
}

func (a *app) buildClient() *http.Client {
	tr, err := httpx.Build(httpx.Options{
		Insecure:       a.opts.Insecure,
		ConnectTimeout: a.opts.ConnectTimeout,
		Proxy:          a.opts.Proxy,
		// curl --noproxy '*' bypasses every host: also skip env lookup.
		NoProxyEnv:  a.opts.Proxy == "" && a.opts.NoProxy == "*",
		IPFamily:    ipFamily(a.opts),
		HTTPVersion: a.opts.HTTPVersion,
		CACertFile:  a.opts.CACertFile,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "curl: %v\n", err)
		os.Exit(exitFailedInit)
	}

	c := &http.Client{
		Transport: tr,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if !a.opts.FollowRedirect {
				return http.ErrUseLastResponse
			}
			if a.opts.MaxRedirs >= 0 && len(via) >= a.opts.MaxRedirs {
				return tooManyRedirects{N: a.opts.MaxRedirs}
			}
			// Capture Set-Cookie from the redirect response so flows like
			// "POST /login → 302 + Set-Cookie: session → GET /home" carry
			// the session forward.
			if req.Response != nil {
				a.jar.StoreFromResponse(req.Response)
			}
			// curl strips Authorization on cross-host redirects; net/http
			// already does this for the Authorization header it set itself,
			// but we set headers manually so do it explicitly.
			if len(via) > 0 && req.URL.Host != via[0].URL.Host {
				req.Header.Del("Authorization")
				req.Header.Del("Cookie")
			}
			// Re-apply jar cookies for the new request URL.
			if c := a.jar.HeaderFor(req.URL); c != "" {
				existing := req.Header.Get("Cookie")
				if existing != "" {
					req.Header.Set("Cookie", existing+"; "+c)
				} else {
					req.Header.Set("Cookie", c)
				}
			}
			return nil
		},
	}
	return c
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

// outputForIndex returns the output path for the i-th URL.
//   - if -o or -O were given, use them positionally
//   - else default to stdout
//
// Returns (path, isStdout). path is "" iff isStdout.
func (a *app) outputForIndex(i int, target *url.URL) (string, bool) {
	if i < len(a.opts.OutputPaths) {
		path := a.opts.OutputPaths[i]
		remote := i < len(a.opts.RemoteName) && a.opts.RemoteName[i]
		if remote {
			name := filepath.Base(target.Path)
			if name == "" || name == "/" || name == "." {
				name = "index.html"
			}
			return name, false
		}
		if path == "" || path == "-" {
			return "", true
		}
		return path, false
	}
	if a.opts.remoteNameAll {
		name := filepath.Base(target.Path)
		if name == "" || name == "/" || name == "." {
			name = "index.html"
		}
		return name, false
	}
	return "", true
}

// openOutput returns a writer and a close function. For stdout, the close
// is a no-op. For files, it creates parent dirs if --create-dirs.
//
// Resume / Continue mode (-C / -C -) only switches to append when the
// server actually honored our Range request with 206 Partial Content. If
// the server returned 200 OK (full body), we truncate so we don't end up
// with previous-partial + full duplicated on disk.
func (a *app) openOutput(path string, stdout bool, resp *http.Response) (writer, func(), error) {
	if stdout {
		return a.stdout, func() {}, nil
	}
	if a.opts.CreateDirs {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, nil, err
		}
	}
	flag := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if (a.opts.Continue != 0 || a.opts.ContinueAuto) && resp != nil && resp.StatusCode == http.StatusPartialContent {
		flag = os.O_WRONLY | os.O_APPEND
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			flag = os.O_WRONLY | os.O_CREATE
		}
	}
	f, err := os.OpenFile(path, flag, 0o644)
	if err != nil {
		return nil, nil, err
	}
	return f, func() { _ = f.Close() }, nil
}

type writer interface {
	Write([]byte) (int, error)
}
