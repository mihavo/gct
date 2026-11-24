package keystore

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	privateKeyPEMType = "PRIVATE KEY"
	keyFileExt        = ".pem"
	dirMode           = 0o700
	fileMode          = 0o600
	kidBytes          = 16
)

func KeyID(pub *ecdsa.PublicKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", fmt.Errorf("encoding public key: %w", err)
	}
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:kidBytes]), nil
}

func Generate(dir string) (kid string, path string, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("generating key: %w", err)
	}

	kid, err = KeyID(&key.PublicKey)
	if err != nil {
		return "", "", err
	}

	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", "", fmt.Errorf("encoding private key: %w", err)
	}
	data := pem.EncodeToMemory(&pem.Block{Type: privateKeyPEMType, Bytes: der})

	if err := os.MkdirAll(dir, dirMode); err != nil {
		return "", "", fmt.Errorf("creating key directory %s: %w", dir, err)
	}

	path = filepath.Join(dir, kid+keyFileExt)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
	if err != nil {
		return "", "", fmt.Errorf("creating key file %s: %w", path, err)
	}

	if _, err := file.Write(data); err != nil {
		file.Close()
		os.Remove(path)
		return "", "", fmt.Errorf("writing key file %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return "", "", fmt.Errorf("closing key file %s: %w", path, err)
	}

	return kid, path, nil
}

func EnsureKey(dir string) (created bool, err error) {
	exists, err := keysExist(dir)
	if err != nil {
		return false, fmt.Errorf("checking key directory %s: %w", dir, err)
	}
	if exists {
		return false, nil
	}

	if _, _, err := Generate(dir); err != nil {
		return false, err
	}
	return true, nil
}

func keysExist(dir string) (bool, error) {
	exists := false
	files, err := os.ReadDir(dir)

	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}

	for _, file := range files {
		if !file.IsDir() && filepath.Ext(file.Name()) == keyFileExt {
			exists = true
			break
		}
	}
	return exists, nil
}
