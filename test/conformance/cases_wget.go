package conformance

import (
	"fmt"
	"os"
	"path/filepath"
)

// WgetCases is the wget conformance corpus.
//
// wget writes its body to a file by default, so most cases use -O- to
// route to stdout for byte-exact comparison.
func WgetCases() []Case {
	return []Case{
		{
			Name: "stdout",
			Tool: ToolWget,
			Args: func(e Env) ([]string, string) {
				return []string{"-q", "-O-", e.HTTP + "/ok"}, ""
			},
		},
		{
			Name: "default_filename",
			Tool: ToolWget,
			Args: func(e Env) ([]string, string) {
				return []string{"-q", e.HTTP + "/ok"}, ""
			},
			CompareFile: "ok",
		},
		{
			Name: "directory_prefix",
			Tool: ToolWget,
			Args: func(e Env) ([]string, string) {
				return []string{"-q", "-P", "saved", e.HTTP + "/ok"}, ""
			},
			CompareFile: filepath.Join("saved", "ok"),
		},
		{
			Name: "user_agent",
			Tool: ToolWget,
			Args: func(e Env) ([]string, string) {
				return []string{"-q", "-O-", "-U", "myUA/1.0", e.HTTP + "/headers"}, ""
			},
			CompareJSON:       true,
			JSONIgnoreFields:  []string{"host"},
			JSONIgnoreHeaders: []string{"Accept-Encoding"},
		},
		{
			Name: "header",
			Tool: ToolWget,
			Args: func(e Env) ([]string, string) {
				return []string{"-q", "-O-", "--header=X-Foo: bar", e.HTTP + "/headers"}, ""
			},
			CompareJSON:       true,
			JSONIgnoreFields:  []string{"host"},
			JSONIgnoreHeaders: []string{"Accept-Encoding", "User-Agent"},
		},
		{
			Name: "basic_auth",
			Tool: ToolWget,
			Args: func(e Env) ([]string, string) {
				return []string{"-q", "-O-", "--user=alice", "--password=secret", e.HTTP + "/basic-auth/alice/secret"}, ""
			},
		},
		{
			Name: "redirect_follows",
			Tool: ToolWget,
			Args: func(e Env) ([]string, string) {
				return []string{"-q", "-O-", e.HTTP + "/redirect/3"}, ""
			},
		},
		{
			Name: "no_check_certificate",
			Tool: ToolWget,
			Args: func(e Env) ([]string, string) {
				return []string{"-q", "-O-", "--no-check-certificate", e.HTTPS + "/ok"}, ""
			},
		},
		{
			Name: "ca_certificate_verify",
			Tool: ToolWget,
			Args: func(e Env) ([]string, string) {
				return []string{"-q", "-O-", "--ca-certificate=" + e.CAPath, e.HTTPS + "/ok"}, ""
			},
		},
		{
			Name: "default_cert_rejects_self_signed",
			Tool: ToolWget,
			Args: func(e Env) ([]string, string) {
				return []string{"-q", "-O-", e.HTTPS + "/ok"}, ""
			},
			// Real wget exits 5 on cert verify; bag does too.
			ExpectExit:    ptr(5),
			CompareStdout: ptr(false),
		},
		{
			Name: "404_exit",
			Tool: ToolWget,
			Args: func(e Env) ([]string, string) {
				return []string{"-q", "-O-", e.HTTP + "/status/404"}, ""
			},
			ExpectExit:    ptr(8),
			CompareStdout: ptr(false),
		},
		{
			Name: "401_exit",
			Tool: ToolWget,
			Args: func(e Env) ([]string, string) {
				return []string{"-q", "-O-", e.HTTP + "/basic-auth/u/p"}, ""
			},
			// real wget reports 401 -> exit 6 (auth fail); bag matches.
			ExpectExit:    ptr(6),
			CompareStdout: ptr(false),
		},
		{
			Name: "max_redirect_exceeded",
			Tool: ToolWget,
			Args: func(e Env) ([]string, string) {
				return []string{"-q", "-O-", "--max-redirect=2", e.HTTP + "/redirect/5"}, ""
			},
			ExpectExitMatch: ptr(false), // wget reports 4; bag may report 1 (generic)
			CompareStdout:   ptr(false),
		},
		{
			Name: "bytes_large_deterministic",
			Tool: ToolWget,
			Args: func(e Env) ([]string, string) {
				return []string{"-q", "-O", "blob", fmt.Sprintf("%s/bytes/%d", e.HTTP, 65536)}, ""
			},
			CompareFile: "blob",
		},
		{
			Name: "tries_one_on_unreachable",
			Tool: ToolWget,
			Args: func(_ Env) ([]string, string) {
				// 127.0.0.1:1 connection-refused is reliable across distros.
				return []string{"-q", "-O-", "-t", "1", "--waitretry=0", "http://127.0.0.1:1/"}, ""
			},
			ExpectExit:    ptr(4),
			CompareStdout: ptr(false),
		},
		{
			Name: "timeout_short",
			Tool: ToolWget,
			Args: func(e Env) ([]string, string) {
				return []string{"-q", "-O-", "--timeout=0.5", "-t", "1", "--waitretry=0",
					fmt.Sprintf("%s/slow?ms=3000", e.HTTP)}, ""
			},
			ExpectExit:    ptr(4),
			CompareStdout: ptr(false),
		},
		{
			Name: "input_file",
			Tool: ToolWget,
			Args: func(e Env) ([]string, string) {
				p := filepath.Join(e.TempDir, "urls.txt")
				_ = writeFile(p, fmt.Sprintf("%s/ok\n%s/empty\n", e.HTTP, e.HTTP))
				return []string{"-q", "-i", p}, ""
			},
			CompareFile: "ok",
		},
		{
			Name: "content_disposition_filename",
			Tool: ToolWget,
			Args: func(e Env) ([]string, string) {
				return []string{"-q", "--content-disposition", e.HTTP + "/cd-filename"}, ""
			},
			CompareFile: "report.txt",
		},
	}
}

func writeFile(p, content string) error {
	return os.WriteFile(p, []byte(content), 0o644)
}
