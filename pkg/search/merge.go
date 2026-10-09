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
	if strings.TrimSpace(out.Path) == "" {
		out.Path = b.Path
	}
	if strings.TrimSpace(out.Title) == "" {
		out.Title = b.Title
	}
	if strings.TrimSpace(out.Symbol) == "" {
		out.Symbol = b.Symbol
	}
	if strings.TrimSpace(out.FQN) == "" {
		out.FQN = b.FQN
	}
	if strings.TrimSpace(out.Kind) == "" {
		out.Kind = b.Kind
	}
	if strings.TrimSpace(out.Granularity) == "" {
		out.Granularity = b.Granularity
	}
	if out.ChunkIndex < 0 && b.ChunkIndex >= 0 {
		out.ChunkIndex = b.ChunkIndex
	}
	if strings.TrimSpace(out.Breadcrumb) == "" {
		out.Breadcrumb = b.Breadcrumb
	}
	if strings.TrimSpace(out.Heading) == "" {
		out.Heading = b.Heading
	}
	if strings.TrimSpace(out.AnchorID) == "" {
		out.AnchorID = b.AnchorID
	}
	if strings.TrimSpace(out.NoteID) == "" {
		out.NoteID = b.NoteID
	}
	if strings.TrimSpace(out.NodeID) == "" {
		out.NodeID = b.NodeID
	}
	if strings.TrimSpace(out.NodeRefJSON) == "" {
		out.NodeRefJSON = b.NodeRefJSON
	}
	if strings.TrimSpace(out.SourceLocator) == "" {
		out.SourceLocator = b.SourceLocator
	}
	if strings.TrimSpace(out.NodeKind) == "" {
		out.NodeKind = b.NodeKind
	}
	if strings.TrimSpace(out.NodeType) == "" {
		out.NodeType = b.NodeType
	}
	if strings.TrimSpace(out.ParentNodeID) == "" {
		out.ParentNodeID = b.ParentNodeID
	}
	if out.DocClass == "" || out.DocClass == DocClassNone {
		out.DocClass = b.DocClass
	}
	if !out.PrimaryDoc {
		out.PrimaryDoc = b.PrimaryDoc
	}
	if out.NodeRef == nil {
		out.NodeRef = b.NodeRef
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
		semantic := func(c Candidate) float64 {
			return AggregateEvidenceScoresForRanking(c.Evidence)[EvidenceChannelSemantic]
		}
		sort.SliceStable(members, func(i, j int) bool {
			if si, sj := semantic(members[i]), semantic(members[j]); si != sj {
				return si > sj
			}
			return members[i].Handle.String() < members[j].Handle.String()
		})
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
			merged.Evidence = append(merged.Evidence, *specificity)
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
	combined = dedupeEvidenceFacts(normalizeEvidenceSlice(combined))
	buckets := make(map[string][]Evidence, len(combined))
	for _, ev := range combined {
		buckets[ev.Type] = append(buckets[ev.Type], ev)
	}
	out := make([]Evidence, 0, min(len(combined), maxEvidenceTotal))
	for _, bucket := range buckets {
		sort.Slice(bucket, func(i, j int) bool {
			if EvidenceScore(bucket[i]) != EvidenceScore(bucket[j]) {
				return EvidenceScore(bucket[i]) > EvidenceScore(bucket[j])
			}
			return evidenceFactKey(bucket[i]) < evidenceFactKey(bucket[j])
		})
		if len(bucket) > maxEvidencePerType {
			bucket = bucket[:maxEvidencePerType]
		}
		out = append(out, bucket...)
	}
	sort.Slice(out, func(i, j int) bool { return evidenceLess(out[i], out[j]) })
	if len(out) > maxEvidenceTotal {
		out = out[:maxEvidenceTotal]
	}
	return out
}

// dedupeEvidenceFacts prevents repeated observations of one fact from becoming
// independent ranking votes. Retriever provenance is retained in Source, while
// the fact identity is the signal type plus its semantic details. Operational
// details such as lane rank do not make the underlying fact independent.
func dedupeEvidenceFacts(evidence []Evidence) []Evidence {
	if len(evidence) < 2 {
		return evidence
	}
	byKey := make(map[string]Evidence, len(evidence))
	for _, ev := range evidence {
		key := evidenceFactKey(ev)
		if prior, ok := byKey[key]; ok {
			byKey[key] = mergeDuplicateEvidence(prior, ev)
			continue
		}
		byKey[key] = ev
	}
	out := make([]Evidence, 0, len(byKey))
	for _, ev := range byKey {
		out = append(out, ev)
	}
	sort.Slice(out, func(i, j int) bool { return evidenceLess(out[i], out[j]) })
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
	ranks := map[string]struct{}{}
	collect := func(ev Evidence) {
		if prior := strings.TrimSpace(ev.Details["lane_ranks"]); prior != "" {
			for _, rank := range strings.Split(prior, ",") {
				if rank = strings.TrimSpace(rank); rank != "" {
					ranks[rank] = struct{}{}
				}
			}
			return
		}
		rank := strings.TrimSpace(ev.Details["rank"])
		if rank == "" {
			rank = strings.TrimSpace(ev.Details["lane_rank"])
		}
		if rank != "" {
			sources := strings.Split(ev.Source, ",")
			if len(sources) == 0 || strings.TrimSpace(sources[0]) == "" {
				ranks[rank] = struct{}{}
			} else {
				for _, source := range sources {
					if source = strings.TrimSpace(source); source != "" {
						ranks[source+"="+rank] = struct{}{}
					}
				}
			}
		}
	}
	collect(a)
	collect(b)
	if len(ranks) > 0 {
		ordered := make([]string, 0, len(ranks))
		for rank := range ranks {
			ordered = append(ordered, rank)
		}
		sort.Strings(ordered)
		details["lane_ranks"] = strings.Join(ordered, ",")
	}
	return details
}

func evidenceLess(a, b Evidence) bool {
	if EvidenceScore(a) != EvidenceScore(b) {
		return EvidenceScore(a) > EvidenceScore(b)
	}
	if a.Type != b.Type {
		return a.Type < b.Type
	}
	if evidenceFactKey(a) != evidenceFactKey(b) {
		return evidenceFactKey(a) < evidenceFactKey(b)
	}
	return canonicalEvidence(a) < canonicalEvidence(b)
}

func canonicalEvidence(ev Evidence) string {
	copy := ev
	copy.Source = strings.Join(sortedStrings(strings.Split(ev.Source, ",")), ",")
	b, _ := json.Marshal(copy)
	return string(b)
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

func sortedStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
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
	case "rank", "lane_rank", "lane_ranks", "retriever", "provenance":
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
