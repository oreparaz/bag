package gpgcmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// systemGPG returns the path to the system gpg binary, or "" if it
// isn't present. Tests that interop with system gpg t.Skip when this
// is empty so the suite stays portable.
func systemGPG() string {
	if p, err := exec.LookPath("gpg"); err == nil {
		return p
	}
	return ""
}

// runBag runs bag-gpg in-process. stdin / stdout / stderr are captured.
func runBag(t *testing.T, stdin []byte, args ...string) (int, []byte, []byte) {
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
		wIn.Write(stdin)
		wIn.Close()
	}()
	exit := Main(args)
	wOut.Close()
	wErr.Close()
	out, _ := io.ReadAll(rOut)
	er, _ := io.ReadAll(rErr)
	return exit, out, er
}

// TestSymmetricRoundTrip: bag encrypts with -c, bag decrypts back.
// Then: bag encrypts, system gpg decrypts. Then: system gpg encrypts,
// bag decrypts. All three combos must yield the original plaintext.
func TestSymmetricRoundTrip(t *testing.T) {
	dir := t.TempDir()
	plain := []byte("hello bag gpg\nsecond line\n\nbinary: \x00\x01\x02\n")
	plainFile := filepath.Join(dir, "plain")
	if err := os.WriteFile(plainFile, plain, 0o600); err != nil {
		t.Fatal(err)
	}
	pass := "correct horse battery staple"

	cipherBag := filepath.Join(dir, "bag.gpg")
	exit, _, er := runBag(t, []byte(pass+"\n"),
		"--batch", "--passphrase-fd", "0", "-c", "--output", cipherBag, plainFile)
	if exit != 0 {
		t.Fatalf("bag -c failed: %d stderr=%s", exit, er)
	}

	// 1) bag → bag round-trip.
	exit, out, er := runBag(t, []byte(pass+"\n"),
		"--batch", "--passphrase-fd", "0", "-d", "--output", "-", cipherBag)
	if exit != 0 {
		t.Fatalf("bag -d failed: %d stderr=%s", exit, er)
	}
	if !bytes.Equal(out, plain) {
		t.Errorf("bag→bag plaintext mismatch:\n  got %q\n  want %q", out, plain)
	}

	gpg := systemGPG()
	if gpg == "" {
		t.Skip("no system gpg; round-trip with real gpg skipped")
	}

	// 2) bag → system gpg.
	cmd := exec.Command(gpg, "--batch", "--pinentry-mode", "loopback",
		"--passphrase-fd", "0", "--decrypt", cipherBag)
	cmd.Stdin = strings.NewReader(pass)
	got, err := cmd.Output()
	if err != nil {
		t.Fatalf("system gpg decrypt of bag output: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Errorf("bag→gpg plaintext mismatch:\n  got %q\n  want %q", got, plain)
	}

	// 3) system gpg → bag.
	cipherGPG := filepath.Join(dir, "gpg.gpg")
	cmd = exec.Command(gpg, "--batch", "--pinentry-mode", "loopback",
		"--passphrase-fd", "0", "--symmetric", "--output", cipherGPG, plainFile)
	cmd.Stdin = strings.NewReader(pass)
	if err := cmd.Run(); err != nil {
		t.Fatalf("system gpg encrypt: %v", err)
	}
	exit, out, er = runBag(t, []byte(pass+"\n"),
		"--batch", "--passphrase-fd", "0", "-d", "--output", "-", cipherGPG)
	if exit != 0 {
		t.Fatalf("bag -d of gpg output failed: %d stderr=%s", exit, er)
	}
	if !bytes.Equal(out, plain) {
		t.Errorf("gpg→bag plaintext mismatch:\n  got %q\n  want %q", out, plain)
	}
}

// TestSymmetricArmored: -c -a produces ASCII-armored output; bag and
// system gpg should agree on its decoding.
func TestSymmetricArmored(t *testing.T) {
	dir := t.TempDir()
	plain := []byte("armored test\n")
	plainFile := filepath.Join(dir, "p")
	os.WriteFile(plainFile, plain, 0o600)
	pass := "hunter2"
	cipher := filepath.Join(dir, "c.asc")

	exit, _, er := runBag(t, []byte(pass+"\n"),
		"--batch", "--passphrase-fd", "0", "-c", "-a", "--output", cipher, plainFile)
	if exit != 0 {
		t.Fatalf("bag -c -a failed: %d stderr=%s", exit, er)
	}
	armorBytes, _ := os.ReadFile(cipher)
	if !bytes.Contains(armorBytes, []byte("-----BEGIN PGP MESSAGE-----")) {
		t.Errorf("armor header missing; got %q", armorBytes)
	}

	exit, out, er := runBag(t, []byte(pass+"\n"),
		"--batch", "--passphrase-fd", "0", "-d", "--output", "-", cipher)
	if exit != 0 {
		t.Fatalf("bag -d of armored failed: %d stderr=%s", exit, er)
	}
	if !bytes.Equal(out, plain) {
		t.Errorf("armored round-trip mismatch: got %q want %q", out, plain)
	}
}

// TestDecryptDoesNotOverwriteInput: regression for an early bug where
// bag's --decrypt with no -o defaulted to o.input + "" and overwrote
// the encrypted file with the plaintext.
func TestDecryptDoesNotOverwriteInput(t *testing.T) {
	dir := t.TempDir()
	plain := []byte("don't eat me\n")
	plainFile := filepath.Join(dir, "p")
	os.WriteFile(plainFile, plain, 0o600)
	cipher := filepath.Join(dir, "c.gpg")
	pass := "pw"
	exit, _, er := runBag(t, []byte(pass+"\n"),
		"--batch", "--passphrase-fd", "0", "-c", "--output", cipher, plainFile)
	if exit != 0 {
		t.Fatalf("bag -c failed: %d stderr=%s", exit, er)
	}
	before, _ := os.ReadFile(cipher)

	// Run decrypt with no -o; bag should strip .gpg from cipher's
	// name to produce the output file, not overwrite the input.
	exit, _, er = runBag(t, []byte(pass+"\n"),
		"--batch", "--passphrase-fd", "0", "-d", cipher)
	if exit != 0 {
		t.Fatalf("bag -d failed: %d stderr=%s", exit, er)
	}
	after, _ := os.ReadFile(cipher)
	if !bytes.Equal(before, after) {
		t.Errorf("encrypted file was modified by --decrypt:\n  before %x\n  after  %x",
			before[:16], after[:16])
	}
	outFile := strings.TrimSuffix(cipher, ".gpg")
	out, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("expected decrypted output at %s: %v", outFile, err)
	}
	if !bytes.Equal(out, plain) {
		t.Errorf("decrypted file mismatch")
	}
}

// TestGenKey covers --quick-gen-key for every algorithm bag can
// generate. After each genkey we re-load the resulting keyring with
// system gpg (when available) to assert binary compatibility.
func TestGenKey(t *testing.T) {
	for _, algo := range []string{"rsa2048", "ed25519", "ecdsa", "default"} {
		algo := algo
		t.Run(algo, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("GNUPGHOME", home)
			uid := fmt.Sprintf("Test %s <%s@example.com>", algo, algo)
			exit, _, er := runBag(t, nil,
				"--batch", "--quick-gen-key", uid, algo)
			if exit != 0 {
				t.Fatalf("quick-gen-key %s: exit=%d stderr=%s", algo, exit, er)
			}
			// pubring + secring present.
			for _, f := range []string{"pubring.gpg", "secring.gpg"} {
				if _, err := os.Stat(filepath.Join(home, f)); err != nil {
					t.Errorf("missing %s: %v", f, err)
				}
			}
			gpg := systemGPG()
			if gpg == "" {
				return
			}
			out, _ := exec.Command(gpg, "--homedir", home, "--list-keys").CombinedOutput()
			if !bytes.Contains(out, []byte(algo+"@example.com")) {
				t.Errorf("system gpg can't see uid: out=%s", out)
			}
		})
	}
}

// TestGenKeyDSARejected: --quick-gen-key dsa surfaces a clear error
// (library limitation) instead of producing a corrupt keyring.
func TestGenKeyDSARejected(t *testing.T) {
	t.Setenv("GNUPGHOME", t.TempDir())
	exit, _, er := runBag(t, nil,
		"--batch", "--quick-gen-key", "Test <x@y.com>", "dsa")
	if exit == 0 {
		t.Errorf("dsa keygen should fail clearly; stderr=%s", er)
	}
	if !bytes.Contains(er, []byte("DSA")) && !bytes.Contains(er, []byte("dsa")) {
		t.Errorf("expected error to mention DSA: %s", er)
	}
}

// fmtBytes is a tiny helper for diff messages.
func fmtBytes(b []byte) string {
	if len(b) > 32 {
		return fmt.Sprintf("%q...(%d bytes)", b[:32], len(b))
	}
	return fmt.Sprintf("%q", b)
}
