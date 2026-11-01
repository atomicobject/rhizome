package ledger

import (
	"crypto/rand"
	"encoding/hex"
)

func newOwner() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
