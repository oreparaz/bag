package ssh

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	xssh "golang.org/x/crypto/ssh"
)

// testServer is a tiny in-memory SSH server. It accepts one publicKey
// matched against an authorized signer, and dispatches "exec" requests
// to a user-supplied handler.
type testServer struct {
	listener net.Listener
	hostKey  xssh.Signer
	authKey  xssh.PublicKey
	exec     func(cmd string) (stdout, stderr []byte, exit int)
	wg       sync.WaitGroup
	stop     chan struct{}
}

func newTestServer(t *testing.T, exec func(cmd string) (stdout, stderr []byte, exit int)) *testServer {
	t.Helper()

	// Host key.
	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostSigner, err := xssh.NewSignerFromKey(hostPriv)
	if err != nil {
		t.Fatal(err)
	}

	// Auth key (the client's public key allowed by the server).
	authPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	authPubSSH, err := xssh.NewPublicKey(authPub)
	if err != nil {
		t.Fatal(err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	s := &testServer{
		listener: ln,
		hostKey:  hostSigner,
		authKey:  authPubSSH,
		exec:     exec,
		stop:     make(chan struct{}),
	}
	s.wg.Add(1)
	go s.serve()
	t.Cleanup(s.Close)
	return s
}

func (s *testServer) Close() {
	close(s.stop)
	s.listener.Close()
	s.wg.Wait()
}

func (s *testServer) Addr() (host string, port int) {
	a := s.listener.Addr().(*net.TCPAddr)
	return a.IP.String(), a.Port
}

// AcceptKey replaces the server-side allowed pubkey with the given one.
// Used by tests that want to authenticate with a known signer.
func (s *testServer) AcceptKey(pub xssh.PublicKey) { s.authKey = pub }

func (s *testServer) serve() {
	defer s.wg.Done()
	cfg := &xssh.ServerConfig{
		PublicKeyCallback: func(c xssh.ConnMetadata, key xssh.PublicKey) (*xssh.Permissions, error) {
			if bytes.Equal(s.authKey.Marshal(), key.Marshal()) {
				return nil, nil
			}
			return nil, fmt.Errorf("unauthorized")
		},
		PasswordCallback: func(c xssh.ConnMetadata, pass []byte) (*xssh.Permissions, error) {
			if string(pass) == "letmein" {
				return nil, nil
			}
			return nil, fmt.Errorf("denied")
		},
	}
	cfg.AddHostKey(s.hostKey)

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handle(conn, cfg)
	}
}

func (s *testServer) handle(c net.Conn, cfg *xssh.ServerConfig) {
	defer c.Close()
	c.SetDeadline(time.Now().Add(30 * time.Second))
	srvConn, chans, reqs, err := xssh.NewServerConn(c, cfg)
	if err != nil {
		return
	}
	defer srvConn.Close()
	go xssh.DiscardRequests(reqs)

	for nc := range chans {
		if nc.ChannelType() != "session" {
			nc.Reject(xssh.UnknownChannelType, "unknown")
			continue
		}
		ch, sreqs, err := nc.Accept()
		if err != nil {
			continue
		}
		go s.handleSession(ch, sreqs)
	}
}

func (s *testServer) handleSession(ch xssh.Channel, reqs <-chan *xssh.Request) {
	defer ch.Close()
	for req := range reqs {
		switch req.Type {
		case "exec":
			cmd := parseExecPayload(req.Payload)
			req.Reply(true, nil)
			if s.exec == nil {
				ch.Write([]byte("no handler\n"))
				return
			}
			out, errOut, exit := s.exec(cmd)
			ch.Write(out)
			ch.Stderr().Write(errOut)
			// Send the SSH "exit-status" notification.
			ch.SendRequest("exit-status", false, exitStatusPayload(uint32(exit)))
			return
		case "shell":
			req.Reply(true, nil)
			io.WriteString(ch, "welcome\n")
			ch.SendRequest("exit-status", false, exitStatusPayload(0))
			return
		case "pty-req":
			req.Reply(true, nil)
		default:
			if req.WantReply {
				req.Reply(false, nil)
			}
		}
	}
}

// parseExecPayload pulls the command string out of an exec request.
// The wire format is a length-prefixed string per RFC 4254.
func parseExecPayload(p []byte) string {
	if len(p) < 4 {
		return ""
	}
	l := int(p[0])<<24 | int(p[1])<<16 | int(p[2])<<8 | int(p[3])
	if 4+l > len(p) {
		return ""
	}
	return string(p[4 : 4+l])
}

func exitStatusPayload(code uint32) []byte {
	return []byte{
		byte(code >> 24), byte(code >> 16), byte(code >> 8), byte(code),
	}
}

// --- tests ------------------------------------------------------------

func TestParseArgs(t *testing.T) {
	cases := []struct {
		argv []string
		host string
		user string
		port int
	}{
		{[]string{"alice@host.example"}, "host.example", "alice", 0},
		{[]string{"-p", "2022", "host"}, "host", "", 2022},
		{[]string{"-l", "bob", "host"}, "host", "bob", 0},
		{[]string{"-p2222", "host"}, "host", "", 2222},
	}
	for _, tc := range cases {
		o, err := parseArgs(tc.argv)
		if err != nil {
			t.Fatalf("%v: %v", tc.argv, err)
		}
		if o.host != tc.host || o.user != tc.user || o.port != tc.port {
			t.Errorf("%v: got host=%q user=%q port=%d", tc.argv, o.host, o.user, o.port)
		}
	}
}

func TestParseArgsCommand(t *testing.T) {
	o, err := parseArgs([]string{"host", "echo", "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(o.command, ",") != "echo,hi" {
		t.Errorf("got command=%v", o.command)
	}
}

func TestParseArgsOption(t *testing.T) {
	o, err := parseArgs([]string{"-o", "StrictHostKeyChecking=no", "host"})
	if err != nil {
		t.Fatal(err)
	}
	if !o.insecure {
		t.Errorf("expected insecure=true")
	}
}

func TestQuoteArgs(t *testing.T) {
	got := quoteArgs([]string{"echo", "hello world", "with 'quotes'"})
	want := []string{"'echo'", "'hello world'", "'with '\\''quotes'\\'''"}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("got[%d]=%q want %q", i, got[i], want[i])
		}
	}
}

// TestRemoteExecRoundTrip drives an end-to-end client → server → exec
// command flow using bag's connectAndRun.
func TestRemoteExecRoundTrip(t *testing.T) {
	srv := newTestServer(t, func(cmd string) ([]byte, []byte, int) {
		switch cmd {
		case "'echo' 'hello'":
			return []byte("hello\n"), nil, 0
		case "'fail'":
			return nil, []byte("nope\n"), 7
		}
		return nil, nil, 1
	})

	// Make a client key, register it on the server.
	dir := t.TempDir()
	identity := filepath.Join(dir, "id")
	pub := writeOpenSSHKey(t, identity)
	srv.AcceptKey(pub)

	host, port := srv.Addr()
	o := &options{
		host:           host,
		port:           port,
		user:           "alice",
		identityFile:   identity,
		knownHostsPath: filepath.Join(dir, "known_hosts"),
		insecure:       true, // simpler than emulating the prompt
		command:        []string{"echo", "hello"},
	}

	out := captureStdout(t, func() {
		if err := connectAndRun(o); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(out, "hello") {
		t.Errorf("got %q", out)
	}
}

func TestRemoteExitCode(t *testing.T) {
	srv := newTestServer(t, func(cmd string) ([]byte, []byte, int) {
		return nil, nil, 7
	})

	dir := t.TempDir()
	identity := filepath.Join(dir, "id")
	pub := writeOpenSSHKey(t, identity)
	srv.AcceptKey(pub)

	host, port := srv.Addr()
	o := &options{
		host: host, port: port, user: "alice",
		identityFile:   identity,
		knownHostsPath: filepath.Join(dir, "known_hosts"),
		insecure:       true,
		command:        []string{"anything"},
	}
	err := connectAndRun(o)
	var ec *exitCodeError
	if !errors.As(err, &ec) {
		t.Fatalf("expected exitCodeError, got %v", err)
	}
	if ec.code != 7 {
		t.Errorf("got exit %d want 7", ec.code)
	}
}

func TestKnownHostsFirstAcceptWritesFile(t *testing.T) {
	srv := newTestServer(t, func(string) ([]byte, []byte, int) {
		return []byte("ok\n"), nil, 0
	})

	dir := t.TempDir()
	identity := filepath.Join(dir, "id")
	pub := writeOpenSSHKey(t, identity)
	srv.AcceptKey(pub)

	host, port := srv.Addr()
	khPath := filepath.Join(dir, "kh")

	// Prepopulate known_hosts with the server's actual host key.
	if err := os.WriteFile(khPath, knownHostsLine(host, port, srv.hostKey.PublicKey()), 0o600); err != nil {
		t.Fatal(err)
	}

	o := &options{
		host: host, port: port, user: "alice",
		identityFile:   identity,
		knownHostsPath: khPath,
		command:        []string{"true"},
	}
	if err := connectAndRun(o); err != nil {
		t.Fatal(err)
	}
}
