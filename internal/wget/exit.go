package wget

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/url"
	"strings"
	"syscall"
)

// wget(1) exit codes:
const (
	exitOK         = 0
	exitGeneric    = 1
	exitParse      = 2
	exitFileIO     = 3
	exitNetwork    = 4
	exitSSLVerify  = 5
	exitAuthFail   = 6
	exitProtocol   = 7
	exitServerErr  = 8
)

// classify maps a Go error from the HTTP/transport stack to a wget exit code.
func classify(err error) int {
	if err == nil {
		return exitOK
	}

	// TLS verification.
	var unkAuth x509.UnknownAuthorityError
	if errors.As(err, &unkAuth) {
		return exitSSLVerify
	}
	var hostErr x509.HostnameError
	if errors.As(err, &hostErr) {
		return exitSSLVerify
	}
	var certInvalid x509.CertificateInvalidError
	if errors.As(err, &certInvalid) {
		return exitSSLVerify
	}
	var recordHdr tls.RecordHeaderError
	if errors.As(err, &recordHdr) {
		return exitSSLVerify
	}

	// DNS / connect / network.
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return exitNetwork
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if errors.Is(opErr.Err, syscall.ECONNREFUSED) ||
			errors.Is(opErr.Err, syscall.EHOSTUNREACH) ||
			errors.Is(opErr.Err, syscall.ENETUNREACH) {
			return exitNetwork
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return exitNetwork
	}
	type timeout interface{ Timeout() bool }
	var to timeout
	if errors.As(err, &to) && to.Timeout() {
		return exitNetwork
	}

	// URL/Protocol.
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		s := urlErr.Err.Error()
		if strings.Contains(s, "unsupported protocol scheme") {
			return exitProtocol
		}
	}
	return exitGeneric
}
