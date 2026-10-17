package validate

import (
	"context"

	"path/filepath"

	"sort"
	"strings"
	"unicode"

	vaulthealth "github.com/atomicobject/rhizome/pkg/vault/health"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

var genericBrokenLinkTokens = map[string]struct{}{
	"doc":       {},
	"docs":      {},
	"index":     {},
	"link":      {},
	"links":     {},
	"note":      {},
	"notes":     {},
	"readme":    {},
	"wikilink":  {},
	"wikilinks": {},
}

type brokenLinkCandidate struct {
	path  string
	score int
}

type brokenLinkMatcherIndex struct {
	notes []brokenLinkNoteFeatures
}

type brokenLinkNoteFeatures struct {
	path             string
	title            string
	normalized       string
	alpha            string
	tokens           []string
	meaningfulTokens []string
}

// alphanumOnly strips everything except lowercase letters and digits.
func alphanumOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func normalizedTokens(s string) []string {
	s = strings.ToLower(obsidian.RemoveMdSuffix(strings.TrimSpace(s)))
	if s == "" {
		return nil
	}
	return strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

func sharedTokenCount(a, b []string) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	seen := make(map[string]struct{}, len(a))
	for _, token := range a {
		seen[token] = struct{}{}
	}
	count := 0
	counted := make(map[string]struct{}, len(b))
	for _, token := range b {
		if _, ok := counted[token]; ok {
			continue
		}
		if _, ok := seen[token]; ok {
			count++
			counted[token] = struct{}{}
		}
	}
	return count
}

func newBrokenLinkMatcherIndex(allNotes []string) brokenLinkMatcherIndex {
	notes := make([]brokenLinkNoteFeatures, 0, len(allNotes))
	for _, notePath := range allNotes {
		title := strings.TrimSuffix(filepath.Base(notePath), filepath.Ext(notePath))
		tokens := normalizedTokens(title)
		notes = append(notes, brokenLinkNoteFeatures{
			path:             notePath,
			title:            title,
			normalized:       vaulthealth.NormalizeName(title),
			alpha:            alphanumOnly(vaulthealth.NormalizeName(title)),
			tokens:           tokens,
			meaningfulTokens: filterMeaningfulTokens(tokens),
		})
	}
	return brokenLinkMatcherIndex{notes: notes}
}

// findCandidateTiers returns exact normalized title matches and, only when
// there are none, guarded fuzzy matches.
func (idx brokenLinkMatcherIndex) findCandidateTiers(ctx context.Context, target string) ([]string, []string, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	needle := vaulthealth.NormalizeName(filepath.Base(strings.TrimSpace(target)))
	if needle == "" {
		return nil, nil, nil
	}

	// Exact means the same title up to case, punctuation, and spacing
	// (`semantic code index spine` and `semantic-code-index-spine`). Short or
	// non-Latin titles compare normalized text only, since stripping them to
	// ASCII letters and digits would erase the difference.
	needleAlpha := alphanumOnly(needle)
	exact := make([]string, 0, 1)
	for _, note := range idx.notes {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if note.normalized == needle || len(needleAlpha) >= 4 && len(needleAlpha)*2 >= len(needle) && note.alpha == needleAlpha {
			exact = append(exact, note.path)
		}
	}
	if len(exact) > 0 {
		sort.Strings(exact)
		return exact, nil, nil
	}

	needleTokens := normalizedTokens(target)
	needleMeaningful := filterMeaningfulTokens(needleTokens)
	if len(needleAlpha) < 6 || len(needleMeaningful) < 2 {
		return nil, nil, nil
	}

	scored := make([]brokenLinkCandidate, 0, 4)
	for _, note := range idx.notes {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		score, ok := scoreBrokenLinkCandidate(needle, needleAlpha, needleTokens, needleMeaningful, note)
		if !ok {
			continue
		}
		scored = append(scored, brokenLinkCandidate{path: note.path, score: score})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].path < scored[j].path
	})

	candidates := make([]string, 0, len(scored))
	for _, candidate := range scored {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		candidates = append(candidates, candidate.path)
	}
	return nil, candidates, nil
}

func scoreBrokenLinkCandidate(needle, needleAlpha string, needleTokens, needleMeaningful []string, note brokenLinkNoteFeatures) (int, bool) {
	sharedMeaningful := sharedTokenCount(needleMeaningful, note.meaningfulTokens)
	if sharedMeaningful < 2 {
		return 0, false
	}

	sharedAll := sharedTokenCount(needleTokens, note.tokens)
	containsAlpha := strings.Contains(needleAlpha, note.alpha) || strings.Contains(note.alpha, needleAlpha)
	containsPhrase := strings.Contains(needle, note.normalized) || strings.Contains(note.normalized, needle)

	// Token overlap alone is too permissive; require actual phrase evidence
	// from normalized/alphanumeric containment before suggesting a retarget.
	if !containsAlpha && !containsPhrase {
		return 0, false
	}

	score := sharedMeaningful * 20
	score += sharedAll * 5
	if containsAlpha {
		score += 30
	}
	if containsPhrase {
		score += 20
	}
	if sharedMeaningful == len(needleMeaningful) {
		score += 25
	}
	if len(note.meaningfulTokens) > 0 && sharedMeaningful == len(note.meaningfulTokens) {
		score += 10
	}
	return score, true
}

func filterMeaningfulTokens(tokens []string) []string {
	if len(tokens) == 0 {
		return nil
	}
	filtered := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if _, ok := genericBrokenLinkTokens[token]; ok {
			continue
		}
		filtered = append(filtered, token)
	}
	return filtered
}
