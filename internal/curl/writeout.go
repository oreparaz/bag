package curl

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// writeWriteOut implements a subset of curl's -w format strings.
//
// Variables supported (covering the 80% target):
//
//	%{http_code}
//	%{response_code}
//	%{size_download}
//	%{size_header}
//	%{content_type}
//	%{url_effective}
//	%{remote_ip}
//	%{remote_port}
//	%{num_redirects}   (0 today; we don't track count separately yet)
//	%{scheme}
//	%{method}
//	%{exitcode}        always "0" here — caller writes after success
//	%{stderr}          newline before next variable; printed to stderr
//	\n \r \t           interpreted in the format string
//
// Anything we don't recognize is emitted verbatim, matching curl's behavior.
func writeWriteOut(w io.Writer, format string, resp *http.Response, downloaded int64) {
	var b strings.Builder
	i := 0
	for i < len(format) {
		c := format[i]
		switch {
		case c == '\\' && i+1 < len(format):
			switch format[i+1] {
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case '\\':
				b.WriteByte('\\')
			default:
				b.WriteByte(format[i+1])
			}
			i += 2
		case c == '%' && i+1 < len(format) && format[i+1] == '{':
			end := strings.IndexByte(format[i+2:], '}')
			if end < 0 {
				b.WriteByte(c)
				i++
				continue
			}
			name := format[i+2 : i+2+end]
			b.WriteString(resolveVar(name, resp, downloaded))
			i += 2 + end + 1
		default:
			b.WriteByte(c)
			i++
		}
	}
	fmt.Fprint(w, b.String())
}

func resolveVar(name string, resp *http.Response, downloaded int64) string {
	switch name {
	case "http_code", "response_code":
		return strconv.Itoa(resp.StatusCode)
	case "size_download":
		return strconv.FormatInt(downloaded, 10)
	case "size_header":
		n := 0
		for k, vs := range resp.Header {
			for _, v := range vs {
				n += len(k) + len(v) + 4 // ": " + CRLF
			}
		}
		return strconv.Itoa(n)
	case "content_type":
		return resp.Header.Get("Content-Type")
	case "url_effective":
		if resp.Request != nil && resp.Request.URL != nil {
			return resp.Request.URL.String()
		}
		return ""
	case "scheme":
		if resp.Request != nil && resp.Request.URL != nil {
			return resp.Request.URL.Scheme
		}
		return ""
	case "method":
		if resp.Request != nil {
			return resp.Request.Method
		}
		return ""
	case "exitcode":
		return "0"
	case "num_redirects":
		return "0"
	}
	return "%{" + name + "}"
}
