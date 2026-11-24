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
	"strings"
)

const (
	privateKeyPEMType = "PRIVATE KEY"
	keyFileExt        = ".pem"
	dirMode           = 0o700
	fileMode          = 0o600
	kidBytes          = 16
)

type Key struct {
	Kid string
	Key *ecdsa.PrivateKey
}

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

func Load(dir string) ([]Key, error) {
	files, err := os.ReadDir(dir)
	keys := make([]Key, 0)

	if err != nil {
		return []Key{}, err
	}
	for _, file := range files {
		if !file.IsDir() && filepath.Ext(file.Name()) == keyFileExt {
			content, err := os.ReadFile(filepath.Join(dir, file.Name()))
			if err != nil {
				return []Key{}, err
			}
			block, _ := pem.Decode(content)
			if block == nil {
				return []Key{}, fmt.Errorf("%s: not a PEM file", file.Name())
			}
			if block.Type != privateKeyPEMType {
				return []Key{}, fmt.Errorf("%s: PEM type is %q, want %q", file.Name(), block.Type, privateKeyPEMType)
			}

			parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
			if err != nil {
				return []Key{}, fmt.Errorf("%s: parsing PKCS#8 key: %w", file.Name(), err)
			}
			key, ok := parsed.(*ecdsa.PrivateKey)
			if !ok {
				return []Key{}, fmt.Errorf("%s: key is %T, want an ECDSA key", file.Name(), parsed)
			}
			if key.Curve != elliptic.P256() {
				return []Key{}, fmt.Errorf("%s: curve is %s, want P-256", file.Name(), key.Curve.Params().Name)
			}

			kid, err := KeyID(&key.PublicKey)
			if err != nil {
				return []Key{}, fmt.Errorf("%s: %w", file.Name(), err)
			}
			if kid != strings.TrimSuffix(file.Name(), keyFileExt) {
				return []Key{}, fmt.Errorf("%s: file name does not match the key's kid %s", file.Name(), kid)
			}

			keys = append(keys, Key{Kid: kid, Key: key})
		}
	}

	return keys, nil
}
