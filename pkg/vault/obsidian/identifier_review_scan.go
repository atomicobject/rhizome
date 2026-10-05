package obsidian

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// IdentifierReviewRegion distinguishes narrative mentions from source-code
// tokens. Both are review-only and never automatic edits.
type IdentifierReviewRegion string

const (
	IdentifierReviewProse IdentifierReviewRegion = "PROSE"
	IdentifierReviewCode  IdentifierReviewRegion = "CODE"
)

// IdentifierReviewCandidate is one bounded authored token outside already
// handled structured fields and links.
type IdentifierReviewCandidate struct {
	Identifier string                 `json:"identifier"`
	Value      string                 `json:"value"`
	Span       StructuredLinkSpan     `json:"span"`
	Region     IdentifierReviewRegion `json:"region"`
}

type identifierReviewScanStats struct {
	RuneSteps          int
	FailureSteps       int
	ExcludedSpanSteps  int
	ProtectedSpanSteps int
}

// ScanIdentifierReviewCandidates discovers bounded identifier mentions in one
// pass over a complete sealed link scan. handledFieldRanges must be exact
// structured-field value ranges from the same source snapshot.
func ScanIdentifierReviewCandidates(content string, snapshot StructuredLinkScanSnapshot, identifiers []string, handledFieldRanges []StructuredLinkSpan) ([]IdentifierReviewCandidate, error) {
	candidates, _, err := scanIdentifierReviewCandidates(content, snapshot, identifiers, handledFieldRanges)
	return candidates, err
}

func scanIdentifierReviewCandidates(content string, snapshot StructuredLinkScanSnapshot, identifiers []string, handledFieldRanges []StructuredLinkSpan) ([]IdentifierReviewCandidate, identifierReviewScanStats, error) {
	validated, err := snapshot.ValidatedSnapshot()
	if err != nil {
		return nil, identifierReviewScanStats{}, err
	}
	if StructuredLinkSourceFingerprint(content) != validated.SourceFingerprint {
		return nil, identifierReviewScanStats{}, fmt.Errorf("structured link scan source does not match review candidate content")
	}
	excluded := make([]StructuredLinkSpan, 0, len(validated.Links)+len(handledFieldRanges))
	for _, link := range validated.Links {
		excluded = append(excluded, link.RawSpan)
	}
	for _, span := range handledFieldRanges {
		if !span.Valid() || span.End > len(content) {
			return nil, identifierReviewScanStats{}, fmt.Errorf("identifier review handled range is outside source content")
		}
		excluded = append(excluded, span)
	}
	excluded = mergeProtectedSpans(excluded)

	matcher := newIdentifierReviewMatcher(identifiers)
	if len(matcher.patterns) == 0 {
		return nil, identifierReviewScanStats{}, nil
	}
	runes, offsets := contentRunesAndOffsets(content)
	matches, stats := matcher.find(runes)
	candidates := make([]IdentifierReviewCandidate, 0, len(matches))
	excludedIndex, protectedIndex := 0, 0
	for _, match := range matches {
		start, end := offsets[match.start], offsets[match.end]
		if !identifierReviewBoundary(content, start, end) {
			continue
		}
		for excludedIndex < len(excluded) && excluded[excludedIndex].End <= start {
			excludedIndex++
			stats.ExcludedSpanSteps++
		}
		if excludedIndex < len(excluded) && excluded[excludedIndex].Start < end {
			continue
		}
		for protectedIndex < len(validated.protected) && validated.protected[protectedIndex].End <= start {
			protectedIndex++
			stats.ProtectedSpanSteps++
		}
		region := IdentifierReviewProse
		if protectedIndex < len(validated.protected) && validated.protected[protectedIndex].Start <= start && end <= validated.protected[protectedIndex].End {
			region = IdentifierReviewCode
		}
		candidates = append(candidates, IdentifierReviewCandidate{
			Identifier: match.identifier, Value: content[start:end], Span: linkSpan(start, end), Region: region,
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Span.Start != candidates[j].Span.Start {
			return candidates[i].Span.Start < candidates[j].Span.Start
		}
		if candidates[i].Span.End != candidates[j].Span.End {
			return candidates[i].Span.End < candidates[j].Span.End
		}
		return candidates[i].Identifier < candidates[j].Identifier
	})
	return candidates, stats, nil
}

type identifierReviewPattern struct {
	identifier string
	length     int
}

type identifierReviewNode struct {
	next    map[rune]int
	failure int
	outputs []int
}

type identifierReviewMatcher struct {
	nodes    []identifierReviewNode
	patterns []identifierReviewPattern
}

func newIdentifierReviewMatcher(identifiers []string) identifierReviewMatcher {
	matcher := identifierReviewMatcher{nodes: []identifierReviewNode{{next: make(map[rune]int)}}}
	unique := make(map[string]struct{}, len(identifiers))
	for _, raw := range identifiers {
		identifier := strings.ToLower(strings.TrimSpace(raw))
		if identifier == "" {
			continue
		}
		if _, duplicate := unique[identifier]; duplicate {
			continue
		}
		unique[identifier] = struct{}{}
		patternIndex := len(matcher.patterns)
		patternRunes := []rune(identifier)
		matcher.patterns = append(matcher.patterns, identifierReviewPattern{identifier: identifier, length: len(patternRunes)})
		node := 0
		for _, value := range patternRunes {
			value = unicode.ToLower(value)
			next, found := matcher.nodes[node].next[value]
			if !found {
				next = len(matcher.nodes)
				matcher.nodes[node].next[value] = next
				matcher.nodes = append(matcher.nodes, identifierReviewNode{next: make(map[rune]int)})
			}
			node = next
		}
		matcher.nodes[node].outputs = append(matcher.nodes[node].outputs, patternIndex)
	}
	matcher.buildFailures()
	return matcher
}

func (m *identifierReviewMatcher) buildFailures() {
	queue := make([]int, 0, len(m.nodes))
	for _, child := range m.nodes[0].next {
		queue = append(queue, child)
	}
	for cursor := 0; cursor < len(queue); cursor++ {
		parent := queue[cursor]
		for value, child := range m.nodes[parent].next {
			failure := m.nodes[parent].failure
			for failure != 0 {
				if _, found := m.nodes[failure].next[value]; found {
					break
				}
				failure = m.nodes[failure].failure
			}
			if next, found := m.nodes[failure].next[value]; found && next != child {
				m.nodes[child].failure = next
			}
			m.nodes[child].outputs = append(m.nodes[child].outputs, m.nodes[m.nodes[child].failure].outputs...)
			queue = append(queue, child)
		}
	}
}

type identifierReviewMatch struct {
	identifier string
	start      int
	end        int
}

func (m identifierReviewMatcher) find(input []rune) ([]identifierReviewMatch, identifierReviewScanStats) {
	var matches []identifierReviewMatch
	stats := identifierReviewScanStats{}
	state := 0
	for index, raw := range input {
		stats.RuneSteps++
		value := unicode.ToLower(raw)
		for state != 0 {
			if _, found := m.nodes[state].next[value]; found {
				break
			}
			state = m.nodes[state].failure
			stats.FailureSteps++
		}
		if next, found := m.nodes[state].next[value]; found {
			state = next
		}
		for _, patternIndex := range m.nodes[state].outputs {
			pattern := m.patterns[patternIndex]
			matches = append(matches, identifierReviewMatch{identifier: pattern.identifier, start: index - pattern.length + 1, end: index + 1})
		}
	}
	return matches, stats
}

func contentRunesAndOffsets(content string) ([]rune, []int) {
	runes := make([]rune, 0, utf8.RuneCountInString(content))
	offsets := make([]int, 0, cap(runes)+1)
	for offset, value := range content {
		offsets = append(offsets, offset)
		runes = append(runes, value)
	}
	offsets = append(offsets, len(content))
	return runes, offsets
}

func identifierReviewBoundary(content string, start, end int) bool {
	if start > 0 {
		value, _ := utf8.DecodeLastRuneInString(content[:start])
		if unicode.IsLetter(value) || unicode.IsDigit(value) {
			return false
		}
	}
	if end < len(content) {
		value, _ := utf8.DecodeRuneInString(content[end:])
		if unicode.IsLetter(value) || unicode.IsDigit(value) {
			return false
		}
	}
	return true
}
