package curl

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oreparaz/bag/test/testserver"
)

func mustStartServer(t *testing.T) *testserver.Servers {
	t.Helper()
	srv, err := testserver.Start()
	if err != nil {
		t.Fatalf("starting test server: %v", err)
	}
	t.Cleanup(srv.Close)
	return srv
}

// runCurl runs the in-process curl with args. stdout is captured into the
// returned bytes; stderr is sent to the test log. exit is returned.
func runCurl(t *testing.T, args ...string) (int, []byte) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	old := os.Stdout
	os.Stdout = w
	exit := Main(args)
	w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	return exit, out
}

func TestSimpleGET(t *testing.T) {
	srv := mustStartServer(t)
	exit, out := runCurl(t, "-s", srv.HTTP.URL+"/ok")
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if string(out) != "ok\n" {
		t.Fatalf("body=%q", out)
	}
}

func TestUserAgent(t *testing.T) {
	srv := mustStartServer(t)
	exit, out := runCurl(t, "-s", "-A", "myUA/1.0", srv.HTTP.URL+"/headers")
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	var resp struct {
		Headers map[string]string `json:"headers"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("json: %v body=%q", err, out)
	}
	if resp.Headers["User-Agent"] != "myUA/1.0" {
		t.Fatalf("ua=%q", resp.Headers["User-Agent"])
	}
}

func TestExplicitHeader(t *testing.T) {
	srv := mustStartServer(t)
	exit, out := runCurl(t, "-s", "-H", "X-Foo: bar", "-H", "Accept: text/plain", srv.HTTP.URL+"/headers")
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	var resp struct {
		Headers map[string]string `json:"headers"`
	}
	_ = json.Unmarshal(out, &resp)
	if resp.Headers["X-Foo"] != "bar" {
		t.Fatalf("X-Foo=%q", resp.Headers["X-Foo"])
	}
	if resp.Headers["Accept"] != "text/plain" {
		t.Fatalf("Accept=%q", resp.Headers["Accept"])
	}
}

func TestPOSTData(t *testing.T) {
	srv := mustStartServer(t)
	exit, out := runCurl(t, "-s", "-d", "name=alice&age=30", srv.HTTP.URL+"/echo")
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	var r struct {
		Method  string            `json:"method"`
		Headers map[string]string `json:"headers"`
		Body    string            `json:"body"`
	}
	_ = json.Unmarshal(out, &r)
	if r.Method != "POST" {
		t.Errorf("method=%q want POST", r.Method)
	}
	if r.Body != "name=alice&age=30" {
		t.Errorf("body=%q", r.Body)
	}
	if r.Headers["Content-Type"] != "application/x-www-form-urlencoded" {
		t.Errorf("ct=%q", r.Headers["Content-Type"])
	}
}

func TestDataMultipleChunks(t *testing.T) {
	srv := mustStartServer(t)
	_, out := runCurl(t, "-s", "-d", "a=1", "-d", "b=2", srv.HTTP.URL+"/echo")
	var r struct {
		Body string `json:"body"`
	}
	_ = json.Unmarshal(out, &r)
	if r.Body != "a=1&b=2" {
		t.Errorf("body=%q", r.Body)
	}
}

func TestDataAtFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "in.txt")
	os.WriteFile(p, []byte("hello\nworld\n"), 0o644)
	srv := mustStartServer(t)

	// Default -d strips newlines.
	_, out := runCurl(t, "-s", "-d", "@"+p, srv.HTTP.URL+"/echo")
	var r struct {
		Body string `json:"body"`
	}
	_ = json.Unmarshal(out, &r)
	if r.Body != "helloworld" {
		t.Errorf("dataASCII body=%q", r.Body)
	}

	// --data-binary preserves newlines.
	_, out = runCurl(t, "-s", "--data-binary", "@"+p, srv.HTTP.URL+"/echo")
	_ = json.Unmarshal(out, &r)
	if r.Body != "hello\nworld\n" {
		t.Errorf("data-binary body=%q", r.Body)
	}
}

func TestDataURLEncode(t *testing.T) {
	srv := mustStartServer(t)
	_, out := runCurl(t, "-s", "--data-urlencode", "msg=hello world", srv.HTTP.URL+"/echo")
	var r struct {
		Body string `json:"body"`
	}
	_ = json.Unmarshal(out, &r)
	// space encoded as %20 by url.QueryEscape becomes "+"; query escape uses '+'.
	if r.Body != "msg=hello+world" {
		t.Errorf("body=%q", r.Body)
	}
}

func TestGetWithDataPutsInQueryString(t *testing.T) {
	srv := mustStartServer(t)
	_, out := runCurl(t, "-s", "-G", "-d", "k=v&x=y", srv.HTTP.URL+"/echo")
	var r struct {
		Method string `json:"method"`
		Query  string `json:"query"`
	}
	_ = json.Unmarshal(out, &r)
	if r.Method != "GET" {
		t.Errorf("method=%q", r.Method)
	}
	if r.Query != "k=v&x=y" {
		t.Errorf("query=%q", r.Query)
	}
}

func TestExplicitMethod(t *testing.T) {
	srv := mustStartServer(t)
	_, out := runCurl(t, "-s", "-X", "DELETE", srv.HTTP.URL+"/echo")
	var r struct {
		Method string `json:"method"`
	}
	_ = json.Unmarshal(out, &r)
	if r.Method != "DELETE" {
		t.Errorf("method=%q", r.Method)
	}
}

func TestHEAD(t *testing.T) {
	srv := mustStartServer(t)
	exit, out := runCurl(t, "-s", "-I", srv.HTTP.URL+"/ok")
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if !bytes.HasPrefix(out, []byte("HTTP/")) {
		t.Errorf("expected status line, got %q", out)
	}
}

func TestIncludeHeaders(t *testing.T) {
	srv := mustStartServer(t)
	exit, out := runCurl(t, "-s", "-i", srv.HTTP.URL+"/ok")
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if !bytes.Contains(out, []byte("\r\nok\n")) {
		t.Errorf("expected headers + body separator, got %q", out)
	}
}

func TestRedirectFollow(t *testing.T) {
	srv := mustStartServer(t)
	exit, out := runCurl(t, "-s", "-L", srv.HTTP.URL+"/redirect/3")
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if string(out) != "ok\n" {
		t.Fatalf("body=%q", out)
	}
}

func TestRedirectNoFollow(t *testing.T) {
	srv := mustStartServer(t)
	exit, _ := runCurl(t, "-s", "-i", srv.HTTP.URL+"/redirect/1")
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
}

func TestRedirectMaxRedirs(t *testing.T) {
	srv := mustStartServer(t)
	exit, _ := runCurl(t, "-s", "-L", "--max-redirs", "2", srv.HTTP.URL+"/redirect/5")
	if exit != exitTooManyRedirs {
		t.Errorf("exit=%d want %d", exit, exitTooManyRedirs)
	}
}

func TestOutputFile(t *testing.T) {
	srv := mustStartServer(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "out.txt")
	exit, _ := runCurl(t, "-s", "-o", p, srv.HTTP.URL+"/ok")
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	body, _ := os.ReadFile(p)
	if string(body) != "ok\n" {
		t.Fatalf("body=%q", body)
	}
}

func TestRemoteName(t *testing.T) {
	srv := mustStartServer(t)
	dir := t.TempDir()
	old, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(old)
	exit, _ := runCurl(t, "-s", "-O", srv.HTTP.URL+"/ok")
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	body, err := os.ReadFile("ok")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(body) != "ok\n" {
		t.Fatalf("body=%q", body)
	}
}

func TestBasicAuth(t *testing.T) {
	srv := mustStartServer(t)
	exit, out := runCurl(t, "-s", "-u", "alice:secret", srv.HTTP.URL+"/basic-auth/alice/secret")
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if string(out) != "auth ok\n" {
		t.Fatalf("body=%q", out)
	}
}

func TestBasicAuthWrong(t *testing.T) {
	srv := mustStartServer(t)
	exit, _ := runCurl(t, "-s", "-u", "alice:nope", "-f", srv.HTTP.URL+"/basic-auth/alice/secret")
	if exit != exitHTTPReturned {
		t.Errorf("exit=%d want %d", exit, exitHTTPReturned)
	}
}

func TestFailOnError(t *testing.T) {
	srv := mustStartServer(t)
	exit, _ := runCurl(t, "-sf", srv.HTTP.URL+"/status/404")
	if exit != exitHTTPReturned {
		t.Errorf("exit=%d want %d", exit, exitHTTPReturned)
	}
	exit, _ = runCurl(t, "-s", srv.HTTP.URL+"/status/404")
	if exit != 0 {
		t.Errorf("exit without -f=%d want 0", exit)
	}
}

func TestCompressed(t *testing.T) {
	srv := mustStartServer(t)
	exit, out := runCurl(t, "-s", "--compressed", srv.HTTP.URL+"/gzip")
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if !bytes.Contains(out, []byte(`"gzipped":true`)) {
		t.Fatalf("decompressed body unexpected: %q", out)
	}
}

func TestTLSInsecure(t *testing.T) {
	srv := mustStartServer(t)
	exit, out := runCurl(t, "-s", "-k", srv.HTTPS.URL+"/ok")
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if string(out) != "ok\n" {
		t.Fatalf("body=%q", out)
	}
}

func TestTLSDefaultRejectsSelfSigned(t *testing.T) {
	srv := mustStartServer(t)
	exit, _ := runCurl(t, "-s", srv.HTTPS.URL+"/ok")
	if exit != exitSSLCACertBad {
		t.Errorf("exit=%d want %d", exit, exitSSLCACertBad)
	}
}

func TestTLSWithCAFile(t *testing.T) {
	srv := mustStartServer(t)
	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.pem")
	os.WriteFile(caPath, srv.CACertPEM, 0o644)
	exit, out := runCurl(t, "-s", "--cacert", caPath, srv.HTTPS.URL+"/ok")
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if string(out) != "ok\n" {
		t.Fatalf("body=%q", out)
	}
}

func TestCookieJarRoundTrip(t *testing.T) {
	srv := mustStartServer(t)
	dir := t.TempDir()
	jar := filepath.Join(dir, "jar.txt")

	exit, _ := runCurl(t, "-s", "-c", jar, srv.HTTP.URL+"/cookies/set?session=abc")
	if exit != 0 {
		t.Fatalf("set exit=%d", exit)
	}
	if _, err := os.Stat(jar); err != nil {
		t.Fatalf("jar not written: %v", err)
	}

	exit, out := runCurl(t, "-s", "-b", jar, srv.HTTP.URL+"/cookies")
	if exit != 0 {
		t.Fatalf("read exit=%d", exit)
	}
	if !strings.Contains(string(out), `"session":"abc"`) {
		t.Errorf("cookies echoed=%q", out)
	}
}

func TestInlineCookies(t *testing.T) {
	srv := mustStartServer(t)
	_, out := runCurl(t, "-s", "-b", "k=v; x=y", srv.HTTP.URL+"/cookies")
	if !strings.Contains(string(out), `"k":"v"`) || !strings.Contains(string(out), `"x":"y"`) {
		t.Errorf("cookies=%q", out)
	}
}

func TestMaxTime(t *testing.T) {
	srv := mustStartServer(t)
	exit, _ := runCurl(t, "-s", "--max-time", "0.1", srv.HTTP.URL+"/slow?ms=1000")
	if exit != exitOperationTimeout {
		t.Errorf("exit=%d want %d", exit, exitOperationTimeout)
	}
}

func TestWriteOut(t *testing.T) {
	srv := mustStartServer(t)
	r, w, _ := os.Pipe()
	oldOut := os.Stdout
	os.Stdout = w
	exit := Main([]string{"-s", "-o", os.DevNull, "-w", "%{http_code}\\n", srv.HTTP.URL + "/status/418"})
	w.Close()
	os.Stdout = oldOut
	out, _ := io.ReadAll(r)
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if string(out) != "418\n" {
		t.Errorf("write-out=%q", out)
	}
}

func TestRangeHeader(t *testing.T) {
	srv := mustStartServer(t)
	_, out := runCurl(t, "-s", "-r", "5-9", srv.HTTP.URL+"/range")
	if string(out) != "56789" {
		t.Errorf("range body=%q", out)
	}
}

func TestUnknownFlag(t *testing.T) {
	exit, _ := runCurl(t, "--no-such-flag", "http://example.com")
	if exit == 0 {
		t.Errorf("expected non-zero exit on unknown flag")
	}
}

func TestNoURL(t *testing.T) {
	exit, _ := runCurl(t, "-s")
	if exit == 0 {
		t.Errorf("expected non-zero exit when no URL given")
	}
}

// TestCookieJarRefusesCRLFInjection: a malicious cookies.txt where the
// value contains a bare CR (which bufio's line scanner would NOT split
// on) must be dropped. Otherwise HeaderFor would emit
// "Cookie: name=val\rX-Pwn: yes" — header smuggling.
func TestCookieJarRefusesCRLFInjection(t *testing.T) {
	dir := t.TempDir()
	jar := filepath.Join(dir, "evil-jar.txt")
	body := "# Netscape HTTP Cookie File\n" +
		"127.0.0.1\tFALSE\t/\tFALSE\t0\tname\tval\rX-Pwn: yes\n"
	if err := os.WriteFile(jar, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := mustStartServer(t)
	exit, out := runCurl(t, "-s", "-b", jar, srv.HTTP.URL+"/cookies")
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	// The poisoned cookie should be dropped — server sees no cookies.
	if strings.Contains(string(out), "X-Pwn") {
		t.Errorf("smuggled header into request: %q", out)
	}
	// And the cookie's "name" key shouldn't have made it through, since
	// its value carried unsafe bytes.
	if strings.Contains(string(out), `"name"`) {
		t.Errorf("dangerous cookie was loaded: %q", out)
	}
}

func TestRetryOnConnectFailure(t *testing.T) {
	// Bind a TCP listener and immediately close it. Connection refused.
	exit, _ := runCurl(t, "-s", "--retry", "2", "--retry-delay", "0", "http://127.0.0.1:1")
	if exit != exitCouldntConnect {
		t.Errorf("exit=%d want %d", exit, exitCouldntConnect)
	}
}

func TestUnsupportedProtocol(t *testing.T) {
	exit, _ := runCurl(t, "-s", "ftp://example.com/")
	if exit != exitUnsupportedURL {
		t.Errorf("exit=%d want %d", exit, exitUnsupportedURL)
	}
}

func TestVerboseEmitsToStderr(t *testing.T) {
	srv := mustStartServer(t)
	r, w, _ := os.Pipe()
	oldErr := os.Stderr
	os.Stderr = w
	exit := Main([]string{"-v", "-o", os.DevNull, srv.HTTP.URL + "/ok"})
	w.Close()
	os.Stderr = oldErr
	stderr, _ := io.ReadAll(r)
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if !bytes.Contains(stderr, []byte("> GET")) || !bytes.Contains(stderr, []byte("< HTTP/")) {
		t.Errorf("verbose output missing prefixes: %q", stderr)
	}
}
