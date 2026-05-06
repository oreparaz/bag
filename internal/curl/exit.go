package curl

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/url"
	"os"
	"strings"
	"syscall"
)

// curl(1) exit codes we map. The full list is large; this is the subset
// matching the 80% feature set.
const (
	exitOK              = 0
	exitUnsupportedURL  = 1
	exitFailedInit      = 2
	exitURLMalformed    = 3
	exitCouldntResolve  = 6
	exitCouldntConnect  = 7
	exitOptionSyntax    = 2 // curl uses 2 for "failed init" / option errors
	exitWriteError      = 23
	exitReadError       = 26
	exitOperationTimeout = 28
	exitTooManyRedirs   = 47
	exitHTTPReturned    = 22 // -f triggers this on >= 400
	exitSSLConnectError = 35
	exitSSLCACertBad    = 60
	exitSendError       = 55
	exitRecvError       = 56
)

// classify converts a Go error from the HTTP/transport stack into a curl
// exit code. Best-effort: errors from net/http and crypto/tls are
// matched by content.
func classify(err error) int {
	if err == nil {
		return exitOK
	}

	// Context-style timeouts.
	if errors.Is(err, context.DeadlineExceeded) || isTimeout(err) {
		return exitOperationTimeout
	}

	// DNS resolution
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return exitCouldntResolve
	}

	// TLS verification. Order matters: tls.CertificateVerificationError
	// is the wrapper used by Go 1.20+ on every platform; on Linux it
	// generally unwraps to one of the x509 types, but on macOS (where
	// cgo-enabled Go uses Apple's Security framework for verification)
	// the underlying error is opaque. We catch the wrapper directly and
	// also keep the x509 unwrap paths for clarity.
	var verifyErr *tls.CertificateVerificationError
	if errors.As(err, &verifyErr) {
		return exitSSLCACertBad
	}
	var unkAuth x509.UnknownAuthorityError
	if errors.As(err, &unkAuth) {
		return exitSSLCACertBad
	}
	var hostErr x509.HostnameError
	if errors.As(err, &hostErr) {
		return exitSSLCACertBad
	}
	var certInvalid x509.CertificateInvalidError
	if errors.As(err, &certInvalid) {
		return exitSSLCACertBad
	}
	var recordHdr tls.RecordHeaderError
	if errors.As(err, &recordHdr) {
		return exitSSLConnectError
	}

	// Connection refused / unreachable.
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if errors.Is(opErr.Err, syscall.ECONNREFUSED) ||
			errors.Is(opErr.Err, syscall.EHOSTUNREACH) ||
			errors.Is(opErr.Err, syscall.ENETUNREACH) {
			return exitCouldntConnect
		}
		if isTimeout(opErr) {
			return exitOperationTimeout
		}
	}

	// URL parse / unsupported scheme.
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if urlErr.Err != nil {
			s := urlErr.Err.Error()
			if strings.Contains(s, "unsupported protocol scheme") {
				return exitUnsupportedURL
			}
		}
	}

	// File I/O on output paths.
	if errors.Is(err, os.ErrPermission) || errors.Is(err, os.ErrNotExist) {
		return exitWriteError
	}

	return exitRecvError
}

func isTimeout(err error) bool {
	type timeout interface{ Timeout() bool }
	var t timeout
	if errors.As(err, &t) {
		return t.Timeout()
	}
	return false
}
