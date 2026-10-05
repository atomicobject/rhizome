package agentchat

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

func newID(prefix string) string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strings.TrimSpace(prefix) + "_fallback"
	}
	if prefix = strings.TrimSpace(prefix); prefix != "" {
		return prefix + "_" + hex.EncodeToString(b[:])
	}
	return hex.EncodeToString(b[:])
}
