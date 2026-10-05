package ontology

import (
	"fmt"
	"sort"
	"strings"
)

type EditBaseDocument struct {
	NotePath    string `json:"notePath"`
	Fingerprint string `json:"fingerprint"`
	Content     string `json:"content"`
}

// RestoreBases installs verified original source snapshots before operations
// are replayed. It never substitutes current disk content for the saved base.
func (s *EditSession) RestoreBases(bases []EditBaseDocument) error {
	if s == nil {
		return fmt.Errorf("edit session is nil")
	}
	seen := make(map[string]struct{}, len(bases))
	for _, base := range bases {
		path, err := CanonicalEditPath(base.NotePath)
		if err != nil {
			return err
		}
		notePath := path.String()
		if _, ok := seen[notePath]; ok {
			return fmt.Errorf("duplicate edit base for %s", notePath)
		}
		seen[notePath] = struct{}{}
		state := &documentState{notePath: notePath, content: base.Content, formats: s.formats}
		snapshot, err := state.documentSnapshot()
		if err != nil {
			return fmt.Errorf("restore edit base %s: %w", notePath, err)
		}
		if strings.TrimSpace(base.Fingerprint) == "" || snapshot.ContentFingerprint != strings.TrimSpace(base.Fingerprint) {
			return fmt.Errorf("restore edit base %s: content fingerprint mismatch", notePath)
		}
		s.docs[notePath] = &documentState{
			notePath:              notePath,
			content:               snapshot.Content,
			baseContent:           snapshot.Content,
			baseSnapshot:          snapshot,
			schema:                s.schema,
			formats:               s.formats,
			allowSelectorRecovery: s.allowSelectorRecovery,
		}
	}
	return nil
}

func (s *EditSession) BaseDocuments() []EditBaseDocument {
	if s == nil || len(s.docs) == 0 {
		return nil
	}
	paths := make([]string, 0, len(s.docs))
	for notePath := range s.docs {
		paths = append(paths, notePath)
	}
	sort.Strings(paths)
	out := make([]EditBaseDocument, 0, len(paths))
	for _, notePath := range paths {
		state := s.docs[notePath]
		if state == nil || state.baseSnapshot == nil {
			continue
		}
		out = append(out, EditBaseDocument{
			NotePath:    notePath,
			Fingerprint: state.baseSnapshot.ContentFingerprint,
			Content:     state.baseContent,
		})
	}
	return out
}
