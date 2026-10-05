package validate

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/atomicobject/rhizome/pkg/notemeta"
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

type brokenLinkGroupKey struct {
	target   string
	reason   obsidian.BrokenLinkReason
	fragment string
	markdown bool
}

// brokenLinkGroup is every occurrence of one authored broken target plus the
// retarget evidence computed once for issues and repair actions alike.
type brokenLinkGroup struct {
	key     brokenLinkGroupKey
	links   []obsidian.BrokenLink
	sources []string
	// strong candidates carry enough evidence to confirm a rewrite: a
	// normalized title match or a git rename. weak candidates are title
	// containment or token overlap and only inform an agent.
	strong []string
	weak   []string
}

// Candidate confidence labels on issue data and repair actions.
const (
	candidateConfidenceHigh = "high"
	candidateConfidenceLow  = "low"
)

// retargetEvidence supplies what candidate filtering needs beyond titles.
// A nil noteContent means fragments cannot be verified, so fragment-bearing
// links never get a strong candidate.
type retargetEvidence struct {
	history      linkHistory
	noteMetadata notemeta.Indexer
	noteContent  func(string) (string, error)
}

func (g brokenLinkGroup) candidates() ([]string, string) {
	switch {
	case len(g.strong) > 0:
		return g.strong, candidateConfidenceHigh
	case len(g.weak) > 0:
		return g.weak, candidateConfidenceLow
	default:
		return nil, ""
	}
}

var periodicNoteName = regexp.MustCompile(`(?i)^\d{4}(-\d{2}(-\d{2})?|-w\d{2}|-q[1-4])?$`)

// attachmentTarget reports a link that names a non-note file. Only a short
// alphanumeric suffix counts: the dot in `fetch.ai call` is part of a name.
var attachmentSuffix = regexp.MustCompile(`(?i)\.[a-z][a-z0-9]{0,4}$`)

func attachmentTarget(target string) bool {
	ext := path.Ext(path.Base(target))
	return ext != "" && !strings.EqualFold(ext, ".md") && attachmentSuffix.MatchString(ext)
}

func groupBrokenLinks(ctx context.Context, broken []obsidian.BrokenLink, allNotes []string, matcher brokenLinkMatcherIndex, evidence retargetEvidence) ([]brokenLinkGroup, error) {
	byKey := map[brokenLinkGroupKey][]obsidian.BrokenLink{}
	for _, link := range broken {
		key := brokenLinkGroupKey{
			target:   link.Target,
			reason:   normalizeBrokenLinkReason(link.Reason),
			fragment: link.Fragment,
			markdown: link.Markdown,
		}
		byKey[key] = append(byKey[key], link)
	}
	keys := make([]brokenLinkGroupKey, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].target != keys[j].target {
			return keys[i].target < keys[j].target
		}
		if keys[i].reason != keys[j].reason {
			return keys[i].reason < keys[j].reason
		}
		if keys[i].fragment != keys[j].fragment {
			return keys[i].fragment < keys[j].fragment
		}
		return !keys[i].markdown && keys[j].markdown
	})
	current := make(map[string]struct{}, len(allNotes))
	for _, notePath := range allNotes {
		current[notePath] = struct{}{}
	}
	groups := make([]brokenLinkGroup, 0, len(keys))
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		links := byKey[key]
		sort.SliceStable(links, func(i, j int) bool {
			if links[i].Source != links[j].Source {
				return links[i].Source < links[j].Source
			}
			if links[i].LinkType != links[j].LinkType {
				return links[i].LinkType < links[j].LinkType
			}
			return links[i].Alias < links[j].Alias
		})
		group := brokenLinkGroup{key: key, links: links}
		seen := map[string]struct{}{}
		for _, link := range links {
			if _, ok := seen[link.Source]; !ok {
				group.sources = append(group.sources, link.Source)
				seen[link.Source] = struct{}{}
			}
		}
		sort.Strings(group.sources)
		var err error
		group.strong, group.weak, err = retargetCandidates(ctx, group, matcher, current, evidence)
		if err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	return groups, nil
}

// retargetCandidates applies the evidence rules for one group: never the
// source note, never across the attachment and note boundary, never between
// a bare periodic date and a dated meeting, and never to a note that lacks
// the link's heading or block.
func retargetCandidates(ctx context.Context, group brokenLinkGroup, matcher brokenLinkMatcherIndex, current map[string]struct{}, evidence retargetEvidence) ([]string, []string, error) {
	if group.key.markdown || attachmentTarget(group.key.target) {
		return nil, nil, nil
	}
	if group.key.reason != obsidian.BrokenLinkReasonNoteMissing {
		// The note exists; its path is evidence for choosing a fragment, never
		// a retarget.
		exact, _, err := matcher.findCandidateTiers(ctx, group.key.target)
		return nil, exact, err
	}
	exact, fuzzy, err := matcher.findCandidateTiers(ctx, group.key.target)
	if err != nil {
		return nil, nil, err
	}
	if destination, commit, ok := evidence.history.renameDestination(group.key.target, func(p string) bool {
		_, ok := current[p]
		return ok
	}); ok {
		orphaned := false
		for _, source := range group.sources {
			linkedBefore, err := evidence.history.linkPredatesRename(ctx, evidence.noteMetadata, commit, source, group.key.target)
			if err != nil {
				return nil, nil, err
			}
			if linkedBefore {
				orphaned = true
				break
			}
		}
		if orphaned {
			exact = []string{destination}
		} else {
			fuzzy = append([]string{destination}, slices.DeleteFunc(fuzzy, func(candidate string) bool {
				return candidate == destination
			})...)
		}
	}
	isSource := map[string]struct{}{}
	for _, source := range group.sources {
		isSource[source] = struct{}{}
	}
	targetPeriodic := periodicNoteName.MatchString(path.Base(group.key.target))
	eligible := func(candidate string) bool {
		if _, self := isSource[candidate]; self {
			return false
		}
		base := strings.TrimSuffix(path.Base(candidate), path.Ext(candidate))
		return periodicNoteName.MatchString(base) == targetPeriodic
	}
	hasFragment := func(candidate string) bool {
		if group.key.fragment == "" {
			return true
		}
		if evidence.noteContent == nil {
			return false
		}
		content, err := evidence.noteContent(candidate)
		return err == nil && obsidian.NoteHasFragment(content, group.key.fragment)
	}
	var strong, weak []string
	for _, candidate := range exact {
		switch {
		case !eligible(candidate):
		case hasFragment(candidate):
			strong = append(strong, candidate)
		default:
			// A title match without the linked heading or block is evidence only.
			weak = append(weak, candidate)
		}
	}
	for _, candidate := range fuzzy {
		if eligible(candidate) {
			weak = append(weak, candidate)
		}
	}
	return strong, weak, nil
}

func buildBrokenLinkFixesFromGroups(ctx context.Context, groups []brokenLinkGroup, allNotes []string, candidateErr error) ([]FixAction, error) {
	actions := make([]FixAction, 0, len(groups))
	for _, group := range groups {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		authoredTarget := authoredBrokenLinkTarget(group.key.target, group.key.fragment)
		issueKeys := make([]string, 0, len(group.links))
		for _, link := range group.links {
			key, err := StableIssueKey(CheckBrokenLinks, brokenLinkValidationIssue(link, group))
			if err != nil {
				return nil, err
			}
			issueKeys = append(issueKeys, key)
		}
		issueKeys = sortedUnique(issueKeys)

		if len(group.strong) != 1 {
			actions = append(actions, buildBrokenLinkReviewAction(group, authoredTarget, issueKeys, candidateErr))
			continue
		}
		rewriteTarget := minimalBrokenLinkRewriteTarget(group.strong[0], allNotes)
		if group.key.target == rewriteTarget {
			actions = append(actions, buildBrokenLinkReviewAction(group, authoredTarget, issueKeys, candidateErr))
			continue
		}

		rewrittenAuthoredTarget := authoredBrokenLinkTarget(rewriteTarget, group.key.fragment)
		edits := make([]FixEdit, 0, len(group.links))
		for _, link := range group.links {
			edits = append(edits, FixEdit{
				Kind:            FixKindRewriteLinkGroup,
				NotePath:        link.Source,
				OldTarget:       authoredTarget,
				NewTarget:       rewrittenAuthoredTarget,
				PreserveDisplay: true,
			})
		}
		actions = append(actions, FixAction{
			ID:             "broken-link:" + string(group.key.reason) + ":" + authoredTarget + "->" + rewriteTarget,
			Check:          CheckBrokenLinks,
			IssueCode:      brokenLinkIssueCode(group.key.reason),
			Kind:           FixKindRewriteLinkGroup,
			Safety:         FixSafetyConfirm,
			Confidence:     candidateConfidenceHigh,
			Title:          fmt.Sprintf("Retarget broken link [[%s]]", authoredTarget),
			Summary:        fmt.Sprintf("update %d broken link instances to %s; the authored text stays as the display alias and the fragment is preserved", len(group.links), rewrittenAuthoredTarget),
			Question:       fmt.Sprintf("Update %d broken link instances targeting [[%s]] to [[%s|%s]]?", len(group.links), authoredTarget, strings.TrimSuffix(rewrittenAuthoredTarget, ".md"), authoredTarget),
			InstanceCount:  len(group.links),
			IssueKeys:      issueKeys,
			AffectedPaths:  group.sources,
			CandidatePaths: group.strong,
			Edits:          edits,
		})
	}
	return actions, nil
}

func authoredBrokenLinkTarget(target, fragment string) string {
	if fragment == "" {
		return target
	}
	return target + "#" + fragment
}

func buildBrokenLinkReviewAction(group brokenLinkGroup, authoredTarget string, issueKeys []string, candidateErr error) FixAction {
	command := brokenLinkReviewCommand(group.key, authoredTarget)
	candidates, confidence := group.candidates()
	if group.key.reason != obsidian.BrokenLinkReasonNoteMissing {
		confidence = ""
	}
	reason := brokenLinkReviewReason(group, candidateErr)
	return FixAction{
		ID:             "broken-link:" + string(group.key.reason) + ":" + authoredTarget + ":review",
		Check:          CheckBrokenLinks,
		IssueCode:      brokenLinkIssueCode(group.key.reason),
		Kind:           FixKindReviewBrokenLink,
		Safety:         FixSafetyAgent,
		Confidence:     confidence,
		Title:          fmt.Sprintf("Resolve broken link [[%s]]", authoredTarget),
		Summary:        fmt.Sprintf("Broken target [[%s]]: %s Next: %s", authoredTarget, reason, command),
		InstanceCount:  len(group.links),
		IssueKeys:      issueKeys,
		AffectedPaths:  group.sources,
		CandidatePaths: candidates,
	}
}

func brokenLinkReviewReason(group brokenLinkGroup, candidateErr error) string {
	switch group.key.reason {
	case obsidian.BrokenLinkReasonHeadingMissing:
		return fmt.Sprintf("Heading fragment %q is missing; choosing replacement content requires an agent.", group.key.fragment)
	case obsidian.BrokenLinkReasonBlockMissing:
		return fmt.Sprintf("Block fragment %q is missing; choosing or creating a stable block ID requires an agent.", group.key.fragment)
	default:
		if candidateErr != nil {
			return fmt.Sprintf("Candidate evidence is unavailable because the note catalog could not be read: %v.", candidateErr)
		}
		switch {
		case len(group.strong) == 1:
			return "The only candidate would not change the authored target, so no rewrite edit was emitted."
		case len(group.strong) > 1:
			return fmt.Sprintf("%d candidate notes match the title; selecting the intended target requires an agent.", len(group.strong))
		case len(group.weak) > 0:
			return fmt.Sprintf("%d low-confidence candidate(s) share only part of the title; a partial title match is not enough evidence to retarget.", len(group.weak))
		default:
			return "No candidate note was found; creating or selecting a target requires an agent."
		}
	}
}

func brokenLinkReviewCommand(group brokenLinkGroupKey, authoredTarget string) string {
	if group.reason == obsidian.BrokenLinkReasonHeadingMissing || group.reason == obsidian.BrokenLinkReasonBlockMissing {
		return "rzm agent node-link --target " + shellQuoteCommandArg(authoredTarget) + " --ensure plan"
	}
	return "rzm agent semantic-query --query " + shellQuoteCommandArg(group.target)
}

func minimalBrokenLinkRewriteTarget(candidate string, allNotes []string) string {
	base := strings.TrimSuffix(filepath.Base(candidate), filepath.Ext(candidate))
	if base == "" {
		return candidate
	}

	baseKey := obsidian.NormalizeForComparison(base)
	matches := 0
	for _, notePath := range allNotes {
		noteBase := strings.TrimSuffix(filepath.Base(notePath), filepath.Ext(notePath))
		if obsidian.NormalizeForComparison(noteBase) == baseKey {
			matches++
			if matches > 1 {
				return candidate
			}
		}
	}

	return base
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
			normalized:       obsidian.NormalizeName(title),
			alpha:            alphanumOnly(obsidian.NormalizeName(title)),
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
	needle := obsidian.NormalizeName(filepath.Base(strings.TrimSpace(target)))
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
