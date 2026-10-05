package codeanchor

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func sha256HexParts(parts ...string) string {
	h := sha256.New()
	for i, p := range parts {
		if i > 0 {
			_, _ = h.Write([]byte{0})
		}
		_, _ = h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func intelAnchorID(lang Lang, kind string, relPath string, stableKey string) string {
	return sha256HexParts(string(lang), kind, relPath, stableKey)
}

func intelDocSectionID(relPath string, breadcrumbSlug string, startByte int) string {
	return sha256HexParts(relPath, breadcrumbSlug, strconv.Itoa(startByte))
}

func slugifyHeading(title string) string {
	title = strings.TrimSpace(title)
	title = strings.ToLower(title)
	title = strings.ReplaceAll(title, " ", "-")
	title = strings.ReplaceAll(title, "/", "-")
	title = strings.ReplaceAll(title, "\\", "-")
	return title
}

// IntelChunkID generates a stable chunk ID from the owner ID, ordinal, and granularity.
// The ID is deterministic: same inputs always produce the same output.
func IntelChunkID(ownerID string, ord int, granularity string) string {
	return sha256HexParts(ownerID, strconv.Itoa(ord), granularity)
}
