package noderead

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"strings"
)

// Hydrate returns node records for note-root, embedded, or section refs using
// the scope cache before falling back to catalog rows or projection.
func (s *Scope) Hydrate(ctx context.Context, refs []ontology.NodeRef, opts HydrateOptions) ([]NodeRecord, error) {
	if s == nil || s.service == nil {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hydrateLocked(ctx, refs, opts)
}

func (s *Scope) hydrateLocked(ctx context.Context, refs []ontology.NodeRef, opts HydrateOptions) ([]NodeRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	notePaths := make([]string, 0, len(refs))
	refRecords := make(map[string]NodeRecord)
	missingRefs := make([]ontology.NodeRef, 0)
	for _, ref := range refs {
		ref = NormalizeRef(ref)
		if ref.IsZero() {
			continue
		}
		if ref.Kind == "" || ref.Kind == ontology.NodeKindNote {
			notePaths = append(notePaths, ref.NotePath)
			continue
		}
		key := nodeRecordCacheKey(ref, opts.Profile)
		record, recordOK := s.recordByRef[key]
		if !recordOK && normalizedHydrateProfile(opts.Profile) == HydrateSummary {
			record, recordOK = s.recordByRef[NodeCacheKey{Ref: key.Ref, Profile: HydrateContent}]
		}
		if recordOK {
			s.diagnostics.RecordHits++
			refRecords[RefIdentityKey(ref)] = record
			continue
		}
		if s.refKnown[key] {
			s.diagnostics.RecordHits++
			continue
		}
		missingRefs = append(missingRefs, ref)
	}
	noteRecords, err := s.recordsForPaths(ctx, notePaths, opts.Profile)
	if err != nil {
		return nil, err
	}
	noteByPath := make(map[string][]NodeRecord, len(noteRecords))
	for _, record := range noteRecords {
		noteByPath[record.Path] = append(noteByPath[record.Path], record)
	}
	if len(missingRefs) > 0 {
		loaded, err := s.embeddedRecordsWithProfile(ctx, missingRefs, opts.Profile)
		if canceled := readCancellation(ctx, err); canceled != nil {
			return nil, canceled
		}
		if err != nil {
			return nil, err
		}
		for _, record := range loaded {
			key := nodeRecordCacheKey(record.Ref, opts.Profile)
			s.recordByRef[key] = record
			s.refKnown[key] = true
			refRecords[RefIdentityKey(record.Ref)] = record
		}
		aliases := indexLoadedRecordsForRequests(missingRefs, loaded)
		for _, ref := range missingRefs {
			key := nodeRecordCacheKey(ref, opts.Profile)
			if record, ok := aliases[RefIdentityKey(ref)]; ok {
				s.recordByRef[key] = record
				refRecords[RefIdentityKey(ref)] = record
			}
			s.refKnown[key] = true
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]NodeRecord, 0, len(refs))
	for _, ref := range refs {
		ref = NormalizeRef(ref)
		if ref.IsZero() {
			continue
		}
		if ref.Kind == "" || ref.Kind == ontology.NodeKindNote {
			records := noteByPath[ref.NotePath]
			if len(records) == 0 {
				continue
			}
			out = append(out, records[0])
			continue
		}
		if record, ok := refRecords[RefIdentityKey(ref)]; ok {
			out = append(out, cloneNodeRecord(record))
		}
	}
	for i := range out {
		if !profileNeedsContent(opts.Profile) {
			out[i].Content = ""
		}
	}
	return out, nil
}

func indexLoadedRecordsForRequests(requests []ontology.NodeRef, loaded []NodeRecord) map[string]NodeRecord {
	type aliasCandidate struct {
		record    NodeRecord
		identity  string
		ambiguous bool
	}
	index := make(map[string]aliasCandidate, len(loaded)*2)
	add := func(key string, record NodeRecord) {
		identity := RefIdentityKey(record.Ref)
		candidate := index[key]
		if candidate.identity == "" {
			candidate.record = record
			candidate.identity = identity
		} else if candidate.identity != identity {
			candidate.ambiguous = true
		}
		index[key] = candidate
	}
	for _, record := range loaded {
		ref := NormalizeRef(record.Ref)
		available := nodeRecordAliasMask(ref)
		for mask := 1; mask <= 7; mask++ {
			if mask&available == mask {
				add(nodeRecordAliasIndexKey(ref, mask), record)
			}
		}
		for _, alias := range record.resolvedFrom {
			alias = NormalizeRef(alias)
			mask := nodeRecordAliasMask(alias)
			if positionalSourceRefWithStructural(alias) {
				mask = 4
			}
			if mask != 0 {
				add(nodeRecordAliasIndexKey(alias, mask), record)
			}
		}
	}
	out := make(map[string]NodeRecord, len(requests))
	for _, requested := range requests {
		requested = NormalizeRef(requested)
		mask := nodeRecordAliasMask(requested)
		if positionalSourceRefWithStructural(requested) {
			mask = 4
		}
		if mask == 0 {
			continue
		}
		candidate := index[nodeRecordAliasIndexKey(requested, mask)]
		if candidate.identity != "" && !candidate.ambiguous && nodeRecordMatchesRequestedRef(candidate.record, requested) {
			out[RefIdentityKey(requested)] = candidate.record
		}
	}
	return out
}

func nodeRecordAliasMask(ref ontology.NodeRef) int {
	mask := 0
	if ref.NodeID != "" {
		mask |= 1
	}
	if ref.Fragment != "" {
		mask |= 2
	}
	if ref.Structural != "" {
		mask |= 4
	}
	return mask
}

func nodeRecordAliasIndexKey(ref ontology.NodeRef, mask int) string {
	nodeID, fragment, structural := "", "", ""
	if mask&1 != 0 {
		nodeID = strings.TrimSpace(ref.NodeID)
	}
	if mask&2 != 0 {
		fragment = strings.TrimSpace(ref.Fragment)
	}
	if mask&4 != 0 {
		structural = strings.TrimSpace(ref.Structural)
	}
	return strings.TrimSpace(ref.NotePath) + "\x00" + string(ref.Kind) + "\x00" +
		nodeID + "\x00" + fragment + "\x00" + structural
}

func nodeRecordMatchesRequestedRef(record NodeRecord, requested ontology.NodeRef) bool {
	actual := NormalizeRef(record.Ref)
	requested = NormalizeRef(requested)
	for _, alias := range record.resolvedFrom {
		if RefIdentityKey(NormalizeRef(alias)) == RefIdentityKey(requested) {
			return true
		}
	}
	if actual.NotePath != requested.NotePath || actual.Kind != requested.Kind {
		return false
	}
	if requested.NodeID == "" && requested.Fragment == "" && requested.Structural == "" {
		return false
	}
	if positionalSourceRefWithStructural(requested) {
		return actual.Structural == requested.Structural
	}
	if requested.NodeID != "" && actual.NodeID != requested.NodeID {
		return false
	}
	if requested.Fragment != "" && actual.Fragment != requested.Fragment {
		return false
	}
	if requested.Structural != "" && actual.Structural != requested.Structural {
		return false
	}
	return true
}

func nodeRecordCacheKey(ref ontology.NodeRef, profile HydrateProfile) NodeCacheKey {
	return NodeCacheKey{Ref: NormalizeRef(ref), Profile: normalizedHydrateProfile(profile)}
}

func normalizedHydrateProfile(profile HydrateProfile) HydrateProfile {
	if profile == HydrateContent {
		return HydrateContent
	}
	return HydrateSummary
}
