package keystore

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func readKey(t *testing.T, path string) *ecdsa.PrivateKey {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	block, rest := pem.Decode(data)
	if block == nil {
		t.Fatalf("%s does not contain a PEM block", path)
	}
	if len(rest) != 0 {
		t.Fatalf("%s has %d bytes after the PEM block", path, len(rest))
	}
	if block.Type != "PRIVATE KEY" {
		t.Fatalf("PEM type = %q, want %q", block.Type, "PRIVATE KEY")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		t.Fatalf("parsing PKCS#8: %v", err)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		t.Fatalf("key type = %T, want *ecdsa.PrivateKey", parsed)
	}
	return key
}

func TestGenerateWritesUsableP256Key(t *testing.T) {
	dir := t.TempDir()

	kid, path, err := Generate(dir)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if want := filepath.Join(dir, kid+".pem"); path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}

	key := readKey(t, path)
	if key.Curve != elliptic.P256() {
		t.Fatalf("curve = %s, want P-256", key.Curve.Params().Name)
	}

	wantKid, err := KeyID(&key.PublicKey)
	if err != nil {
		t.Fatalf("KeyID() error = %v", err)
	}
	if kid != wantKid {
		t.Fatalf("kid = %q, but the file's public key hashes to %q", kid, wantKid)
	}
}

func TestGenerateRestrictsPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "keys")

	_, path, err := Generate(dir)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat key file: %v", err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("key file mode = %o, want 600", got)
	}

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat key dir: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("key dir mode = %o, want 700", got)
	}
}

func TestGenerateCreatesMissingDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b", "keys")

	if _, _, err := Generate(dir); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("directory was not created: %v", err)
	}
}

func TestGenerateTwiceKeepsBothKeys(t *testing.T) {
	dir := t.TempDir()

	kid1, path1, err := Generate(dir)
	if err != nil {
		t.Fatalf("first Generate() error = %v", err)
	}
	kid2, path2, err := Generate(dir)
	if err != nil {
		t.Fatalf("second Generate() error = %v", err)
	}

	if kid1 == kid2 {
		t.Fatalf("both keys got kid %q", kid1)
	}
	for _, path := range []string{path1, path2} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("key file %s is missing: %v", path, err)
		}
	}
}

func TestGenerateFailsWhenDirectoryIsAFile(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("creating blocker file: %v", err)
	}

	if _, _, err := Generate(blocker); err == nil {
		t.Fatalf("Generate() = nil error, want an error when the directory path is a file")
	}
}

func TestKeyIDIsStableAndDiffersPerKey(t *testing.T) {
	dir := t.TempDir()
	_, pathA, err := Generate(dir)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	_, pathB, err := Generate(dir)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	keyA, keyB := readKey(t, pathA), readKey(t, pathB)

	first, err := KeyID(&keyA.PublicKey)
	if err != nil {
		t.Fatalf("KeyID() error = %v", err)
	}
	second, err := KeyID(&keyA.PublicKey)
	if err != nil {
		t.Fatalf("KeyID() error = %v", err)
	}
	other, err := KeyID(&keyB.PublicKey)
	if err != nil {
		t.Fatalf("KeyID() error = %v", err)
	}

	if first != second {
		t.Fatalf("KeyID changed between calls: %q vs %q", first, second)
	}
	if first == other {
		t.Fatalf("two different keys share kid %q", first)
	}
	if len(first) != 32 {
		t.Fatalf("len(kid) = %d, want 32 hex characters", len(first))
	}
}

func countPEM(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	n := 0
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".pem" {
			n++
		}
	}
	return n
}

func TestEnsureKeyCreatesKeyInEmptyDirectory(t *testing.T) {
	dir := t.TempDir()

	created, err := EnsureKey(dir)
	if err != nil {
		t.Fatalf("EnsureKey() error = %v", err)
	}
	if !created {
		t.Fatalf("created = false, want true for an empty directory")
	}
	if got := countPEM(t, dir); got != 1 {
		t.Fatalf("%d .pem files after EnsureKey, want 1", got)
	}
}

func TestEnsureKeyKeepsExistingKey(t *testing.T) {
	dir := t.TempDir()
	kid, _, err := Generate(dir)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	created, err := EnsureKey(dir)
	if err != nil {
		t.Fatalf("EnsureKey() error = %v", err)
	}
	if created {
		t.Fatalf("created = true, want false when a key already exists")
	}
	if got := countPEM(t, dir); got != 1 {
		t.Fatalf("%d .pem files after EnsureKey, want the original 1", got)
	}
	if _, err := os.Stat(filepath.Join(dir, kid+".pem")); err != nil {
		t.Fatalf("original key file is gone: %v", err)
	}
}

func TestEnsureKeyCreatesMissingDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "keys")

	created, err := EnsureKey(dir)
	if err != nil {
		t.Fatalf("EnsureKey() error = %v", err)
	}
	if !created {
		t.Fatalf("created = false, want true for a missing directory")
	}
	if got := countPEM(t, dir); got != 1 {
		t.Fatalf("%d .pem files after EnsureKey, want 1", got)
	}
}

func TestEnsureKeyFailsWhenDirectoryIsAFile(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("creating blocker file: %v", err)
	}

	created, err := EnsureKey(blocker)
	if err == nil {
		t.Fatalf("EnsureKey() = nil error, want an error when the path is a file")
	}
	if created {
		t.Fatalf("created = true, want false on error")
	}
}

func TestEnsureKeyIgnoresOtherFilesAndDirectories(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("writing notes.txt: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "looks-like-a-key.pem"), 0o700); err != nil {
		t.Fatalf("creating directory named .pem: %v", err)
	}

	created, err := EnsureKey(dir)
	if err != nil {
		t.Fatalf("EnsureKey() error = %v", err)
	}
	if !created {
		t.Fatalf("created = false, want true when no regular .pem file exists")
	}
}

func TestKeysExist(t *testing.T) {
	t.Run("missing directory", func(t *testing.T) {
		ok, err := keysExist(filepath.Join(t.TempDir(), "nope"))
		if ok || err != nil {
			t.Fatalf("keysExist() = %v, %v, want false and nil", ok, err)
		}
	})

	t.Run("empty directory", func(t *testing.T) {
		ok, err := keysExist(t.TempDir())
		if ok || err != nil {
			t.Fatalf("keysExist() = %v, %v, want false and nil", ok, err)
		}
	})

	t.Run("directory with a key", func(t *testing.T) {
		dir := t.TempDir()
		if _, _, err := Generate(dir); err != nil {
			t.Fatalf("Generate() error = %v", err)
		}
		ok, err := keysExist(dir)
		if !ok || err != nil {
			t.Fatalf("keysExist() = %v, %v, want true and nil", ok, err)
		}
	})

	t.Run("unreadable directory", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root can read any directory")
		}
		dir := t.TempDir()
		if err := os.Chmod(dir, 0); err != nil {
			t.Fatalf("chmod: %v", err)
		}
		t.Cleanup(func() { os.Chmod(dir, 0o700) })

		ok, err := keysExist(dir)
		if ok || err == nil || errors.Is(err, os.ErrNotExist) {
			t.Fatalf("keysExist() = %v, %v, want false and a permission error", ok, err)
		}
	})
}
