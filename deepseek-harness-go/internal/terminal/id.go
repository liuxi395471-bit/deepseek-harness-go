package terminal

import (
	"crypto/rand"
	"encoding/hex"
)

func newRandID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
