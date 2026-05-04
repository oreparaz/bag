package curl

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
)

// buildRequest assembles the *http.Request with all headers, auth, cookies.
func buildRequest(ctx context.Context, method, url string, body io.Reader, ct string, opts *options, jar *cookieJar) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}

	// Default User-Agent.
	if opts.UserAgent != "" {
		req.Header.Set("User-Agent", opts.UserAgent)
	} else {
		req.Header.Set("User-Agent", "curl/8.0.0 (bag)")
	}

	// Default Accept.
	req.Header.Set("Accept", "*/*")

	// Compressed.
	if opts.Compressed {
		req.Header.Set("Accept-Encoding", "gzip, deflate")
	}

	// Body content-type.
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	}

	// Referer.
	if opts.Referer != "" {
		req.Header.Set("Referer", opts.Referer)
	}

	// Range.
	if opts.Range != "" {
		req.Header.Set("Range", "bytes="+opts.Range)
	}

	// User-supplied headers override defaults / can delete (-H "Header:" is delete).
	for _, h := range opts.Headers {
		applyUserHeader(req, h)
	}

	// Basic auth.
	if opts.BasicAuth != "" {
		u, p := splitUserPass(opts.BasicAuth)
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(u+":"+p)))
	}

	// Cookies from -b inline pairs (file form is loaded into jar already).
	if opts.CookieIn != "" && !looksLikeCookieFile(opts.CookieIn) {
		req.Header.Set("Cookie", opts.CookieIn)
	}

	// Cookies from jar.
	if c := jar.HeaderFor(req.URL); c != "" {
		existing := req.Header.Get("Cookie")
		if existing != "" {
			req.Header.Set("Cookie", existing+"; "+c)
		} else {
			req.Header.Set("Cookie", c)
		}
	}

	if body == nil {
		req.Body = http.NoBody
		req.ContentLength = 0
	}

	return req, nil
}

// applyUserHeader handles three -H forms:
//
//	"Name: Value"   set (or replace)
//	"Name:"         delete
//	"Name;"         set empty
func applyUserHeader(req *http.Request, h string) {
	if i := strings.IndexByte(h, ':'); i >= 0 {
		name := strings.TrimSpace(h[:i])
		val := strings.TrimSpace(h[i+1:])
		if val == "" {
			req.Header.Del(name)
			return
		}
		// Special-case Host: net/http requires req.Host
		if strings.EqualFold(name, "Host") {
			req.Host = val
			return
		}
		req.Header.Set(name, val)
		return
	}
	if i := strings.IndexByte(h, ';'); i >= 0 {
		name := strings.TrimSpace(h[:i])
		req.Header.Set(name, "")
	}
}

func splitUserPass(s string) (string, string) {
	if i := strings.IndexByte(s, ':'); i >= 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

func looksLikeCookieFile(s string) bool {
	// Heuristic: contains a path separator or a known cookie-file marker
	// or no '=' (cookie pairs always have '=').
	if strings.ContainsAny(s, "/\\") {
		return true
	}
	if !strings.Contains(s, "=") {
		return true
	}
	return false
}

