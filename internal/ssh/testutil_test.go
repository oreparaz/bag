package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"testing"

	xssh "golang.org/x/crypto/ssh"
)

// writeOpenSSHKey writes an ed25519 OpenSSH-format private key to path
// and returns the matching public key in SSH wire format.
func writeOpenSSHKey(t *testing.T, path string) xssh.PublicKey {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := xssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(block)
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	pubSSH, err := xssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return pubSSH
}

// captureStdout swaps os.Stdout for a pipe, runs fn, and returns what
// was written. Tests use this because connectAndRun streams remote
// stdout to os.Stdout. We close the write end after fn returns so the
// reader goroutine sees EOF, then collect its output through a buffered
// channel.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan []byte, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- b
	}()
	fn()
	w.Close()
	os.Stdout = old
	return string(<-done)
}

// knownHostsLine returns one OpenSSH known_hosts line for host:port +
// the given pubkey. Format: "host KEYTYPE BASE64".
func knownHostsLine(host string, port int, pub xssh.PublicKey) []byte {
	hostField := host
	if port != 22 {
		hostField = "[" + host + "]:" + strconv.Itoa(port)
	}
	line := fmt.Sprintf("%s %s\n", hostField, string(xssh.MarshalAuthorizedKey(pub)))
	// MarshalAuthorizedKey appends "\n" already; trim and rebuild for
	// determinism.
	if line[len(line)-2] == '\n' {
		line = line[:len(line)-1]
	}
	return []byte(line)
}

// silence unused-import grumbles when the file's helpers move around.
var _ = net.JoinHostPort
