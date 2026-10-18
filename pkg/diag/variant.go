// Package diag defines presentation data shared by validation producers and storage.
package diag

// Variant identifies one case within a diagnostic code. It does not affect
// stable finding identity or repair authority.
type Variant struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

func NewVariant(key, label string) *Variant {
	if key == "" {
		return nil
	}
	return &Variant{Key: key, Label: label}
}
