package httpx

import (
	"crypto/x509"
	"errors"
	"fmt"
	"os"
)

// loadCAFile loads a PEM bundle and returns a CertPool containing only those
// roots. Used by --cacert / --ca-certificate.
func loadCAFile(path string) (*x509.CertPool, error) {
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading CA file: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, errors.New("no PEM certificates found in CA file")
	}
	return pool, nil
}
