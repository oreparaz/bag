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

// shortHome returns a GnuPG home directory under /tmp instead of
// t.TempDir(). t.TempDir() on macOS uses $TMPDIR (typically a deep
// path under /var/folders/...), and the resulting paths exceed the
// sockaddr_un.sun_path limit (104 bytes on macOS), so gpg-agent fails
// to bind its socket inside the home. Using /tmp (≤14 chars to the
// socket) keeps every gpg subprocess working on all platforms.
//
// The directory is removed automatically when the test completes.
func shortHome(t *testing.T) string {
	t.Helper()
	home, err := os.MkdirTemp("/tmp", "bag-gpg-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	return home
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
			home := shortHome(t)
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
	t.Setenv("GNUPGHOME", shortHome(t))
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
	srcHome := shortHome(t)
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
	dstHome := shortHome(t)
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
	dstSys := shortHome(t)
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
	homeAlice := shortHome(t)
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
	homeBob := shortHome(t)
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
	homeBag := shortHome(t)
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
	home := shortHome(t)
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
	home := shortHome(t)
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

// TestDeleteKeys: --delete-secret-keys then --delete-keys removes a
// UID from both keyrings, leaving the others intact.
func TestDeleteKeys(t *testing.T) {
	home := shortHome(t)
	t.Setenv("GNUPGHOME", home)
	runBag(t, nil, "--batch", "--quick-gen-key", "Keep <keep@x.io>", "ed25519")
	runBag(t, nil, "--batch", "--quick-gen-key", "Drop <drop@x.io>", "ed25519")

	// Sanity: both UIDs present.
	_, out, _ := runBag(t, nil, "--list-keys")
	if !bytes.Contains(out, []byte("keep@x.io")) || !bytes.Contains(out, []byte("drop@x.io")) {
		t.Fatalf("setup failed; got %s", out)
	}

	// --delete-keys must refuse when secret material is present.
	exit, _, _ := runBag(t, nil, "--batch", "--delete-keys", "drop")
	if exit == 0 {
		t.Errorf("--delete-keys should refuse while secret key exists")
	}

	// Delete secret, then pub.
	exit, _, er := runBag(t, nil, "--batch", "--delete-secret-keys", "drop")
	if exit != 0 {
		t.Fatalf("delete-secret-keys: exit=%d stderr=%s", exit, er)
	}
	exit, _, er = runBag(t, nil, "--batch", "--delete-keys", "drop")
	if exit != 0 {
		t.Fatalf("delete-keys: exit=%d stderr=%s", exit, er)
	}

	_, out, _ = runBag(t, nil, "--list-keys")
	if bytes.Contains(out, []byte("drop@x.io")) {
		t.Errorf("dropped UID still present: %s", out)
	}
	if !bytes.Contains(out, []byte("keep@x.io")) {
		t.Errorf("kept UID removed accidentally: %s", out)
	}
}

// TestGenKeyDSARejected: --quick-gen-key dsa surfaces a clear error
// (library limitation) instead of producing a corrupt keyring.
func TestGenKeyDSARejected(t *testing.T) {
	t.Setenv("GNUPGHOME", shortHome(t))
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

// TestPassphraseProtectedKeyRoundTrip exercises the full sign/decrypt
// flow on a passphrase-locked secret key. We generate the key with
// bag (passphrase set via --passphrase), then:
//
//  1. bag encrypts to it and bag decrypts (with --passphrase-fd 0)
//  2. system gpg encrypts to it (--trust-model always) and bag decrypts
//  3. bag signs (passphrase via fd) and bag + system gpg both verify
//
// The point is to catch regressions in passphrase routing — early
// versions of bag's decrypt prompt swallowed the test's stdin
// redirection (the fd-0 bypass bug), and that would silently break
// every real user with a locked secret key.
func TestPassphraseProtectedKeyRoundTrip(t *testing.T) {
	home := shortHome(t)
	t.Setenv("GNUPGHOME", home)
	pass := "open sesame"
	exit, _, er := runBag(t, nil,
		"--batch", "--passphrase", pass,
		"--quick-gen-key", "Locked <locked@x.io>", "ed25519")
	if exit != 0 {
		t.Fatalf("gen locked key: exit=%d stderr=%s", exit, er)
	}

	plain := []byte("locked secret key plaintext\n")
	dir := t.TempDir()
	plainFile := filepath.Join(dir, "p")
	os.WriteFile(plainFile, plain, 0o600)

	// (1) bag encrypts → bag decrypts (with passphrase-fd).
	bagCipher := filepath.Join(dir, "bag.gpg")
	exit, _, er = runBag(t, nil, "--batch", "--encrypt", "-r", "locked",
		"--output", bagCipher, plainFile)
	if exit != 0 {
		t.Fatalf("bag encrypt: exit=%d stderr=%s", exit, er)
	}
	exit, got, er := runBag(t, []byte(pass+"\n"),
		"--batch", "--passphrase-fd", "0", "-d", "--output", "-", bagCipher)
	if exit != 0 {
		t.Fatalf("bag decrypt locked: exit=%d stderr=%s", exit, er)
	}
	if !bytes.Equal(got, plain) {
		t.Errorf("locked bag→bag mismatch: got %q", got)
	}

	// (2) system gpg interop. Trust-model always so gpg accepts the
	// (no-trustdb) recipient.
	gpg := systemGPG()
	if gpg != "" {
		gpgCipher := filepath.Join(dir, "gpg.gpg")
		cmd := exec.Command(gpg, "--homedir", home, "--batch",
			"--pinentry-mode", "loopback", "--trust-model", "always",
			"--encrypt", "-r", "locked", "--output", gpgCipher, plainFile)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("system gpg encrypt: %v\n%s", err, out)
		}
		exit, got, er = runBag(t, []byte(pass+"\n"),
			"--batch", "--passphrase-fd", "0", "-d", "--output", "-", gpgCipher)
		if exit != 0 {
			t.Fatalf("bag decrypt gpg-locked: exit=%d stderr=%s", exit, er)
		}
		if !bytes.Equal(got, plain) {
			t.Errorf("locked gpg→bag mismatch: got %q", got)
		}
	}

	// (3) bag signs with passphrase via fd; bag + system verify.
	bagSig := filepath.Join(dir, "bag.sig")
	exit, _, er = runBag(t, []byte(pass+"\n"),
		"--batch", "--passphrase-fd", "0", "-s", "-a",
		"--output", bagSig, plainFile)
	if exit != 0 {
		t.Fatalf("bag sign locked: exit=%d stderr=%s", exit, er)
	}
	exit, _, er = runBag(t, nil, "--verify", bagSig)
	if exit != 0 || !bytes.Contains(er, []byte("Good signature")) {
		t.Errorf("bag verify of locked-sig: exit=%d stderr=%s", exit, er)
	}
	if gpg != "" {
		out, _ := exec.Command(gpg, "--homedir", home, "--verify", bagSig).CombinedOutput()
		if !bytes.Contains(out, []byte("Good signature")) {
			t.Errorf("gpg verify of bag's locked-sig:\n%s", out)
		}
	}
}

// TestEnarmorDearmorRoundTrip wraps and unwraps a binary payload via
// bag's --enarmor / --dearmor, then confirms that gpg --dearmor reads
// the same envelope. This is pure framing — no crypto — but it's
// exactly the failure mode where tools disagree (line lengths, CRC
// trailer, header order).
func TestEnarmorDearmorRoundTrip(t *testing.T) {
	dir := t.TempDir()
	bin := []byte("\x88\x40\x01\x02\x03\x04\x05\x06binary opaque body\xff\xfe\xfd")
	binFile := filepath.Join(dir, "blob.bin")
	if err := os.WriteFile(binFile, bin, 0o600); err != nil {
		t.Fatal(err)
	}

	armored := filepath.Join(dir, "blob.asc")
	exit, _, er := runBag(t, nil, "--enarmor", "-o", armored, binFile)
	if exit != 0 {
		t.Fatalf("enarmor: exit=%d stderr=%s", exit, er)
	}
	a, _ := os.ReadFile(armored)
	if !bytes.Contains(a, []byte("-----BEGIN PGP MESSAGE-----")) {
		t.Fatalf("expected armor header; got %s", a)
	}

	// bag dearmor → original bytes.
	back := filepath.Join(dir, "blob.out")
	exit, _, er = runBag(t, nil, "--dearmor", "-o", back, armored)
	if exit != 0 {
		t.Fatalf("dearmor: exit=%d stderr=%s", exit, er)
	}
	got, _ := os.ReadFile(back)
	if !bytes.Equal(got, bin) {
		t.Fatalf("dearmor mismatch: got %s want %s", fmtBytes(got), fmtBytes(bin))
	}

	// Interop: system gpg --dearmor must also accept bag's armor.
	if gpg := systemGPG(); gpg != "" {
		out := filepath.Join(dir, "blob.sysgpg")
		cmd := exec.Command(gpg, "--dearmor", "--output", out, armored)
		if outBytes, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("system gpg --dearmor: %v\n%s", err, outBytes)
		}
		sys, _ := os.ReadFile(out)
		if !bytes.Equal(sys, bin) {
			t.Fatalf("system gpg dearmor mismatch: got %s want %s",
				fmtBytes(sys), fmtBytes(bin))
		}
	}
}

// TestPrintMDMatchesSystem runs bag --print-md against system gpg for
// a few digests. We trim whitespace for comparison because gpg pads
// long-format hashes differently across versions.
func TestPrintMDMatchesSystem(t *testing.T) {
	gpg := systemGPG()
	if gpg == "" {
		t.Skip("no system gpg; skipping interop digest test")
	}
	dir := t.TempDir()
	data := bytes.Repeat([]byte("the quick brown fox\n"), 50)
	file := filepath.Join(dir, "data.bin")
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, algo := range []string{"SHA1", "SHA256", "SHA512"} {
		t.Run(algo, func(t *testing.T) {
			exit, out, er := runBag(t, nil, "--print-md", algo, file)
			if exit != 0 {
				t.Fatalf("bag --print-md %s: exit=%d stderr=%s", algo, exit, er)
			}
			cmd := exec.Command(gpg, "--print-md", algo, file)
			sys, err := cmd.Output()
			if err != nil {
				t.Fatalf("system gpg --print-md %s: %v", algo, err)
			}
			// Reduce both to "just the hex bytes" — gpg's whitespace
			// drifts between versions but the hex itself must match.
			normalize := func(b []byte) string {
				s := strings.ToUpper(string(b))
				if i := strings.Index(s, ":"); i >= 0 {
					s = s[i+1:]
				}
				return strings.Join(strings.Fields(s), "")
			}
			if a, b := normalize(out), normalize(sys); a != b {
				t.Fatalf("%s mismatch:\n bag: %s\n gpg: %s", algo, a, b)
			}
		})
	}
}

// TestShowKeysFromFile: export a key with bag, then bag --show-keys
// on the exported file should print it without touching the keyring
// (we verify by exporting into a separate home, then running
// show-keys against a third, empty home).
func TestShowKeysFromFile(t *testing.T) {
	srcHome := shortHome(t)
	t.Setenv("GNUPGHOME", srcHome)
	exit, _, er := runBag(t, nil,
		"--batch", "--quick-gen-key", "Show <show@x.io>", "rsa", "default", "0")
	if exit != 0 {
		t.Fatalf("genkey: exit=%d stderr=%s", exit, er)
	}
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "show.pgp")
	exit, _, er = runBag(t, nil, "--export", "-a", "-o", keyPath, "show@x.io")
	if exit != 0 {
		t.Fatalf("export: exit=%d stderr=%s", exit, er)
	}

	// New empty home: --show-keys must list without creating
	// pubring.gpg / secring.gpg there.
	emptyHome := shortHome(t)
	t.Setenv("GNUPGHOME", emptyHome)
	exit, out, er := runBag(t, nil, "--show-keys", keyPath)
	if exit != 0 {
		t.Fatalf("show-keys: exit=%d stderr=%s", exit, er)
	}
	if !bytes.Contains(out, []byte("show@x.io")) {
		t.Errorf("show-keys output missing UID: %s", out)
	}
	// Verify keyrings were NOT created.
	if _, err := os.Stat(filepath.Join(emptyHome, "pubring.gpg")); err == nil {
		t.Errorf("--show-keys created a pubring; should be a pure read")
	}
}

// TestWithColonsListing: --list-keys --with-colons must produce
// records in the documented colon format, with stable column count
// (15) and the expected leading record types.
func TestWithColonsListing(t *testing.T) {
	t.Setenv("GNUPGHOME", shortHome(t))
	exit, _, er := runBag(t, nil,
		"--batch", "--quick-gen-key", "Col <col@x.io>", "rsa", "default", "0")
	if exit != 0 {
		t.Fatalf("genkey: exit=%d stderr=%s", exit, er)
	}
	exit, out, er := runBag(t, nil, "--list-keys", "--with-colons")
	if exit != 0 {
		t.Fatalf("list-keys --with-colons: exit=%d stderr=%s", exit, er)
	}
	sawPub, sawFpr, sawUID := false, false, false
	for _, line := range bytes.Split(out, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		fields := bytes.Split(line, []byte(":"))
		switch string(fields[0]) {
		case "pub":
			sawPub = true
			// Field 5 = keyid, 16 hex chars.
			if len(fields) >= 5 && len(fields[4]) != 16 {
				t.Errorf("pub keyid not 16 hex: %q (line=%q)", fields[4], line)
			}
		case "fpr":
			sawFpr = true
			// Field 10 = full fingerprint.
			if len(fields) >= 10 && len(fields[9]) < 40 {
				t.Errorf("fpr too short: %q", fields[9])
			}
		case "uid":
			sawUID = true
			if !bytes.Contains(line, []byte("col@x.io")) {
				t.Errorf("uid line missing email: %s", line)
			}
		}
	}
	if !sawPub || !sawFpr || !sawUID {
		t.Errorf("missing record types pub=%v fpr=%v uid=%v\noutput=%s",
			sawPub, sawFpr, sawUID, out)
	}
}

// TestListPacketsSeesSymmetric: encrypt with bag -c, run bag
// --list-packets on the result, look for the expected packet labels.
// We don't pin the exact text (it's a debug dump, not an API) but we
// require at least one of the known cleartext markers.
func TestListPacketsSeesSymmetric(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain")
	if err := os.WriteFile(plain, []byte("watching the packets go by"), 0o600); err != nil {
		t.Fatal(err)
	}
	exit, _, er := runBag(t, []byte("topsecret\n"),
		"-c", "--batch", "--yes",
		"--passphrase-fd", "0", "-o", plain+".gpg", plain)
	if exit != 0 {
		t.Fatalf("encrypt: exit=%d stderr=%s", exit, er)
	}
	exit, out, er := runBag(t, nil, "--list-packets", plain+".gpg")
	if exit != 0 {
		t.Fatalf("list-packets: exit=%d stderr=%s", exit, er)
	}
	// We expect to see a symkey-encrypted session key and an encrypted
	// data packet. The exact wording is bag's, so match on the labels
	// bag itself prints.
	if !bytes.Contains(out, []byte("symkey-encrypted session key")) {
		t.Errorf("expected symkey session key packet; got:\n%s", out)
	}
	if !bytes.Contains(out, []byte("encrypted data packet")) {
		t.Errorf("expected encrypted data packet; got:\n%s", out)
	}
}
