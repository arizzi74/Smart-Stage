package identity

import (
	"crypto/rand"
	"encoding/hex"
)

func New() string {
	var data [24]byte
	if _, err := rand.Read(data[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(data[:])
}
