package gate

import (
	"crypto/sha256"
	"encoding/hex"
)

func GenerateKeyHash(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}
