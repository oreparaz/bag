package nc

import (
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func runNC(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	rIn, wIn, _ := os.Pipe()
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	oldIn, oldOut, oldErr := os.Stdin, os.Stdout, os.Stderr
	os.Stdin = rIn
	os.Stdout = wOut
	os.Stderr = wErr
	defer func() {
		os.Stdin, os.Stdout, os.Stderr = oldIn, oldOut, oldErr
		rIn.Close()
		rOut.Close()
		rErr.Close()
	}()
	go func() {
		wIn.WriteString(stdin)
		wIn.Close()
	}()
	exit := Main(args)
	wOut.Close()
	wErr.Close()
	out, _ := io.ReadAll(rOut)
	er, _ := io.ReadAll(rErr)
	return exit, string(out), string(er)
}

// echoServer returns a listening tcp address and a stop func.
func echoServer(t *testing.T) (string, func()) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c)
			}(c)
		}
	}()
	return l.Addr().String(), func() {
		l.Close()
		wg.Wait()
	}
}

func TestConnectAndEcho(t *testing.T) {
	addr, stop := echoServer(t)
	defer stop()
	host, port, _ := net.SplitHostPort(addr)
	exit, out, _ := runNC(t, "hello world\n", host, port)
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if !strings.Contains(out, "hello world") {
		t.Errorf("got %q", out)
	}
}

func TestVerbose(t *testing.T) {
	addr, stop := echoServer(t)
	defer stop()
	host, port, _ := net.SplitHostPort(addr)
	_, _, er := runNC(t, "x", "-v", host, port)
	if !strings.Contains(er, "succeeded") {
		t.Errorf("verbose stderr missing: %q", er)
	}
}

func TestZeroIOScan(t *testing.T) {
	addr, stop := echoServer(t)
	defer stop()
	host, port, _ := net.SplitHostPort(addr)
	_, out, _ := runNC(t, "", "-z", host, port)
	if !strings.Contains(out, "succeeded") {
		t.Errorf("scan output missing: %q", out)
	}
}

func TestScanRange(t *testing.T) {
	addr, stop := echoServer(t)
	defer stop()
	host, port, _ := net.SplitHostPort(addr)
	// Scan a small range around the open port. Most will fail; one
	// will succeed. We just need to see at least one "succeeded".
	pInt := portInt(port)
	if pInt == 0 {
		t.Skip("could not parse port")
	}
	lo := pInt
	hi := pInt + 2
	_, out, _ := runNC(t, "", "-z", host, lo2hi(lo, hi))
	if !strings.Contains(out, "succeeded") {
		t.Errorf("expected at least one succeeded line, got %q", out)
	}
}

func TestRefused(t *testing.T) {
	exit, _, er := runNC(t, "", "127.0.0.1", "1") // port 1 = closed
	if exit == 0 {
		t.Errorf("expected non-zero exit on connect failure")
	}
	if !strings.Contains(er, "nc:") {
		t.Errorf("expected diagnostic, got %s", er)
	}
}

func TestConnectTimeout(t *testing.T) {
	// 192.0.2.0/24 is the TEST-NET-1 block (RFC 5737), guaranteed
	// unroutable, so a connect attempt with a short timeout fails fast.
	t0 := time.Now()
	exit, _, _ := runNC(t, "", "-w", "1", "192.0.2.1", "12345")
	if exit == 0 {
		t.Errorf("expected timeout to fail")
	}
	if time.Since(t0) > 3*time.Second {
		t.Errorf("timeout took too long: %s", time.Since(t0))
	}
}

// lo2hi formats a port range as nc expects: "lo-hi".
func lo2hi(lo, hi int) string {
	return strings.Join([]string{itoa(lo), itoa(hi)}, "-")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
