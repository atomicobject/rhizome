package frontmatter

import "strings"

// BlessedKeys are the only frontmatter keys that are treated as "summary-like"
// metadata for lightweight surfacing (e.g., semantic chunking, file_context stubs).
//
// Keep this list small and high-signal: it is intended to provide awareness,
// not a full metadata mirror.
var BlessedKeys = []string{
	"summary",
	"synopsis",
	"about",
	"doc",
	"desc",
	"description",
	"tags",
}

// FilterBlessed returns a new map containing only blessed keys (when present)
// from the provided frontmatter map.
func FilterBlessed(frontmatter map[string]interface{}) map[string]interface{} {
	if len(frontmatter) == 0 {
		return nil
	}

	// Build the set per call to keep BlessedKeys externally mutable in tests and
	// future config experiments without sharing a global map across packages.
	blessed := make(map[string]struct{}, len(BlessedKeys))
	for _, k := range BlessedKeys {
		blessed[strings.ToLower(k)] = struct{}{}
	}

	// Be forgiving: Obsidian users often vary capitalization (e.g., "Summary" vs "summary").
	// We normalize output keys to lowercase for consistent downstream handling.
	out := make(map[string]interface{})
	for k, v := range frontmatter {
		lk := strings.ToLower(strings.TrimSpace(k))
		if _, ok := blessed[lk]; !ok {
			continue
		}
		if _, exists := out[lk]; exists {
			continue
		}
		out[lk] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
