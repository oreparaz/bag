package scp

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	xssh "golang.org/x/crypto/ssh"
)

// --- in-memory ssh server ---------------------------------------------

// scpServer is a tiny ssh server that serves a single exec request:
// either `scp -t* TARGET` (we receive into a directory) or
// `scp -f* SOURCE` (we send from a directory).
type scpServer struct {
	listener net.Listener
	host     xssh.Signer
	authPub  xssh.PublicKey
	root     string // local fs root used as the remote's filesystem
	stop     chan struct{}
	wg       sync.WaitGroup
}

func newSCPServer(t *testing.T, root string) *scpServer {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := xssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	s := &scpServer{
		listener: ln,
		host:     signer,
		root:     root,
		stop:     make(chan struct{}),
	}
	s.wg.Add(1)
	go s.serve()
	t.Cleanup(s.Close)
	return s
}

func (s *scpServer) Addr() (host string, port int) {
	a := s.listener.Addr().(*net.TCPAddr)
	return a.IP.String(), a.Port
}

func (s *scpServer) AcceptKey(pub xssh.PublicKey) { s.authPub = pub }

func (s *scpServer) Close() {
	close(s.stop)
	s.listener.Close()
	s.wg.Wait()
}

func (s *scpServer) serve() {
	defer s.wg.Done()
	cfg := &xssh.ServerConfig{
		PublicKeyCallback: func(c xssh.ConnMetadata, key xssh.PublicKey) (*xssh.Permissions, error) {
			if s.authPub != nil && bytes.Equal(s.authPub.Marshal(), key.Marshal()) {
				return nil, nil
			}
			return nil, fmt.Errorf("unauthorized")
		},
	}
	cfg.AddHostKey(s.host)

	for {
		c, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handle(c, cfg)
	}
}

func (s *scpServer) handle(c net.Conn, cfg *xssh.ServerConfig) {
	defer c.Close()
	c.SetDeadline(time.Now().Add(30 * time.Second))
	srv, chans, reqs, err := xssh.NewServerConn(c, cfg)
	if err != nil {
		return
	}
	defer srv.Close()
	go xssh.DiscardRequests(reqs)

	for nc := range chans {
		if nc.ChannelType() != "session" {
			nc.Reject(xssh.UnknownChannelType, "")
			continue
		}
		ch, sreqs, err := nc.Accept()
		if err != nil {
			continue
		}
		go s.session(ch, sreqs)
	}
}

func (s *scpServer) session(ch xssh.Channel, reqs <-chan *xssh.Request) {
	defer ch.Close()
	for req := range reqs {
		if req.Type != "exec" {
			if req.WantReply {
				req.Reply(false, nil)
			}
			continue
		}
		req.Reply(true, nil)
		cmd := parseExecPayload(req.Payload)
		exit := s.runSCP(ch, cmd)
		ch.SendRequest("exit-status", false, exitStatusPayload(uint32(exit)))
		return
	}
}

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
	return []byte{byte(code >> 24), byte(code >> 16), byte(code >> 8), byte(code)}
}

// runSCP parses the exec line, dispatches to scp -t / -f, and returns
// the exit code.
func (s *scpServer) runSCP(ch xssh.Channel, cmd string) int {
	parts := strings.Fields(cmd)
	if len(parts) < 3 || parts[0] != "scp" {
		fmt.Fprintf(ch.Stderr(), "unexpected exec %q\n", cmd)
		return 1
	}
	flags := parts[1]
	target := strings.Trim(parts[2], "'")
	full := filepath.Join(s.root, filepath.Clean("/"+target))
	switch {
	case strings.Contains(flags, "t"):
		return s.scpReceive(ch, full, strings.Contains(flags, "p"))
	case strings.Contains(flags, "f"):
		return s.scpSend(ch, full, strings.Contains(flags, "r"), strings.Contains(flags, "p"))
	}
	return 1
}

// scpReceive: -t. Reads C/D/E records from the client, materializing
// files under root.
func (s *scpServer) scpReceive(ch xssh.Channel, target string, preserveTimes bool) int {
	r := bufio.NewReader(ch)
	w := ch
	// Initial OK.
	w.Write([]byte{0})

	// If target doesn't exist, we'll write a single file there;
	// if it does and is a dir, we drop entries inside.
	stack := []string{}
	if fi, err := os.Stat(target); err == nil && fi.IsDir() {
		stack = append(stack, target)
	}
	pendingTimes := timesPair{}

	for {
		first, err := r.ReadByte()
		if err == io.EOF {
			return 0
		}
		if err != nil {
			return 1
		}
		switch first {
		case 'C', 'D':
			line, err := r.ReadString('\n')
			if err != nil {
				return 1
			}
			line = strings.TrimRight(line, "\n")
			parts := strings.SplitN(line, " ", 3)
			mode64, _ := strconv.ParseInt(parts[0], 8, 32)
			size64, _ := strconv.ParseInt(parts[1], 10, 64)
			name := parts[2]

			var dest string
			if len(stack) > 0 {
				dest = filepath.Join(stack[len(stack)-1], name)
			} else {
				dest = target
			}

			if first == 'D' {
				if err := os.MkdirAll(dest, os.FileMode(mode64)); err != nil {
					return 1
				}
				stack = append(stack, dest)
				w.Write([]byte{0})
				pendingTimes = timesPair{}
				continue
			}
			// 'C' file.
			os.MkdirAll(filepath.Dir(dest), 0o755)
			f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, os.FileMode(mode64))
			if err != nil {
				return 1
			}
			w.Write([]byte{0})
			if _, err := io.CopyN(f, r, size64); err != nil {
				f.Close()
				return 1
			}
			f.Close()
			end, _ := r.ReadByte()
			if end != 0 {
				return 1
			}
			w.Write([]byte{0})
			if preserveTimes && pendingTimes.mtime > 0 {
				_ = setTimes(dest, pendingTimes)
			}
			pendingTimes = timesPair{}
		case 'E':
			r.ReadString('\n')
			if len(stack) == 0 {
				return 1
			}
			stack = stack[:len(stack)-1]
			w.Write([]byte{0})
		case 'T':
			line, _ := r.ReadString('\n')
			parts := strings.Fields(strings.TrimRight(line, "\n"))
			if len(parts) >= 4 {
				mt, _ := strconv.ParseInt(parts[0], 10, 64)
				at, _ := strconv.ParseInt(parts[2], 10, 64)
				pendingTimes = timesPair{mtime: mt, atime: at}
			}
			w.Write([]byte{0})
		default:
			return 1
		}
	}
}

// scpSend: -f. Walks target and emits records.
func (s *scpServer) scpSend(ch xssh.Channel, source string, recursive, preserve bool) int {
	r := bufio.NewReader(ch)
	w := ch
	if _, err := r.ReadByte(); err != nil { // initial client ack
		return 1
	}

	info, err := os.Lstat(source)
	if err != nil {
		fmt.Fprintf(ch.Stderr(), "%s: %v\n", source, err)
		w.Write([]byte("\x01remote error\n"))
		return 1
	}

	if info.IsDir() {
		if !recursive {
			fmt.Fprintf(ch.Stderr(), "%s: is a directory\n", source)
			return 1
		}
		return sendDir(r, w, source, info, preserve)
	}
	return sendFile(r, w, source, info, preserve)
}

func sendDir(r *bufio.Reader, w io.Writer, dir string, info os.FileInfo, preserve bool) int {
	if preserve {
		mtime := info.ModTime().Unix()
		fmt.Fprintf(w, "T%d 0 %d 0\n", mtime, mtime)
		if _, err := r.ReadByte(); err != nil {
			return 1
		}
	}
	mode := info.Mode().Perm()
	fmt.Fprintf(w, "D%04o 0 %s\n", mode, filepath.Base(dir))
	if _, err := r.ReadByte(); err != nil {
		return 1
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 1
	}
	for _, de := range entries {
		ci, _ := os.Lstat(filepath.Join(dir, de.Name()))
		if ci == nil {
			continue
		}
		var rc int
		if ci.IsDir() {
			rc = sendDir(r, w, filepath.Join(dir, de.Name()), ci, preserve)
		} else {
			rc = sendFile(r, w, filepath.Join(dir, de.Name()), ci, preserve)
		}
		if rc != 0 {
			return rc
		}
	}
	fmt.Fprint(w, "E\n")
	if _, err := r.ReadByte(); err != nil {
		return 1
	}
	return 0
}

func sendFile(r *bufio.Reader, w io.Writer, path string, info os.FileInfo, preserve bool) int {
	if preserve {
		mtime := info.ModTime().Unix()
		fmt.Fprintf(w, "T%d 0 %d 0\n", mtime, mtime)
		if _, err := r.ReadByte(); err != nil {
			return 1
		}
	}
	mode := info.Mode().Perm()
	fmt.Fprintf(w, "C%04o %d %s\n", mode, info.Size(), filepath.Base(path))
	if _, err := r.ReadByte(); err != nil {
		return 1
	}
	f, err := os.Open(path)
	if err != nil {
		return 1
	}
	defer f.Close()
	io.Copy(w, f)
	w.Write([]byte{0})
	if _, err := r.ReadByte(); err != nil {
		return 1
	}
	return 0
}

// --- helpers (key generation) ----------------------------------------

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

// --- tests -----------------------------------------------------------

func TestParseEndpoint(t *testing.T) {
	cases := []struct {
		in     string
		remote bool
		user   string
		host   string
		path   string
	}{
		{"file.txt", false, "", "", "file.txt"},
		{"./a:b", false, "", "", "./a:b"}, // colon after slash → local
		{"host:path", true, "", "host", "path"},
		{"alice@host:/tmp/file", true, "alice", "host", "/tmp/file"},
		{"host:", true, "", "host", ""},
	}
	for _, c := range cases {
		ep := parseEndpoint(c.in)
		if ep.isRemote() != c.remote || ep.user != c.user || ep.host != c.host || ep.path != c.path {
			t.Errorf("%q: got %+v", c.in, ep)
		}
	}
}

func TestUploadSingleFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(src, []byte("hello scp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	remoteRoot := t.TempDir()
	srv := newSCPServer(t, remoteRoot)
	identity := filepath.Join(dir, "id")
	pub := writeOpenSSHKey(t, identity)
	srv.AcceptKey(pub)
	host, port := srv.Addr()

	o := &options{
		srcs:        []endpoint{{path: src}},
		dst:         endpoint{host: host, path: "/dest.txt"},
		port:        port,
		identityKey: identity,
		insecure:    true,
		knownHosts:  filepath.Join(dir, "known_hosts"),
	}
	if err := dispatch(o); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(remoteRoot, "dest.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "hello scp\n" {
		t.Errorf("got %q", body)
	}
}

func TestUploadIntoDir(t *testing.T) {
	local := t.TempDir()
	src := filepath.Join(local, "a.txt")
	os.WriteFile(src, []byte("A\n"), 0o644)

	remoteRoot := t.TempDir()
	// Pre-create a "dest" directory; the protocol pushes 'a.txt' into it.
	os.MkdirAll(filepath.Join(remoteRoot, "dest"), 0o755)
	srv := newSCPServer(t, remoteRoot)
	identity := filepath.Join(local, "id")
	pub := writeOpenSSHKey(t, identity)
	srv.AcceptKey(pub)
	host, port := srv.Addr()

	o := &options{
		srcs:        []endpoint{{path: src}},
		dst:         endpoint{host: host, path: "/dest"},
		port:        port,
		identityKey: identity,
		insecure:    true,
		knownHosts:  filepath.Join(local, "kh"),
	}
	if err := dispatch(o); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(remoteRoot, "dest", "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "A\n" {
		t.Errorf("got %q", body)
	}
}

func TestUploadRecursiveDir(t *testing.T) {
	local := t.TempDir()
	srcRoot := filepath.Join(local, "tree")
	if err := os.MkdirAll(filepath.Join(srcRoot, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(srcRoot, "top.txt"), []byte("top\n"), 0o644)
	os.WriteFile(filepath.Join(srcRoot, "sub", "deep.txt"), []byte("deep\n"), 0o644)

	remoteRoot := t.TempDir()
	os.MkdirAll(filepath.Join(remoteRoot, "dest"), 0o755)
	srv := newSCPServer(t, remoteRoot)
	identity := filepath.Join(local, "id")
	pub := writeOpenSSHKey(t, identity)
	srv.AcceptKey(pub)
	host, port := srv.Addr()

	o := &options{
		srcs:        []endpoint{{path: srcRoot}},
		dst:         endpoint{host: host, path: "/dest"},
		port:        port,
		identityKey: identity,
		insecure:    true,
		recursive:   true,
		knownHosts:  filepath.Join(local, "kh"),
	}
	if err := dispatch(o); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		filepath.Join(remoteRoot, "dest", "tree", "top.txt"),
		filepath.Join(remoteRoot, "dest", "tree", "sub", "deep.txt"),
	} {
		if _, err := os.Stat(want); err != nil {
			t.Errorf("missing %s: %v", want, err)
		}
	}
}

func TestDownloadSingleFile(t *testing.T) {
	remote := t.TempDir()
	os.WriteFile(filepath.Join(remote, "src.txt"), []byte("from server\n"), 0o644)

	srv := newSCPServer(t, remote)
	local := t.TempDir()
	identity := filepath.Join(local, "id")
	pub := writeOpenSSHKey(t, identity)
	srv.AcceptKey(pub)
	host, port := srv.Addr()

	dst := filepath.Join(local, "out.txt")
	o := &options{
		srcs:        []endpoint{{host: host, path: "/src.txt"}},
		dst:         endpoint{path: dst},
		port:        port,
		identityKey: identity,
		insecure:    true,
		knownHosts:  filepath.Join(local, "kh"),
	}
	if err := dispatch(o); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "from server\n" {
		t.Errorf("got %q", body)
	}
}

func TestDownloadRecursive(t *testing.T) {
	remote := t.TempDir()
	os.MkdirAll(filepath.Join(remote, "tree", "sub"), 0o755)
	os.WriteFile(filepath.Join(remote, "tree", "a.txt"), []byte("AAA\n"), 0o644)
	os.WriteFile(filepath.Join(remote, "tree", "sub", "b.txt"), []byte("BBB\n"), 0o644)

	srv := newSCPServer(t, remote)
	local := t.TempDir()
	identity := filepath.Join(local, "id")
	pub := writeOpenSSHKey(t, identity)
	srv.AcceptKey(pub)
	host, port := srv.Addr()

	o := &options{
		srcs:        []endpoint{{host: host, path: "/tree"}},
		dst:         endpoint{path: local},
		port:        port,
		identityKey: identity,
		insecure:    true,
		recursive:   true,
		knownHosts:  filepath.Join(local, "kh"),
	}
	if err := dispatch(o); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		filepath.Join(local, "tree", "a.txt"),
		filepath.Join(local, "tree", "sub", "b.txt"),
	} {
		if _, err := os.Stat(want); err != nil {
			t.Errorf("missing %s: %v", want, err)
		}
	}
}

// TestRefuseTraversalOnDownload: a malicious server that sends
// 'C0644 5 ../escape.txt' must NOT cause us to write outside the
// destination tree. We spin up a custom evil sshd that emits the bad
// header and confirm the file does not appear in the parent dir.
func TestRefuseTraversalOnDownload(t *testing.T) {
	local := t.TempDir()
	identity := filepath.Join(local, "id")
	pub := writeOpenSSHKey(t, identity)

	// Build a host signer for the evil server.
	_, hostPriv, _ := ed25519.GenerateKey(rand.Reader)
	hostSig, err := xssh.NewSignerFromKey(hostPriv)
	if err != nil {
		t.Fatal(err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go runEvilServer(ln, hostSig, pub)
	addr := ln.Addr().(*net.TCPAddr)
	host, port := addr.IP.String(), addr.Port

	dst := filepath.Join(local, "dest")
	os.MkdirAll(dst, 0o755)
	o := &options{
		srcs:        []endpoint{{host: host, path: "/whatever"}},
		dst:         endpoint{path: dst},
		port:        port,
		identityKey: identity,
		insecure:    true,
		knownHosts:  filepath.Join(local, "kh"),
	}
	err = dispatch(o)
	if err == nil {
		t.Errorf("expected refusal of evil header")
	}
	if _, err := os.Stat(filepath.Join(local, "escape.txt")); err == nil {
		t.Errorf("file escaped to %s", filepath.Join(local, "escape.txt"))
	}
}

// runEvilServer simulates a malicious sshd that, on `scp -f`, sends a
// path-traversal entry name.
func runEvilServer(ln net.Listener, host xssh.Signer, authPub xssh.PublicKey) {
	c, err := ln.Accept()
	if err != nil {
		return
	}
	defer c.Close()
	cfg := &xssh.ServerConfig{
		PublicKeyCallback: func(_ xssh.ConnMetadata, key xssh.PublicKey) (*xssh.Permissions, error) {
			if bytes.Equal(authPub.Marshal(), key.Marshal()) {
				return nil, nil
			}
			return nil, fmt.Errorf("denied")
		},
	}
	cfg.AddHostKey(host)
	srv, chans, reqs, err := xssh.NewServerConn(c, cfg)
	if err != nil {
		return
	}
	defer srv.Close()
	go xssh.DiscardRequests(reqs)
	nc := <-chans
	if nc == nil {
		return
	}
	if nc.ChannelType() != "session" {
		nc.Reject(xssh.UnknownChannelType, "")
		return
	}
	ch, sreqs, _ := nc.Accept()
	defer ch.Close()
	for req := range sreqs {
		if req.Type != "exec" {
			req.Reply(false, nil)
			continue
		}
		req.Reply(true, nil)
		r := bufio.NewReader(ch)
		// initial client ack
		_, _ = r.ReadByte()
		fmt.Fprint(ch, "C0644 4 ../escape.txt\n")
		_, _ = r.ReadByte()
		ch.Write([]byte("DATA"))
		ch.Write([]byte{0})
		_, _ = r.ReadByte()
		ch.SendRequest("exit-status", false, exitStatusPayload(0))
		return
	}
}
