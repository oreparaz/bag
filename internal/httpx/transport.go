// Package httpx is the shared HTTP transport layer used by the bag tools.
//
// It builds a *http.Transport (and matching dialer) from a small set of
// orthogonal options: TLS verification, connect timeout, proxy URL, IP
// family, and HTTP version. Higher-level concerns (retries, redirects,
// auth, output, progress) belong in the calling tool.
package httpx

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Options configures the transport. The zero value is reasonable.
type Options struct {
	// Insecure disables TLS verification (curl -k, wget --no-check-certificate).
	Insecure bool

	// ConnectTimeout caps the dial. Zero means no extra cap (the OS default).
	ConnectTimeout time.Duration

	// Proxy is an explicit proxy URL. If empty, the environment
	// (HTTP_PROXY / HTTPS_PROXY / NO_PROXY) is consulted.
	Proxy string

	// NoProxyEnv disables environment proxy lookup. If Proxy is empty
	// and NoProxyEnv is true, no proxy is used at all.
	NoProxyEnv bool

	// IPFamily forces IPv4 ("4") or IPv6 ("6"). Empty means both.
	IPFamily string

	// HTTPVersion: 1 for HTTP/1.1 only, 2 for ALPN preferring h2, 0 for default.
	HTTPVersion int

	// CACertFile, if non-empty, replaces the system root pool.
	CACertFile string

	// ServerName overrides the SNI/verification hostname.
	ServerName string
}

// Build returns a transport configured from opts. Any error is fatal for
// the calling tool — typically a malformed proxy URL.
func Build(opts Options) (*http.Transport, error) {
	dialer := &net.Dialer{
		Timeout:   opts.ConnectTimeout,
		KeepAlive: 30 * time.Second,
	}

	dialContext := func(ctx context.Context, network, addr string) (net.Conn, error) {
		switch opts.IPFamily {
		case "4":
			network = forceFamily(network, "tcp4")
		case "6":
			network = forceFamily(network, "tcp6")
		}
		return dialer.DialContext(ctx, network, addr)
	}

	tlsConf := &tls.Config{
		InsecureSkipVerify: opts.Insecure,
		MinVersion:         tls.VersionTLS12,
		ServerName:         opts.ServerName,
	}
	if opts.CACertFile != "" {
		pool, err := loadCAFile(opts.CACertFile)
		if err != nil {
			return nil, err
		}
		tlsConf.RootCAs = pool
	}

	tr := &http.Transport{
		DialContext:           dialContext,
		TLSClientConfig:       tlsConf,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		ForceAttemptHTTP2:     opts.HTTPVersion != 1,
		MaxIdleConns:          100,
		// We manage Accept-Encoding ourselves to mirror curl/wget:
		// no header on bare GET, gzip only when the user asks.
		DisableCompression: true,
	}

	switch {
	case opts.Proxy != "":
		u, err := parseProxyURL(opts.Proxy)
		if err != nil {
			return nil, err
		}
		tr.Proxy = http.ProxyURL(u)
	case !opts.NoProxyEnv:
		tr.Proxy = http.ProxyFromEnvironment
	default:
		tr.Proxy = nil
	}

	return tr, nil
}

func forceFamily(network, family string) string {
	switch network {
	case "tcp", "tcp4", "tcp6":
		return family
	}
	return network
}

// parseProxyURL accepts forms curl accepts: "host:port", "http://host:port",
// "socks5://host:port", etc. Bare host:port is treated as http://.
func parseProxyURL(s string) (*url.URL, error) {
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy URL: %w", err)
	}
	if u.Host == "" {
		return nil, errors.New("invalid proxy URL: missing host")
	}
	return u, nil
}
