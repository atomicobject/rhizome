package search

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
)

// MergeCandidate combines two candidates with the same handle identity.
// Evidence lists are merged with bounded growth (max 12 total, 3 per type).
// Metadata fields use first-non-empty semantics: existing values are kept,
// missing fields are filled from the incoming candidate.
//
// Ordering: call as `MergeCandidate(existing, incoming)` where existing
// is the primary record and incoming provides supplementary evidence/metadata.
func MergeCandidate(a, b Candidate) Candidate {
	out := a
	if out.Handle.String() == "" {
		out.Handle = b.Handle
	}
	if out.Owner.String() == "" {
		out.Owner = b.Owner
	}
	out.Evidence = mergeEvidenceBounded(out.Evidence, b.Evidence)
	out.SupportOnly = out.SupportOnly && b.SupportOnly

	if strings.TrimSpace(out.Type) == "" {
		out.Type = b.Type
	}
	out.SourceRef.FillFrom(b.SourceRef)
	if out.DocClass == "" || out.DocClass == DocClassNone {
		out.DocClass = b.DocClass
	}
	if !out.PrimaryDoc {
		out.PrimaryDoc = b.PrimaryDoc
	}
	if out.handleKey == "" {
		out.handleKey = b.handleKey
	}
	if out.handleKey == "" {
		out.handleKey = out.Handle.String()
	}
	return out
}

// coalesceNoteCandidates merges each note's whole-note candidates into one
// before ranking, so lanes that found the same note under different handles (a
// vector chunk and a title match) add up instead of being scored apart. A
// whole-note candidate is a note-type candidate whose canonical identity is the
// note path; embedded and section nodes keep their own identity, and Intel FTS
// sections have their own type. The member with the strongest semantic
// evidence is the base, so the best-matching chunk remains the displayed node.
// MergeCandidate dedupes repeated facts, so several chunks of one note count
// their similarity once; the note keeps one query_specificity, the strongest
// member's, which the ranker recomputes and the deadline fallback must not stack.
func coalesceNoteCandidates(candidates []Candidate) []Candidate {
	wholeNote := func(c Candidate) bool {
		return c.Type == "note" && c.Owner.String() != "" && (c.NodeRef == nil || c.NodeRef.Kind == "" || c.NodeRef.Kind == ontology.NodeKindNote)
	}
	groups := make(map[string][]Candidate)
	for _, c := range candidates {
		if wholeNote(c) {
			groups[c.Owner.String()] = append(groups[c.Owner.String()], c)
		}
	}
	out := make([]Candidate, 0, len(candidates))
	emitted := make(map[string]bool, len(groups))
	for _, c := range candidates {
		key := c.Owner.String()
		members := groups[key]
		if !wholeNote(c) || len(members) < 2 {
			out = append(out, c)
			continue
		}
		if emitted[key] {
			continue
		}
		emitted[key] = true
		type semanticMember struct {
			candidate Candidate
			score     float64
		}
		ordered := make([]semanticMember, len(members))
		for i, member := range members {
			ordered[i] = semanticMember{member, AggregateEvidenceScoresForRanking(member.Evidence)[EvidenceChannelSemantic]}
		}
		sort.SliceStable(ordered, func(i, j int) bool {
			if ordered[i].score != ordered[j].score {
				return ordered[i].score > ordered[j].score
			}
			return ordered[i].candidate.Handle.String() < ordered[j].candidate.Handle.String()
		})
		for i := range ordered {
			members[i] = ordered[i].candidate
		}
		// A member's specificity can read evidence the others lack, such as
		// an agreed link label, so the note keeps the strongest one.
		var specificity *Evidence
		for _, member := range members {
			for _, ev := range member.Evidence {
				if ev.Type == "query_specificity" && (specificity == nil || ev.RawScore > specificity.RawScore) {
					specificity = &ev
				}
			}
		}
		merged := members[0]
		merged.Evidence = withoutEvidenceType(merged.Evidence, "query_specificity")
		for _, member := range members[1:] {
			member.Evidence = withoutEvidenceType(member.Evidence, "query_specificity")
			merged = MergeCandidate(merged, member)
		}
		if specificity != nil {
			merged.Evidence = mergeEvidenceBounded(merged.Evidence, []Evidence{*specificity})
		}
		out = append(out, merged)
	}
	return out
}

func withoutEvidenceType(evidence []Evidence, typ string) []Evidence {
	out := make([]Evidence, 0, len(evidence))
	for _, ev := range evidence {
		if ev.Type != typ {
			out = append(out, ev)
		}
	}
	return out
}

const (
	maxEvidenceTotal   = 12
	maxEvidencePerType = 3
)

// mergeEvidenceBounded merges evidence lists with bounded growth to prevent
// hot-path allocations. Keeps the best signals per evidence type.
//
// This bound is part of search latency control: graph/refs expansion can
// attach many facts to a popular handle, but ranking only needs the strongest
// few signals per evidence type. Preserve provenance in Details where possible
// rather than raising these caps casually.
//
// Limits:
//   - maxEvidenceTotal (12): hard cap on total evidence items
//   - maxEvidencePerType (3): max items per evidence type before capping
//
// Strategy: group by Type, keep top N by RawScore, then globally cap.
func mergeEvidenceBounded(existing, incoming []Evidence) []Evidence {
	combined := append(existing, incoming...)
	if len(combined) == 0 {
		return combined
	}
	byKey := make(map[string]Evidence, len(combined))
	for _, ev := range normalizeEvidenceSlice(combined) {
		key := evidenceFactKey(ev)
		if existing, ok := byKey[key]; ok {
			byKey[key] = mergeDuplicateEvidence(existing, ev)
		} else {
			byKey[key] = ev
		}
	}
	type rankedEvidence struct {
		evidence Evidence
		score    float64
		key      string
	}
	ordered := make([]rankedEvidence, 0, len(byKey))
	for key, ev := range byKey {
		ordered = append(ordered, rankedEvidence{ev, EvidenceScore(ev), key})
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].score != ordered[j].score {
			return ordered[i].score > ordered[j].score
		}
		if ordered[i].evidence.Type != ordered[j].evidence.Type {
			return ordered[i].evidence.Type < ordered[j].evidence.Type
		}
		return ordered[i].key < ordered[j].key
	})
	out := make([]Evidence, 0, min(len(combined), maxEvidenceTotal))
	perType := make(map[string]int)
	for _, item := range ordered {
		if perType[item.evidence.Type] >= maxEvidencePerType {
			continue
		}
		perType[item.evidence.Type]++
		out = append(out, item.evidence)
		if len(out) == maxEvidenceTotal {
			break
		}
	}
	return out
}

func mergeDuplicateEvidence(a, b Evidence) Evidence {
	winner, other := a, b
	if EvidenceScore(b) > EvidenceScore(a) || (EvidenceScore(b) == EvidenceScore(a) && canonicalEvidenceFactValue(b) < canonicalEvidenceFactValue(a)) {
		winner, other = b, a
	}
	winner.Details = mergeEvidenceDetails(winner, other)
	winner.Source = mergeProvenance(a.Source, b.Source)
	return winner
}

func mergeEvidenceDetails(a, b Evidence) map[string]string {
	details := make(map[string]string, len(a.Details)+2)
	for _, ev := range []Evidence{a, b} {
		for key, value := range ev.Details {
			if isEvidenceOperationalDetail(key) {
				continue
			}
			if prior, ok := details[key]; !ok || value < prior {
				details[key] = value
			}
		}
	}
	return details
}

func canonicalEvidenceFactValue(ev Evidence) string {
	copy := ev
	copy.Source = ""
	copy.Details = make(map[string]string, len(ev.Details))
	for key, value := range ev.Details {
		if !isEvidenceOperationalDetail(key) {
			copy.Details[key] = value
		}
	}
	b, _ := json.Marshal(copy)
	return string(b)
}

func evidenceFactKey(ev Evidence) string {
	details := make(map[string]string, len(ev.Details))
	for key, value := range ev.Details {
		if isEvidenceOperationalDetail(key) {
			continue
		}
		details[key] = value
	}
	encoded, _ := json.Marshal(details)
	return strings.Join([]string{normalizeEvidenceKey(ev.Type), string(ev.Channel), string(encoded)}, "\x00")
}

func isEvidenceOperationalDetail(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "provenance":
		return true
	default:
		return false
	}
}

func mergeProvenance(a, b string) string {
	seen := map[string]struct{}{}
	for _, source := range strings.Split(a+","+b, ",") {
		if source = strings.TrimSpace(source); source != "" {
			seen[source] = struct{}{}
		}
	}
	sources := make([]string, 0, len(seen))
	for source := range seen {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	return strings.Join(sources, ",")
}

func normalizeEvidenceSlice(evidence []Evidence) []Evidence {
	if len(evidence) == 0 {
		return evidence
	}
	out := make([]Evidence, len(evidence))
	for i, ev := range evidence {
		norm, err := NormalizeEvidence(ev)
		if err != nil {
			out[i] = ev
			continue
		}
		out[i] = norm
	}
	return out
}
