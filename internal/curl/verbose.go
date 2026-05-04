package curl

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

// writeVerboseRequest emits the curl-style "> " trace lines for a request.
func writeVerboseRequest(w io.Writer, req *http.Request, hasBody bool) {
	fmt.Fprintf(w, "* Connected to %s\n", req.URL.Host)
	if req.URL.Scheme == "https" {
		fmt.Fprintln(w, "* TLS connection established")
	}
	path := req.URL.RequestURI()
	if path == "" {
		path = "/"
	}
	fmt.Fprintf(w, "> %s %s HTTP/1.1\n", req.Method, path)
	host := req.Host
	if host == "" {
		host = req.URL.Host
	}
	fmt.Fprintf(w, "> Host: %s\n", host)
	for _, k := range sortedHeaderKeys(req.Header) {
		for _, v := range req.Header[k] {
			fmt.Fprintf(w, "> %s: %s\n", k, v)
		}
	}
	fmt.Fprintln(w, "> ")
	if hasBody {
		fmt.Fprintln(w, "* upload completely sent off")
	}
}

func writeVerboseResponse(w io.Writer, resp *http.Response) {
	fmt.Fprintf(w, "< HTTP/%d.%d %s\n", resp.ProtoMajor, resp.ProtoMinor, resp.Status)
	for _, k := range sortedHeaderKeys(resp.Header) {
		for _, v := range resp.Header[k] {
			fmt.Fprintf(w, "< %s: %s\n", k, v)
		}
	}
	fmt.Fprintln(w, "< ")
}

func sortedHeaderKeys(h http.Header) []string {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// printHelp emits a short usage summary. Real curl --help is huge; we ship
// the common subset with a pointer to README for the rest.
func printHelp(w io.Writer) {
	const help = `Usage: curl [options...] <url>
Common options:
 -A, --user-agent <name>      Send User-Agent <name> to server
 -b, --cookie <data|file>     Send cookies from string/file
 -c, --cookie-jar <file>      Save cookies to file
 -d, --data <data>            HTTP POST data
     --data-binary <data>     HTTP POST binary data
     --data-urlencode <data>  HTTP POST data url encoded
     --data-raw <data>        HTTP POST data, '@' allowed
 -e, --referer <url>          Referer URL
 -F, --form <name=content>    Specify multipart MIME data
 -f, --fail                   Fail silently (no output) on HTTP errors
 -G, --get                    Put the post data in the URL and use GET
 -H, --header <header>        Pass custom header(s) to server
 -h, --help                   Get help
 -I, --head                   Show document info only
 -i, --include                Include response headers in output
 -k, --insecure               Allow insecure server connections
 -L, --location               Follow redirects
 -m, --max-time <fractional>  Maximum time allowed for transfer
     --max-redirs <num>       Maximum number of redirects allowed
 -o, --output <file>          Write to file instead of stdout
 -O, --remote-name            Write output to file named as the remote file
     --connect-timeout <sec>  Maximum time allowed for connection
     --compressed             Request compressed response
     --retry <num>            Retry request if transient problems occur
 -s, --silent                 Silent mode
 -S, --show-error             Show error even when -s is used
 -u, --user <user:password>   Server user and password
 -v, --verbose                Make the operation more talkative
 -w, --write-out <format>     Use output FORMAT after completion
 -X, --request <command>      Specify request command to use
 -x, --proxy [protocol://]host[:port]   Use this proxy
 -4, --ipv4                   Resolve names to IPv4 addresses
 -6, --ipv6                   Resolve names to IPv6 addresses

For full documentation see https://github.com/oreparaz/bag
`
	io.WriteString(w, strings.TrimLeft(help, "\n"))
}
