package gpgcmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMatrixEncryptDecrypt enumerates every (algo × armor × size × source)
// combination for symmetric and public-key encrypt/decrypt and asserts
// the plaintext survives a bag↔gpg round-trip in both directions.
//
// Skipped when system gpg isn't installed.
func TestMatrixEncryptDecrypt(t *testing.T) {
	gpg := systemGPG()
	if gpg == "" {
		t.Skip("system gpg not available")
	}

	// Message variants.
	sizes := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"oneline", []byte("hello\n")},
		{"binary", append([]byte{0, 1, 2, 0xff, 0xfe}, bytes.Repeat([]byte{0x55, 0xaa}, 64)...)},
		{"longtext", bytes.Repeat([]byte("the quick brown fox jumps over the lazy dog\n"), 64)},
		{"4kb", bytes.Repeat([]byte("abcdefghijklmnopqrstuvwxyz"), 200)},
		{"utf8", []byte("héllo wörld — émojis 🎉🔐\n")},
	}

	// Symmetric matrix: cipher × armor × size, both directions.
	t.Run("symmetric", func(t *testing.T) {
		// Modern OpenPGP libraries (incl. ProtonMail's go-crypto) refuse
		// to *encrypt* with 3DES or CAST5 — both retired by RFC 9580.
		// bag can still *decrypt* messages produced with those ciphers
		// by old clients; the matrix only exercises algorithms still
		// allowed for outbound encryption.
		for _, cipher := range []string{"", "AES", "AES192", "AES256"} {
			for _, armored := range []bool{false, true} {
				for _, sz := range sizes {
					cipher, armored, sz := cipher, armored, sz
					name := fmt.Sprintf("%s/armor=%v/%s",
						labelOrDefault(cipher), armored, sz.name)
					t.Run(name, func(t *testing.T) {
						testSymmetricRoundTrip(t, gpg, sz.data, cipher, armored)
					})
				}
			}
		}
	})

	// Public-key matrix: key algo × armor × size, both directions.
	t.Run("publickey", func(t *testing.T) {
		for _, keyAlgo := range []string{"ed25519", "rsa2048", "ecdsa", "default"} {
			for _, armored := range []bool{false, true} {
				for _, sz := range sizes {
					keyAlgo, armored, sz := keyAlgo, armored, sz
					name := fmt.Sprintf("%s/armor=%v/%s", keyAlgo, armored, sz.name)
					t.Run(name, func(t *testing.T) {
						testPublicKeyRoundTrip(t, gpg, sz.data, keyAlgo, armored)
					})
				}
			}
		}
	})

	// Sign+verify matrix: key algo × sig style × direction.
	t.Run("signverify", func(t *testing.T) {
		for _, keyAlgo := range []string{"ed25519", "rsa2048", "ecdsa"} {
			for _, mode := range []string{"-s", "-b", "--clearsign"} {
				keyAlgo, mode := keyAlgo, mode
				name := fmt.Sprintf("%s/%s", keyAlgo, sigModeName(mode))
				t.Run(name, func(t *testing.T) {
					testSignVerifyRoundTrip(t, gpg, keyAlgo, mode)
				})
			}
		}
	})
}

func testSymmetricRoundTrip(t *testing.T, gpg string, plain []byte, cipher string, armored bool) {
	t.Helper()
	dir := t.TempDir()
	plainFile := writeTempFile(t, dir, "p", plain)
	pass := "mat" + cipher + "rix"

	// bag encrypts.
	bagCipher := filepath.Join(dir, "bag.gpg")
	args := []string{"--batch", "--passphrase-fd", "0", "-c"}
	if cipher != "" {
		args = append(args, "--cipher-algo", cipher)
	}
	if armored {
		args = append(args, "-a")
	}
	args = append(args, "--output", bagCipher, plainFile)
	exit, _, er := runBag(t, []byte(pass+"\n"), args...)
	if exit != 0 {
		t.Fatalf("bag -c: exit=%d stderr=%s", exit, er)
	}

	// system decrypts bag's output.
	cmd := exec.Command(gpg, "--batch", "--pinentry-mode", "loopback",
		"--passphrase-fd", "0", "--decrypt", bagCipher)
	cmd.Stdin = strings.NewReader(pass)
	got, err := cmd.Output()
	if err != nil {
		t.Fatalf("system gpg decrypt of bag output: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Errorf("bag→gpg mismatch: got %s want %s", fmtBytes(got), fmtBytes(plain))
	}

	// system encrypts.
	gpgCipher := filepath.Join(dir, "gpg.gpg")
	args = []string{"--batch", "--pinentry-mode", "loopback", "--passphrase-fd", "0",
		"--symmetric"}
	if cipher != "" {
		args = append(args, "--cipher-algo", cipher)
	}
	if armored {
		args = append(args, "-a")
	}
	args = append(args, "--output", gpgCipher, plainFile)
	cmd = exec.Command(gpg, args...)
	cmd.Stdin = strings.NewReader(pass)
	if err := cmd.Run(); err != nil {
		t.Fatalf("system gpg encrypt: %v", err)
	}

	// bag decrypts system's output.
	exit, got, er = runBag(t, []byte(pass+"\n"),
		"--batch", "--passphrase-fd", "0", "-d", "--output", "-", gpgCipher)
	if exit != 0 {
		t.Fatalf("bag -d: exit=%d stderr=%s", exit, er)
	}
	if !bytes.Equal(got, plain) {
		t.Errorf("gpg→bag mismatch: got %s want %s", fmtBytes(got), fmtBytes(plain))
	}
}

func testPublicKeyRoundTrip(t *testing.T, gpg string, plain []byte, keyAlgo string, armored bool) {
	t.Helper()
	dir := t.TempDir()
	home := filepath.Join(dir, "gnupg")
	os.MkdirAll(home, 0o700)
	t.Setenv("GNUPGHOME", home)

	// bag generates a key.
	uid := fmt.Sprintf("Mat <%s@x.io>", keyAlgo)
	exit, _, er := runBag(t, nil, "--batch", "--quick-gen-key", uid, keyAlgo)
	if exit != 0 {
		t.Fatalf("genkey: exit=%d stderr=%s", exit, er)
	}
	plainFile := writeTempFile(t, dir, "p", plain)

	// bag encrypts, system decrypts.
	bagCipher := filepath.Join(dir, "bag.gpg")
	encArgs := []string{"--batch", "--encrypt", "-r", "x.io"}
	if armored {
		encArgs = append(encArgs, "-a")
	}
	encArgs = append(encArgs, "--output", bagCipher, plainFile)
	exit, _, er = runBag(t, nil, encArgs...)
	if exit != 0 {
		t.Fatalf("bag --encrypt: exit=%d stderr=%s", exit, er)
	}
	cmd := exec.Command(gpg, "--homedir", home, "--batch",
		"--pinentry-mode", "loopback", "--decrypt", bagCipher)
	got, err := cmd.Output()
	if err != nil {
		t.Fatalf("system decrypt: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Errorf("bag→gpg mismatch %s", fmtBytes(got))
	}

	// system encrypts, bag decrypts. --trust-model always lets system
	// gpg encrypt to a key it has never seen a trust signature for —
	// without it, every Phase-3 encrypt fails with "Unusable public
	// key" because bag's --quick-gen-key doesn't populate trustdb.gpg
	// (a deliberate scope decision; trust DB is OOS for bag).
	gpgCipher := filepath.Join(dir, "gpg.gpg")
	encArgs2 := []string{"--homedir", home, "--batch",
		"--pinentry-mode", "loopback", "--trust-model", "always",
		"--encrypt", "-r", "x.io"}
	if armored {
		encArgs2 = append(encArgs2, "-a")
	}
	encArgs2 = append(encArgs2, "--output", gpgCipher, plainFile)
	if err := exec.Command(gpg, encArgs2...).Run(); err != nil {
		t.Fatalf("system encrypt: %v", err)
	}
	exit, got, er = runBag(t, nil, "-d", "--output", "-", gpgCipher)
	if exit != 0 {
		t.Fatalf("bag --decrypt: exit=%d stderr=%s", exit, er)
	}
	if !bytes.Equal(got, plain) {
		t.Errorf("gpg→bag mismatch %s", fmtBytes(got))
	}
}

func testSignVerifyRoundTrip(t *testing.T, gpg, keyAlgo, mode string) {
	t.Helper()
	dir := t.TempDir()
	home := filepath.Join(dir, "gnupg")
	os.MkdirAll(home, 0o700)
	t.Setenv("GNUPGHOME", home)

	uid := fmt.Sprintf("Sig <%s@x.io>", keyAlgo)
	exit, _, er := runBag(t, nil, "--batch", "--quick-gen-key", uid, keyAlgo)
	if exit != 0 {
		t.Fatalf("genkey: %d %s", exit, er)
	}
	plain := []byte("matrix sign verify\n")
	plainFile := writeTempFile(t, dir, "p", plain)

	// bag signs, both sides verify.
	bagSig := filepath.Join(dir, "bag.sig")
	exit, _, er = runBag(t, nil, "--batch", mode, "-a", "--output", bagSig, plainFile)
	if exit != 0 {
		t.Fatalf("bag sign: %d %s", exit, er)
	}
	verifyArgs := []string{"--verify", bagSig}
	if mode == "-b" {
		verifyArgs = append(verifyArgs, plainFile)
	}
	exit, _, er = runBag(t, nil, verifyArgs...)
	if exit != 0 || !bytes.Contains(er, []byte("Good signature")) {
		t.Errorf("bag verify of bag's %s: exit=%d stderr=%s", mode, exit, er)
	}

	gpgArgs := append([]string{"--homedir", home, "--verify"}, verifyArgs[1:]...)
	out, _ := exec.Command(gpg, gpgArgs...).CombinedOutput()
	if !bytes.Contains(out, []byte("Good signature")) {
		t.Errorf("gpg verify of bag's %s:\n%s", mode, out)
	}

	// system signs, bag verifies.
	gpgSig := filepath.Join(dir, "gpg.sig")
	signArgs := []string{"--homedir", home, "--batch",
		"--pinentry-mode", "loopback", "-a", "--output", gpgSig, mode, plainFile}
	if err := exec.Command(gpg, signArgs...).Run(); err != nil {
		t.Fatalf("gpg sign %s: %v", mode, err)
	}
	verifyArgs = []string{"--verify", gpgSig}
	if mode == "-b" {
		verifyArgs = append(verifyArgs, plainFile)
	}
	exit, _, er = runBag(t, nil, verifyArgs...)
	if exit != 0 || !bytes.Contains(er, []byte("Good signature")) {
		t.Errorf("bag verify of gpg's %s: exit=%d stderr=%s", mode, exit, er)
	}
}

func sigModeName(m string) string {
	switch m {
	case "-s":
		return "inline"
	case "-b":
		return "detached"
	case "--clearsign":
		return "clearsign"
	}
	return m
}

func labelOrDefault(s string) string {
	if s == "" {
		return "default"
	}
	return s
}

func writeTempFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}
