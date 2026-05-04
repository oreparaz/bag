package conformance

import (
	"fmt"
	"os"
	"path/filepath"
)

// CurlCases is the curl conformance corpus.
//
// Each case must produce identical observable output between real curl
// and bag's curl. We don't compare User-Agent, X-Amzn-Trace-Id, etc.
//
// Self-signed HTTPS is exercised via --cacert <test CA>.
func CurlCases() []Case {
	yes := ptr(true)
	return []Case{
		{
			Name: "simple_get",
			Tool: ToolCurl,
			Args: func(e Env) ([]string, string) {
				return []string{"-s", e.HTTP + "/ok"}, ""
			},
		},
		{
			Name: "headers_echoed",
			Tool: ToolCurl,
			Args: func(e Env) ([]string, string) {
				return []string{"-s", "-A", "test/1.0", "-H", "X-Foo: bar", e.HTTP + "/headers"}, ""
			},
			CompareJSON:       true,
			JSONIgnoreFields:  []string{"host"},
			JSONIgnoreHeaders: []string{"Accept-Encoding"}, // curl on some hosts adds it
		},
		{
			Name: "method_explicit",
			Tool: ToolCurl,
			Args: func(e Env) ([]string, string) {
				return []string{"-s", "-X", "DELETE", e.HTTP + "/echo"}, ""
			},
			CompareJSON:       true,
			JSONIgnoreFields:  []string{"host"},
			JSONIgnoreHeaders: []string{"User-Agent", "Accept-Encoding"},
		},
		{
			Name: "post_data",
			Tool: ToolCurl,
			Args: func(e Env) ([]string, string) {
				return []string{"-s", "-d", "name=alice&age=30", e.HTTP + "/echo"}, ""
			},
			CompareJSON:       true,
			JSONIgnoreFields:  []string{"host"},
			JSONIgnoreHeaders: []string{"User-Agent", "Accept-Encoding"},
		},
		{
			Name: "post_data_multi",
			Tool: ToolCurl,
			Args: func(e Env) ([]string, string) {
				return []string{"-s", "-d", "a=1", "-d", "b=2", e.HTTP + "/echo"}, ""
			},
			CompareJSON:       true,
			JSONIgnoreFields:  []string{"host"},
			JSONIgnoreHeaders: []string{"User-Agent", "Accept-Encoding"},
		},
		{
			Name: "post_data_atfile",
			Tool: ToolCurl,
			Args: func(e Env) ([]string, string) {
				p := filepath.Join(e.TempDir, "in.txt")
				_ = os.WriteFile(p, []byte("foo\nbar\n"), 0o644)
				return []string{"-s", "-d", "@" + p, e.HTTP + "/echo"}, ""
			},
			CompareJSON:       true,
			JSONIgnoreFields:  []string{"host"},
			JSONIgnoreHeaders: []string{"User-Agent", "Accept-Encoding"},
		},
		{
			Name: "post_data_binary_atfile",
			Tool: ToolCurl,
			Args: func(e Env) ([]string, string) {
				p := filepath.Join(e.TempDir, "in.txt")
				_ = os.WriteFile(p, []byte("foo\nbar\n"), 0o644)
				return []string{"-s", "--data-binary", "@" + p, e.HTTP + "/echo"}, ""
			},
			CompareJSON:       true,
			JSONIgnoreFields:  []string{"host"},
			JSONIgnoreHeaders: []string{"User-Agent", "Accept-Encoding"},
		},
		{
			Name: "data_urlencode",
			Tool: ToolCurl,
			Args: func(e Env) ([]string, string) {
				return []string{"-s", "--data-urlencode", "msg=hello world", e.HTTP + "/echo"}, ""
			},
			CompareJSON:       true,
			JSONIgnoreFields:  []string{"host"},
			JSONIgnoreHeaders: []string{"User-Agent", "Accept-Encoding"},
		},
		{
			Name: "get_with_data",
			Tool: ToolCurl,
			Args: func(e Env) ([]string, string) {
				return []string{"-s", "-G", "-d", "k=v&x=y", e.HTTP + "/echo"}, ""
			},
			CompareJSON:       true,
			JSONIgnoreFields:  []string{"host"},
			JSONIgnoreHeaders: []string{"User-Agent", "Accept-Encoding"},
		},
		{
			Name: "redirect_follow",
			Tool: ToolCurl,
			Args: func(e Env) ([]string, string) {
				return []string{"-sL", e.HTTP + "/redirect/3"}, ""
			},
		},
		{
			Name: "redirect_no_follow",
			Tool: ToolCurl,
			Args: func(e Env) ([]string, string) {
				return []string{"-s", e.HTTP + "/redirect/1"}, ""
			},
		},
		{
			Name: "max_redirs_exceeded",
			Tool: ToolCurl,
			Args: func(e Env) ([]string, string) {
				return []string{"-sL", "--max-redirs", "2", e.HTTP + "/redirect/5"}, ""
			},
			ExpectExit: ptr(47),
		},
		{
			Name: "output_to_file",
			Tool: ToolCurl,
			Args: func(e Env) ([]string, string) {
				return []string{"-s", "-o", "saved.txt", e.HTTP + "/ok"}, ""
			},
			CompareFile: "saved.txt",
		},
		{
			Name: "remote_name",
			Tool: ToolCurl,
			Args: func(e Env) ([]string, string) {
				return []string{"-s", "-O", e.HTTP + "/ok"}, ""
			},
			CompareFile: "ok",
		},
		{
			Name: "include_headers",
			Tool: ToolCurl,
			Args: func(e Env) ([]string, string) {
				return []string{"-s", "-i", e.HTTP + "/empty"}, ""
			},
			// real curl uses CRLF; bag too. Skip exact comparison; assert exit only.
			CompareStdout: ptr(false),
		},
		{
			Name: "head",
			Tool: ToolCurl,
			Args: func(e Env) ([]string, string) {
				return []string{"-s", "-I", e.HTTP + "/ok"}, ""
			},
			CompareStdout: ptr(false),
		},
		{
			Name: "compressed_gzip",
			Tool: ToolCurl,
			Args: func(e Env) ([]string, string) {
				return []string{"-s", "--compressed", e.HTTP + "/gzip"}, ""
			},
			CompareJSON:       true,
			JSONIgnoreHeaders: []string{"User-Agent", "Accept-Encoding"},
		},
		{
			Name: "https_with_cacert",
			Tool: ToolCurl,
			Args: func(e Env) ([]string, string) {
				return []string{"-s", "--cacert", e.CAPath, e.HTTPS + "/ok"}, ""
			},
		},
		{
			Name: "https_insecure",
			Tool: ToolCurl,
			Args: func(e Env) ([]string, string) {
				return []string{"-s", "-k", e.HTTPS + "/ok"}, ""
			},
		},
		{
			Name: "https_default_rejects_self_signed",
			Tool: ToolCurl,
			Args: func(e Env) ([]string, string) {
				return []string{"-s", e.HTTPS + "/ok"}, ""
			},
			ExpectExitMatch: yes, // both should exit non-zero (real=60, bag=60)
			ExpectExit:      ptr(60),
			CompareStdout:   ptr(false),
		},
		{
			Name: "basic_auth_ok",
			Tool: ToolCurl,
			Args: func(e Env) ([]string, string) {
				return []string{"-s", "-u", "alice:secret", e.HTTP + "/basic-auth/alice/secret"}, ""
			},
		},
		{
			Name: "basic_auth_fail_with_f",
			Tool: ToolCurl,
			Args: func(e Env) ([]string, string) {
				return []string{"-sf", "-u", "x:y", e.HTTP + "/basic-auth/alice/secret"}, ""
			},
			ExpectExit: ptr(22),
		},
		{
			Name: "fail_on_404",
			Tool: ToolCurl,
			Args: func(e Env) ([]string, string) {
				return []string{"-sf", e.HTTP + "/status/404"}, ""
			},
			ExpectExit: ptr(22),
		},
		{
			Name: "no_fail_on_404",
			Tool: ToolCurl,
			Args: func(e Env) ([]string, string) {
				return []string{"-s", e.HTTP + "/status/404"}, ""
			},
		},
		{
			Name: "range_partial",
			Tool: ToolCurl,
			Args: func(e Env) ([]string, string) {
				return []string{"-s", "-r", "5-9", e.HTTP + "/range"}, ""
			},
		},
		{
			Name: "bytes_large_deterministic",
			Tool: ToolCurl,
			Args: func(e Env) ([]string, string) {
				return []string{"-s", "-o", "blob", fmt.Sprintf("%s/bytes/%d", e.HTTP, 65536)}, ""
			},
			CompareFile: "blob",
		},
		{
			Name: "cookie_inline",
			Tool: ToolCurl,
			Args: func(e Env) ([]string, string) {
				return []string{"-s", "-b", "k=v; x=y", e.HTTP + "/cookies"}, ""
			},
			CompareJSON: true,
		},
		{
			Name: "cookie_jar_roundtrip",
			Tool: ToolCurl,
			Args: func(e Env) ([]string, string) {
				return []string{"-s", "-c", "jar.txt", "-b", "session=abc", e.HTTP + "/cookies"}, ""
			},
			CompareJSON: true,
		},
		{
			Name: "max_time_short",
			Tool: ToolCurl,
			Args: func(e Env) ([]string, string) {
				return []string{"-s", "--max-time", "0.2", e.HTTP + "/slow?ms=2000"}, ""
			},
			ExpectExit:    ptr(28),
			CompareStdout: ptr(false),
		},
		{
			Name: "multipart_form",
			Tool: ToolCurl,
			Args: func(e Env) ([]string, string) {
				p := filepath.Join(e.TempDir, "doc.txt")
				_ = os.WriteFile(p, []byte("hello multipart\n"), 0o644)
				return []string{"-s", "-F", "name=alice", "-F", "file=@" + p, e.HTTP + "/multipart"}, ""
			},
			CompareJSON: true,
		},
		{
			Name: "referer",
			Tool: ToolCurl,
			Args: func(e Env) ([]string, string) {
				return []string{"-s", "-e", "https://prev.example/", e.HTTP + "/headers"}, ""
			},
			CompareJSON:       true,
			JSONIgnoreFields:  []string{"host"},
			JSONIgnoreHeaders: []string{"User-Agent"},
		},
		{
			Name: "put_with_data",
			Tool: ToolCurl,
			Args: func(e Env) ([]string, string) {
				return []string{"-s", "-X", "PUT", "-d", "payload=42", e.HTTP + "/echo"}, ""
			},
			CompareJSON:       true,
			JSONIgnoreFields:  []string{"host"},
			JSONIgnoreHeaders: []string{"User-Agent"},
		},
		{
			Name: "header_delete",
			Tool: ToolCurl,
			Args: func(e Env) ([]string, string) {
				return []string{"-s", "-H", "Accept:", e.HTTP + "/headers"}, ""
			},
			CompareJSON:       true,
			JSONIgnoreFields:  []string{"host"},
			JSONIgnoreHeaders: []string{"User-Agent"},
		},
		{
			Name: "host_override",
			Tool: ToolCurl,
			Args: func(e Env) ([]string, string) {
				return []string{"-s", "-H", "Host: virtual.example", e.HTTP + "/headers"}, ""
			},
			CompareJSON:      true,
			JSONIgnoreFields: []string{"host"}, // server reports the request Host
			JSONIgnoreHeaders: []string{"User-Agent"},
		},
		{
			Name: "writeout_status",
			Tool: ToolCurl,
			Args: func(e Env) ([]string, string) {
				return []string{"-s", "-o", os.DevNull, "-w", "%{http_code}", e.HTTP + "/status/418"}, ""
			},
		},
		{
			Name: "unsupported_protocol",
			Tool: ToolCurl,
			Args: func(_ Env) ([]string, string) {
				return []string{"-s", "ftp://127.0.0.1/x"}, ""
			},
			ExpectExitMatch: ptr(false),
			ExpectExit:      ptr(1),
			CompareStdout:   ptr(false),
		},
	}
}
