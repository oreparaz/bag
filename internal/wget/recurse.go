package wget

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// recurse implements a deliberately shallow `-r`: starting from start, fetch
// the page, save it, extract <a href> / <img src> / <link href> /
// <script src>, recurse on those that are same-host (and below the path
// when --no-parent is set), up to maxDepth levels.
//
// We do NOT rewrite links, do NOT respect robots.txt, and do NOT do
// timestamping. Anything fancy is intentionally deferred (see FUTURE.md).
func (a *app) recurse(start string, maxDepth int) int {
	root, err := normalizeURL(start)
	if err != nil {
		fmt.Fprintf(a.logW, "wget: %v\n", err)
		return exitParse
	}
	if !isHTTPScheme(root.Scheme) {
		return exitProtocol
	}

	visited := map[string]bool{}
	type item struct {
		u     *url.URL
		depth int
	}
	queue := []item{{u: root, depth: 0}}

	exit := exitOK

	for len(queue) > 0 {
		it := queue[0]
		queue = queue[1:]
		key := canonical(it.u)
		if visited[key] {
			continue
		}
		visited[key] = true

		body, code := a.fetchAndCapture(it.u)
		if code != 0 && exit == 0 {
			exit = code
		}
		if it.depth >= maxDepth || body == nil {
			continue
		}

		for _, link := range extractLinks(body) {
			lu, err := it.u.Parse(link)
			if err != nil {
				continue
			}
			lu.Fragment = ""
			if lu.Host != root.Host {
				continue // off-host, skip
			}
			if a.opts.NoParent && !strings.HasPrefix(path.Clean(lu.Path), path.Clean(root.Path)) {
				continue
			}
			queue = append(queue, item{u: lu, depth: it.depth + 1})
		}
	}

	return exit
}

// fetchAndCapture is like fetch but also returns the response body bytes for
// link extraction. It writes to disk in the standard recursive layout.
func (a *app) fetchAndCapture(u *url.URL) ([]byte, int) {
	ctx, cancel := requestContext(a.opts.Timeout, a.opts.ReadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		fmt.Fprintf(a.logW, "wget: %v\n", err)
		return nil, exitGeneric
	}
	setWgetDefaults(req, a.opts)

	if !a.opts.Quiet {
		fmt.Fprintf(a.logW, "--%s--  %s\n", time.Now().Format("2006-01-02 15:04:05"), u.String())
	}

	resp, err := a.client.Do(req)
	if err != nil {
		fmt.Fprintf(a.logW, "wget: %v\n", err)
		return nil, classify(err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(a.logW, "wget: %v\n", err)
		return nil, exitNetwork
	}

	if resp.StatusCode >= 400 {
		fmt.Fprintf(a.logW, "%d %s\n", resp.StatusCode, http.StatusText(resp.StatusCode))
		return nil, exitServerErr
	}

	// File layout: host/<path>, defaulting index.html for trailing slash.
	rel := u.Path
	if rel == "" || strings.HasSuffix(rel, "/") {
		rel = strings.TrimSuffix(rel, "/") + "/index.html"
	}
	// Defense in depth: reject server-controlled paths that contain ".."
	// segments. filepath.Join would resolve them and could land us outside
	// the intended output tree (e.g. a redirect to /../../../etc/passwd).
	if hasParentSegment(rel) {
		fmt.Fprintf(a.logW, "wget: refusing to write outside output dir: %q\n", rel)
		return body, exitFileIO
	}
	out := filepath.Join(a.applyDirPrefix(""), u.Host, rel)
	if a.opts.NoDirectories {
		out = a.applyDirPrefix(filepath.Base(rel))
	}

	if a.opts.NoClobber {
		if _, err := os.Stat(out); err == nil {
			return body, exitOK
		}
	}

	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		fmt.Fprintf(a.logW, "wget: %v\n", err)
		return body, exitFileIO
	}
	if err := os.WriteFile(out, body, 0o644); err != nil {
		fmt.Fprintf(a.logW, "wget: %v\n", err)
		return body, exitFileIO
	}
	return body, exitOK
}

// hasParentSegment reports whether p contains a ".." segment (after splitting
// by '/'). Used to refuse server-controlled paths that could escape the
// output directory.
func hasParentSegment(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

func canonical(u *url.URL) string {
	c := *u
	c.Fragment = ""
	return c.String()
}

// linkRE pulls href= and src= attribute values out of HTML. This is a regex,
// not a parser, so it intentionally fails on edge-case HTML — the recursive
// mode is documented as shallow.
var linkRE = regexp.MustCompile(`(?i)(?:href|src)\s*=\s*"([^"]+)"|(?:href|src)\s*=\s*'([^']+)'`)

func extractLinks(body []byte) []string {
	matches := linkRE.FindAllSubmatch(body, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		v := ""
		if len(m[1]) > 0 {
			v = string(m[1])
		} else if len(m[2]) > 0 {
			v = string(m[2])
		}
		v = strings.TrimSpace(v)
		if v == "" || strings.HasPrefix(v, "#") || strings.HasPrefix(v, "javascript:") || strings.HasPrefix(v, "mailto:") {
			continue
		}
		out = append(out, v)
	}
	return out
}
