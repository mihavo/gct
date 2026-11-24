package keystore

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
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

func pemFile(t *testing.T, typ string, der []byte) []byte {
	t.Helper()
	return pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der})
}

func pkcs8(t *testing.T, key any) []byte {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshalling key: %v", err)
	}
	return der
}

func TestLoadReturnsEveryKey(t *testing.T) {
	dir := t.TempDir()
	kid1, _, err := Generate(dir)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	kid2, _, err := Generate(dir)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	keys, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("len(keys) = %d, want 2", len(keys))
	}

	got := map[string]bool{}
	for _, key := range keys {
		if key.Key == nil {
			t.Fatalf("key %s has a nil private key", key.Kid)
		}
		wantKid, err := KeyID(&key.Key.PublicKey)
		if err != nil {
			t.Fatalf("KeyID() error = %v", err)
		}
		if key.Kid != wantKid {
			t.Fatalf("Kid = %q, but its key hashes to %q", key.Kid, wantKid)
		}
		got[key.Kid] = true
	}
	if !got[kid1] || !got[kid2] {
		t.Fatalf("loaded kids %v, want %q and %q", got, kid1, kid2)
	}
}

func TestLoadReturnsNoKeysForEmptyDirectory(t *testing.T) {
	keys, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("len(keys) = %d, want 0", len(keys))
	}
}

func TestLoadFailsForMissingDirectory(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatalf("Load() = nil error, want an error for a missing directory")
	}
}

func TestLoadIgnoresOtherFilesAndDirectories(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not a key"), 0o600); err != nil {
		t.Fatalf("writing notes.txt: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "looks-like-a-key.pem"), 0o700); err != nil {
		t.Fatalf("creating directory named .pem: %v", err)
	}

	keys, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("len(keys) = %d, want 0", len(keys))
	}
}

func TestLoadRejectsBadKeyFiles(t *testing.T) {
	p256, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating P-256 key: %v", err)
	}
	p384, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatalf("generating P-384 key: %v", err)
	}
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating RSA key: %v", err)
	}
	p384Kid, err := KeyID(&p384.PublicKey)
	if err != nil {
		t.Fatalf("KeyID() error = %v", err)
	}
	p256Kid, err := KeyID(&p256.PublicKey)
	if err != nil {
		t.Fatalf("KeyID() error = %v", err)
	}

	tests := []struct {
		name    string
		file    string
		content []byte
	}{
		{"not PEM", "garbage.pem", []byte("this is not pem")},
		{"wrong PEM type", p256Kid + ".pem", pemFile(t, "EC PRIVATE KEY", pkcs8(t, p256))},
		{"invalid PKCS#8 bytes", "broken.pem", pemFile(t, "PRIVATE KEY", []byte("junk"))},
		{"RSA key", "rsa.pem", pemFile(t, "PRIVATE KEY", pkcs8(t, rsaKey))},
		{"P-384 key", p384Kid + ".pem", pemFile(t, "PRIVATE KEY", pkcs8(t, p384))},
		{"file name differs from kid", "renamed.pem", pemFile(t, "PRIVATE KEY", pkcs8(t, p256))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, tt.file), tt.content, 0o600); err != nil {
				t.Fatalf("writing %s: %v", tt.file, err)
			}

			keys, err := Load(dir)
			if err == nil {
				t.Fatalf("Load() = nil error, want an error")
			}
			if len(keys) != 0 {
				t.Fatalf("len(keys) = %d on error, want 0", len(keys))
			}
		})
	}
}

func TestLoadFailsWhenOneOfSeveralFilesIsBad(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := Generate(dir); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "zzz.pem"), []byte("garbage"), 0o600); err != nil {
		t.Fatalf("writing bad key: %v", err)
	}

	keys, err := Load(dir)
	if err == nil {
		t.Fatalf("Load() = nil error, want an error when any file is bad")
	}
	if len(keys) != 0 {
		t.Fatalf("len(keys) = %d on error, want 0", len(keys))
	}
}
