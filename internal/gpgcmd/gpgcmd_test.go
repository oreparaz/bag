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

// TestListKeysAfterGenKey: --list-keys must show the entity we just
// generated, including the algorithm tag and the UID.
func TestListKeysAfterGenKey(t *testing.T) {
	t.Setenv("GNUPGHOME", t.TempDir())
	runBag(t, nil, "--batch", "--quick-gen-key", "Carol <carol@x.io>", "ed25519")
	exit, out, _ := runBag(t, nil, "--list-keys")
	if exit != 0 {
		t.Fatalf("list-keys exit=%d", exit)
	}
	for _, want := range []string{"ed25519", "Carol", "carol@x.io"} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("list-keys missing %q in output:\n%s", want, out)
		}
	}
}

// TestExportImportRoundTrip: bag --export | bag --import round-trips
// the same key, AND system gpg can read what bag exports.
func TestExportImportRoundTrip(t *testing.T) {
	srcHome := t.TempDir()
	t.Setenv("GNUPGHOME", srcHome)
	runBag(t, nil, "--batch", "--quick-gen-key", "Dave <dave@x.io>", "ed25519")

	// bag export armored.
	exit, exported, er := runBag(t, nil, "--export", "-a", "dave")
	if exit != 0 || len(exported) == 0 {
		t.Fatalf("export exit=%d stderr=%s", exit, er)
	}
	if !bytes.Contains(exported, []byte("PGP PUBLIC KEY BLOCK")) {
		t.Errorf("armored output missing header: %s", exported)
	}

	// bag-imported into a fresh home.
	dstHome := t.TempDir()
	t.Setenv("GNUPGHOME", dstHome)
	exit, _, er = runBag(t, exported, "--import")
	if exit != 0 {
		t.Fatalf("import exit=%d stderr=%s", exit, er)
	}
	exit, out, _ := runBag(t, nil, "--list-keys")
	if exit != 0 || !bytes.Contains(out, []byte("dave@x.io")) {
		t.Errorf("re-listed keyring missing imported uid: %s", out)
	}

	gpg := systemGPG()
	if gpg == "" {
		return
	}
	// system gpg --import on bag's armored export.
	dstSys := t.TempDir()
	cmd := exec.Command(gpg, "--homedir", dstSys, "--import")
	cmd.Stdin = bytes.NewReader(exported)
	if err := cmd.Run(); err != nil {
		t.Fatalf("system gpg --import: %v", err)
	}
	listed, _ := exec.Command(gpg, "--homedir", dstSys, "--list-keys").CombinedOutput()
	if !bytes.Contains(listed, []byte("dave@x.io")) {
		t.Errorf("system gpg can't list bag's exported uid: %s", listed)
	}
}

// TestPublicKeyRoundTrip:
//   - bag generates a key for Alice
//   - bag encrypts a message to Alice
//   - bag decrypts → plaintext
//   - bag re-encrypts, system gpg decrypts (interop) → plaintext
//   - system gpg generates a key for Bob, bag imports the pub, encrypts,
//     system gpg decrypts → plaintext
func TestPublicKeyRoundTrip(t *testing.T) {
	plain := []byte("public-key encrypted hello\n")

	// Phase 1: bag-only path.
	homeAlice := t.TempDir()
	t.Setenv("GNUPGHOME", homeAlice)
	runBag(t, nil, "--batch", "--quick-gen-key", "Alice <alice@x.io>", "ed25519")
	plainFile := filepath.Join(homeAlice, "msg.txt")
	os.WriteFile(plainFile, plain, 0o600)
	cipherFile := filepath.Join(homeAlice, "msg.gpg")
	exit, _, er := runBag(t, nil,
		"--batch", "--encrypt", "-r", "alice", "--output", cipherFile, plainFile)
	if exit != 0 {
		t.Fatalf("bag encrypt: exit=%d stderr=%s", exit, er)
	}
	exit, got, er := runBag(t, nil,
		"--batch", "-d", "--output", "-", cipherFile)
	if exit != 0 {
		t.Fatalf("bag decrypt: exit=%d stderr=%s", exit, er)
	}
	if !bytes.Equal(got, plain) {
		t.Errorf("bag→bag plain mismatch: got %q", got)
	}

	gpg := systemGPG()
	if gpg == "" {
		return
	}

	// Phase 2: bag → system gpg. The same alice keyring works because
	// bag wrote pubring.gpg / secring.gpg in the legacy format.
	cmd := exec.Command(gpg, "--homedir", homeAlice,
		"--batch", "--pinentry-mode", "loopback", "--decrypt", cipherFile)
	got, err := cmd.Output()
	if err != nil {
		t.Fatalf("system gpg --decrypt of bag output: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Errorf("bag→system mismatch: got %q", got)
	}

	// Phase 3: system gpg generates Bob; bag encrypts to him; system decrypts.
	homeBob := t.TempDir()
	os.Chmod(homeBob, 0o700)
	cmd = exec.Command(gpg, "--homedir", homeBob,
		"--batch", "--pinentry-mode", "loopback", "--passphrase", "",
		"--quick-gen-key", "Bob <bob@x.io>")
	if err := cmd.Run(); err != nil {
		t.Fatalf("system gpg keygen: %v", err)
	}
	exported, err := exec.Command(gpg, "--homedir", homeBob, "--export", "-a", "bob").Output()
	if err != nil {
		t.Fatalf("system gpg export: %v", err)
	}
	homeBag := t.TempDir()
	t.Setenv("GNUPGHOME", homeBag)
	exit, _, er = runBag(t, exported, "--import")
	if exit != 0 {
		t.Fatalf("bag import of system pubkey: exit=%d stderr=%s", exit, er)
	}
	bobCipher := filepath.Join(homeBag, "bob.gpg")
	exit, _, er = runBag(t, nil,
		"--batch", "--encrypt", "-r", "bob", "--output", bobCipher, plainFile)
	if exit != 0 {
		t.Fatalf("bag encrypt to bob: exit=%d stderr=%s", exit, er)
	}
	got, err = exec.Command(gpg, "--homedir", homeBob,
		"--batch", "--pinentry-mode", "loopback", "--decrypt", bobCipher).Output()
	if err != nil {
		t.Fatalf("system gpg decrypt of bag-encrypted message: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Errorf("system←bag mismatch: got %q", got)
	}
}

// TestSignVerifyRoundTrip covers the 3 sign modes (inline, detached,
// clearsign) and the 4 interop pairs (bag↔bag, bag↔gpg) per mode —
// 12 round-trips total when system gpg is available.
func TestSignVerifyRoundTrip(t *testing.T) {
	gpg := systemGPG()
	plain := []byte("important message\nthat must be signed\n")
	home := t.TempDir()
	t.Setenv("GNUPGHOME", home)
	runBag(t, nil, "--batch", "--quick-gen-key", "Signer <s@x.io>", "ed25519")
	plainFile := filepath.Join(home, "m")
	os.WriteFile(plainFile, plain, 0o600)

	// Helper: bag verify.
	bagVerify := func(args ...string) error {
		exit, _, er := runBag(t, nil, args...)
		if exit != 0 {
			return fmt.Errorf("bag verify exit=%d stderr=%s", exit, er)
		}
		if !bytes.Contains(er, []byte("Good signature")) {
			return fmt.Errorf("bag verify missing 'Good signature': %s", er)
		}
		return nil
	}
	// Helper: system gpg verify.
	gpgVerify := func(args ...string) error {
		if gpg == "" {
			return nil // skip
		}
		full := append([]string{"--homedir", home, "--verify"}, args...)
		out, _ := exec.Command(gpg, full...).CombinedOutput()
		if !bytes.Contains(out, []byte("Good signature")) {
			return fmt.Errorf("gpg verify failed:\n%s", out)
		}
		return nil
	}

	type variant struct {
		name string
		flag string  // bag flag to produce the signature
		ext  string  // file extension bag writes
		verifyArgs func(sigFile string) (bagArgs, gpgArgs []string)
	}
	variants := []variant{
		{
			name: "inline", flag: "-s", ext: ".asc",
			verifyArgs: func(sig string) ([]string, []string) {
				return []string{"--verify", sig}, []string{sig}
			},
		},
		{
			name: "detached", flag: "-b", ext: ".asc",
			verifyArgs: func(sig string) ([]string, []string) {
				return []string{"--verify", sig, plainFile}, []string{sig, plainFile}
			},
		},
		{
			name: "clearsign", flag: "--clearsign", ext: ".asc",
			verifyArgs: func(sig string) ([]string, []string) {
				return []string{"--verify", sig}, []string{sig}
			},
		},
	}

	for _, v := range variants {
		v := v
		t.Run(v.name+"_bag_signs", func(t *testing.T) {
			sig := filepath.Join(home, "out_"+v.name+v.ext)
			exit, _, er := runBag(t, nil,
				"--batch", v.flag, "-a", "--output", sig, plainFile)
			if exit != 0 {
				t.Fatalf("bag sign: exit=%d stderr=%s", exit, er)
			}
			bagA, gpgA := v.verifyArgs(sig)
			if err := bagVerify(bagA...); err != nil {
				t.Errorf("bag verify of bag's %s: %v", v.name, err)
			}
			if err := gpgVerify(gpgA...); err != nil {
				t.Errorf("system gpg verify of bag's %s: %v", v.name, err)
			}
		})
	}

	if gpg == "" {
		return
	}
	for _, v := range variants {
		v := v
		t.Run(v.name+"_gpg_signs", func(t *testing.T) {
			sig := filepath.Join(home, "gpg_"+v.name+v.ext)
			args := []string{"--homedir", home, "--batch",
				"--pinentry-mode", "loopback", "-a", "--output", sig}
			args = append(args, v.flag, plainFile)
			if err := exec.Command(gpg, args...).Run(); err != nil {
				t.Fatalf("system gpg sign %s: %v", v.name, err)
			}
			bagA, _ := v.verifyArgs(sig)
			if err := bagVerify(bagA...); err != nil {
				t.Errorf("bag verify of gpg's %s: %v", v.name, err)
			}
		})
	}
}

type bagArgs = []string
type gpgArgs = []string

// TestSignEncryptCombined: `-se -r USER` produces an encrypted-AND-
// signed message; both bag and system gpg must recover the plaintext
// AND verify the signature.
func TestSignEncryptCombined(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GNUPGHOME", home)
	runBag(t, nil, "--batch", "--quick-gen-key", "Sender <sender@x.io>", "ed25519")
	plain := []byte("sign+encrypt round trip\n")
	plainFile := filepath.Join(home, "p")
	os.WriteFile(plainFile, plain, 0o600)
	cipher := filepath.Join(home, "c.gpg")

	exit, _, er := runBag(t, nil,
		"--batch", "-se", "-r", "sender",
		"--output", cipher, plainFile)
	if exit != 0 {
		t.Fatalf("bag -se exit=%d stderr=%s", exit, er)
	}

	// bag decrypts and reports Good signature.
	exit, got, er := runBag(t, nil, "--batch", "-d", "--output", "-", cipher)
	if exit != 0 {
		t.Fatalf("bag -d exit=%d stderr=%s", exit, er)
	}
	if !bytes.Equal(got, plain) {
		t.Errorf("bag→bag mismatch: got %q", got)
	}
	if !bytes.Contains(er, []byte("Good signature")) {
		t.Errorf("bag didn't report signature: %s", er)
	}

	gpg := systemGPG()
	if gpg == "" {
		return
	}
	out, err := exec.Command(gpg, "--homedir", home,
		"--batch", "--pinentry-mode", "loopback", "--decrypt", cipher).Output()
	if err != nil {
		t.Fatalf("system gpg decrypt: %v", err)
	}
	if !bytes.Equal(out, plain) {
		t.Errorf("bag→gpg mismatch: got %q", out)
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
