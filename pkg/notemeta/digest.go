package notemeta

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/atomicobject/rhizome/pkg/paths"
	"strings"
)

type noteSetDigest [sha256.Size]byte

func (d *noteSetDigest) Add(path, hash string) {
	canonical, err := paths.CleanNotePath(path)
	if err != nil {
		return
	}
	value := sha256.Sum256([]byte(canonical.String() + "\x00" + strings.TrimSpace(hash)))
	for i := range d {
		d[i] ^= value[i]
	}
}
func (d *noteSetDigest) Remove(path, hash string) { d.Add(path, hash) }
func (d noteSetDigest) String() string            { return hex.EncodeToString(d[:]) }

func decodeNotesHash(value string) (noteSetDigest, error) {
	var out noteSetDigest
	value = strings.TrimSpace(value)
	if value == "" {
		return out, nil
	}
	decoded, err := hex.DecodeString(value)
	if err != nil {
		return out, err
	}
	if len(decoded) != len(out) {
		return out, fmt.Errorf("invalid notes hash length")
	}
	copy(out[:], decoded)
	return out, nil
}
