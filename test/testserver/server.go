// Package testserver runs a local HTTP/HTTPS server used by the unit
// and conformance tests. The endpoints cover the surface of curl / wget
// features we ship.
package testserver

import (
	"compress/flate"
	"compress/gzip"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"path"
	"strconv"
	"strings"
	"time"
)

func shortHash(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:8])
}

// Servers wraps the HTTP and HTTPS servers used by tests.
type Servers struct {
	HTTP  *httptest.Server
	HTTPS *httptest.Server
	// CACertPEM is the CA cert that signed the HTTPS test cert.
	CACertPEM []byte
}

// Start launches both an HTTP and an HTTPS server on ephemeral ports.
// Both serve the same handler. Callers should defer Servers.Close().
func Start() (*Servers, error) {
	mux := buildMux()

	http := httptest.NewServer(mux)
	https := httptest.NewUnstartedServer(mux)

	caPEM, certPEM, keyPEM, err := selfSign("localhost", []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")})
	if err != nil {
		http.Close()
		return nil, err
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		http.Close()
		return nil, err
	}
	https.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	https.StartTLS()

	return &Servers{HTTP: http, HTTPS: https, CACertPEM: caPEM}, nil
}

func (s *Servers) Close() {
	if s.HTTP != nil {
		s.HTTP.Close()
	}
	if s.HTTPS != nil {
		s.HTTPS.Close()
	}
}

func buildMux() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/ok", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte("ok\n"))
	})

	mux.HandleFunc("/empty", func(w http.ResponseWriter, _ *http.Request) {})

	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		out := map[string]any{
			"method":  r.Method,
			"path":    r.URL.Path,
			"query":   r.URL.RawQuery,
			"headers": flattenHeaders(r.Header),
			"body":    string(body),
			"host":    r.Host,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})

	mux.HandleFunc("/status/", func(w http.ResponseWriter, r *http.Request) {
		code, _ := strconv.Atoi(path.Base(r.URL.Path))
		if code == 0 {
			code = 200
		}
		w.WriteHeader(code)
		fmt.Fprintf(w, "status %d\n", code)
	})

	mux.HandleFunc("/redirect/", func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(path.Base(r.URL.Path))
		if n <= 0 {
			http.Redirect(w, r, "/ok", http.StatusFound)
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/redirect/%d", n-1), http.StatusFound)
	})

	mux.HandleFunc("/redirect-loop", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/redirect-loop", http.StatusFound)
	})

	mux.HandleFunc("/redirect-absolute/", func(w http.ResponseWriter, r *http.Request) {
		// /redirect-absolute/<scheme>/<host>/<port>/<path>
		// (used to test cross-host redirects)
		http.Redirect(w, r, r.URL.Query().Get("to"), http.StatusFound)
	})

	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		ms, _ := strconv.Atoi(r.URL.Query().Get("ms"))
		time.Sleep(time.Duration(ms) * time.Millisecond)
		w.Write([]byte("slow ok\n"))
	})

	mux.HandleFunc("/headers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"headers": flattenHeaders(r.Header),
			"host":    r.Host,
		})
	})

	mux.HandleFunc("/basic-auth/", func(w http.ResponseWriter, r *http.Request) {
		// /basic-auth/{user}/{pass}
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) < 3 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		want := "Basic " + base64.StdEncoding.EncodeToString([]byte(parts[1]+":"+parts[2]))
		got := r.Header.Get("Authorization")
		if got != want {
			w.Header().Set("WWW-Authenticate", "Basic realm=test")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte("auth ok\n"))
	})

	mux.HandleFunc("/cookies/set", func(w http.ResponseWriter, r *http.Request) {
		for k, vs := range r.URL.Query() {
			http.SetCookie(w, &http.Cookie{Name: k, Value: vs[0], Path: "/"})
		}
		w.Write([]byte("set\n"))
	})

	mux.HandleFunc("/cookies", func(w http.ResponseWriter, r *http.Request) {
		out := map[string]string{}
		for _, c := range r.Cookies() {
			out[c.Name] = c.Value
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})

	mux.HandleFunc("/gzip", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "application/json")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		_ = json.NewEncoder(gz).Encode(map[string]any{
			"gzipped": true,
			"headers": flattenHeaders(r.Header),
		})
	})

	mux.HandleFunc("/deflate", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "deflate")
		w.Header().Set("Content-Type", "application/json")
		zw, _ := flate.NewWriter(w, flate.DefaultCompression)
		defer zw.Close()
		_ = json.NewEncoder(zw).Encode(map[string]any{"deflated": true})
	})

	mux.HandleFunc("/bytes/", func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(path.Base(r.URL.Path))
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(n))
		// Deterministic body so tests can diff bag vs curl byte-for-byte.
		buf := make([]byte, 1024)
		for i := range buf {
			buf[i] = byte(i)
		}
		written := 0
		for written < n {
			chunk := len(buf)
			if n-written < chunk {
				chunk = n - written
			}
			_, _ = w.Write(buf[:chunk])
			written += chunk
		}
	})

	mux.HandleFunc("/chunked", func(w http.ResponseWriter, _ *http.Request) {
		fl, _ := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/plain")
		for i := 0; i < 5; i++ {
			fmt.Fprintf(w, "chunk %d\n", i)
			if fl != nil {
				fl.Flush()
			}
		}
	})

	mux.HandleFunc("/range", func(w http.ResponseWriter, r *http.Request) {
		full := []byte("0123456789ABCDEF") // 16 bytes
		w.Header().Set("Accept-Ranges", "bytes")
		rng := r.Header.Get("Range")
		if rng == "" {
			w.Header().Set("Content-Length", strconv.Itoa(len(full)))
			w.Write(full)
			return
		}
		// Parse "bytes=N-" or "bytes=N-M"
		spec := strings.TrimPrefix(rng, "bytes=")
		dash := strings.IndexByte(spec, '-')
		if dash < 0 {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		start, _ := strconv.Atoi(spec[:dash])
		end := len(full) - 1
		if dash+1 < len(spec) {
			end, _ = strconv.Atoi(spec[dash+1:])
		}
		if start < 0 || start > end || end >= len(full) {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(full)))
		w.Header().Set("Content-Length", strconv.Itoa(end-start+1))
		w.WriteHeader(http.StatusPartialContent)
		w.Write(full[start : end+1])
	})

	mux.HandleFunc("/cd-filename", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="report.txt"`)
		_, _ = w.Write([]byte("downloaded\n"))
	})

	mux.HandleFunc("/multipart", func(w http.ResponseWriter, r *http.Request) {
		// Returns a deterministic JSON representation of a multipart POST,
		// independent of the random boundary the client picks.
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintln(w, err)
			return
		}
		out := map[string]any{}
		fields := map[string]string{}
		for k, vs := range r.MultipartForm.Value {
			fields[k] = strings.Join(vs, ",")
		}
		out["fields"] = fields
		files := map[string]map[string]any{}
		for k, fs := range r.MultipartForm.File {
			for _, fh := range fs {
				f, err := fh.Open()
				if err != nil {
					continue
				}
				body, _ := io.ReadAll(f)
				f.Close()
				files[k] = map[string]any{
					"filename": fh.Filename,
					"size":     fh.Size,
					"sha":      shortHash(body),
				}
			}
		}
		out["files"] = files
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})

	mux.HandleFunc("/links", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><body>
<a href="/ok">ok</a>
<a href="/echo">echo</a>
</body></html>`))
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Write([]byte("root\n"))
			return
		}
		http.NotFound(w, r)
	})

	return mux
}

func flattenHeaders(h http.Header) map[string]string {
	out := map[string]string{}
	for k, vs := range h {
		out[k] = strings.Join(vs, ",")
	}
	return out
}

// selfSign returns CA + leaf cert/key signed by it. Cert is valid for the
// passed DNS names and IPs.
func selfSign(commonName string, ips []net.IP) (caPEM, certPEM, keyPEM []byte, err error) {
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, nil, err
	}
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "bag-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, nil, err
	}

	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, nil, err
	}
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{commonName},
		IPAddresses:  ips,
	}
	caCert, _ := x509.ParseCertificate(caDER)
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, caCert, &leafKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, nil, err
	}

	caPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(leafKey)})
	return caPEM, certPEM, keyPEM, nil
}
