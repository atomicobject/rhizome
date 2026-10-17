package store

import (
	pathutil "github.com/atomicobject/rhizome/pkg/paths"
	"path"
	"strings"
)

func noteLookupKeys(notePath string) (fullKey string, baseKey string, pathLen int) {
	notePath = string(pathutil.Normalize(strings.TrimSpace(notePath)))
	if notePath == "" {
		return "", "", 0
	}
	fullKey = pathutil.LinkKey(strings.TrimSuffix(notePath, path.Ext(notePath)))
	baseKey = path.Base(fullKey)
	return fullKey, baseKey, len(notePath)
}

func firstSegmentNorm(notePath string) string {
	notePath = string(pathutil.Normalize(strings.TrimSpace(notePath)))
	if notePath == "" {
		return ""
	}
	if parts := strings.SplitN(notePath, "/", 2); len(parts) > 0 {
		return strings.ToLower(strings.TrimSpace(parts[0]))
	}
	return strings.ToLower(notePath)
}

func buildNoteSearchTermRowsForNote(noteID int64, notePath, title string) []NoteSearchTermRow {
	rows := make([]NoteSearchTermRow, 0, 16)
	appendTerms := func(scope NoteSearchScope, segmentOrdinal int, text string) {
		for tokenOrdinal, token := range searchTokens(text) {
			rows = append(rows, NoteSearchTermRow{
				NoteID:         noteID,
				Scope:          scope,
				SegmentOrdinal: segmentOrdinal,
				TokenOrdinal:   tokenOrdinal,
				TokenText:      token,
			})
		}
	}

	pathLower := strings.ToLower(strings.TrimSpace(notePath))
	appendTerms(NoteSearchScopePath, 0, pathLower)
	appendTerms(NoteSearchScopeTitle, 0, strings.ToLower(strings.TrimSpace(title)))
	if parts := strings.SplitN(pathLower, "/", 2); len(parts) == 2 {
		appendTerms(NoteSearchScopeContent, 0, parts[1])
	}
	for segmentOrdinal, segment := range strings.Split(pathLower, "/") {
		appendTerms(NoteSearchScopeSegment, segmentOrdinal, segment)
	}
	return rows
}

func searchTokens(text string) []string {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return nil
	}
	var tokens []string
	var current strings.Builder
	flush := func() {
		if current.Len() == 0 {
			return
		}
		tokens = append(tokens, current.String())
		current.Reset()
	}
	for _, r := range text {
		if searchTokenDelimiter(r) {
			flush()
			continue
		}
		current.WriteRune(r)
	}
	flush()
	return tokens
}

func searchTokenDelimiter(r rune) bool {
	switch r {
	case '/', '-', '_', ' ', '.', ',', '(', ')':
		return true
	default:
		return false
	}
}
