package actions

import (
	"github.com/atomicobject/rhizome/pkg/vault/frontmatter"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// AuthorityScorePayload carries a note and its hub/authority scores, plus lightweight
// metadata (title + blessed frontmatter) to help callers decide whether to fetch
// the full note.
type AuthorityScorePayload struct {
	Path        string                 `json:"path"`
	Title       string                 `json:"title,omitempty"`
	Frontmatter map[string]interface{} `json:"frontmatter,omitempty"`
	Authority   float64                `json:"authority"`
	Hub         float64                `json:"hub,omitempty"`
}

// AuthorityScoresToPayload converts authority score rows to JSON-friendly payloads.
//
// When noteMgr is available, it will attempt to include blessed frontmatter keys
// (e.g., summary/tags) to keep the payload high-signal without mirroring all metadata.
func AuthorityScoresToPayload(scores []obsidian.AuthorityScore, limit int, vaultDef obsidian.VaultDefinition, noteMgr obsidian.NoteReader) []AuthorityScorePayload {
	if len(scores) == 0 {
		return nil
	}
	if limit > 0 && len(scores) > limit {
		scores = scores[:limit]
	}
	out := make([]AuthorityScorePayload, 0, len(scores))
	facts := NoteFactsFromReader(noteMgr)
	for _, s := range scores {
		payload := AuthorityScorePayload{
			Path:      s.Path,
			Authority: s.Authority,
			Hub:       s.Hub,
			Title:     titleFromPath(s.Path),
		}
		if fact, ok := facts.LookupFact(s.Path); ok {
			payload.Frontmatter = frontmatter.FilterBlessed(fact.Frontmatter)
		}
		out = append(out, payload)
	}
	return out
}
