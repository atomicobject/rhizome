package noderead

import (
	"context"
	"strings"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
)

// Hydrate returns node records for note-root, embedded, or section refs using
// the scope cache before falling back to catalog rows or projection.
func (s *Scope) Hydrate(ctx context.Context, refs []ontology.NodeRef, opts HydrateOptions) ([]NodeRecord, error) {
	if s == nil || s.service == nil {
		return nil, nil
	}
	// Docs: [[noderef-batch-traversal-read-api#^spec-0022-us7-ac2]] defines
	// catalog-first embedded hydration; content/workspace profiles may still
	// project source per [[noderef-batch-traversal-read-api#^spec-0022-us7-ac3]].
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	notePaths := make([]string, 0, len(refs))
	refRecords := make(map[string]NodeRecord)
	missingRefs := make([]ontology.NodeRef, 0)
	for _, ref := range refs {
		ref = normalizeNodeRef(ref)
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
			refRecords[nodeRefIdentityKey(ref)] = cloneNodeRecord(record)
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
			s.recordByRef[key] = cloneNodeRecord(record)
			s.refKnown[key] = true
			refRecords[nodeRefIdentityKey(record.Ref)] = cloneNodeRecord(record)
		}
		aliases := indexLoadedRecordsForRequests(missingRefs, loaded)
		for _, ref := range missingRefs {
			key := nodeRecordCacheKey(ref, opts.Profile)
			if record, ok := aliases[nodeRefIdentityKey(ref)]; ok {
				s.recordByRef[key] = cloneNodeRecord(record)
				refRecords[nodeRefIdentityKey(ref)] = cloneNodeRecord(record)
			}
			s.refKnown[key] = true
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]NodeRecord, 0, len(refs))
	for _, ref := range refs {
		ref = normalizeNodeRef(ref)
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
		if record, ok := refRecords[nodeRefIdentityKey(ref)]; ok {
			out = append(out, cloneNodeRecord(record))
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
		identity := nodeRefIdentityKey(record.Ref)
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
		ref := normalizeNodeRef(record.Ref)
		available := nodeRecordAliasMask(ref)
		for mask := 1; mask <= 7; mask++ {
			if mask&available == mask {
				add(nodeRecordAliasIndexKey(ref, mask), record)
			}
		}
		for _, alias := range record.resolvedFrom {
			alias = normalizeNodeRef(alias)
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
		requested = normalizeNodeRef(requested)
		mask := nodeRecordAliasMask(requested)
		if positionalSourceRefWithStructural(requested) {
			mask = 4
		}
		if mask == 0 {
			continue
		}
		candidate := index[nodeRecordAliasIndexKey(requested, mask)]
		if candidate.identity != "" && !candidate.ambiguous && nodeRecordMatchesRequestedRef(candidate.record, requested) {
			out[nodeRefIdentityKey(requested)] = candidate.record
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
	actual := normalizeNodeRef(record.Ref)
	requested = normalizeNodeRef(requested)
	for _, alias := range record.resolvedFrom {
		if nodeRefIdentityKey(normalizeNodeRef(alias)) == nodeRefIdentityKey(requested) {
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
	return NodeCacheKey{Ref: normalizeNodeRef(ref), Profile: normalizedHydrateProfile(profile)}
}

func normalizedHydrateProfile(profile HydrateProfile) HydrateProfile {
	if profile == HydrateContent || profile == HydrateWorkspace {
		return HydrateContent
	}
	return HydrateSummary
}

// Traverse batches raw indexed ontology edge reads and groups rows by source.
//
// It accepts note-root sources only; use Neighborhood for embedded endpoint
// identity and target hydration.
func (s *Scope) Traverse(ctx context.Context, req TraverseRequest) (TraverseResult, error) {
	if s == nil || s.service == nil || s.service.Store == nil {
		return TraverseResult{EdgesBySource: map[string][]semdb.OntologyEdgeRow{}}, nil
	}
	// Docs: [[noderef-batch-traversal-read-api#^spec-0022-us4-ac1]] requires
	// traversal output to stay grouped by source ref for batch callers.
	s.mu.Lock()
	defer s.mu.Unlock()

	sources := make([]string, 0, len(req.Sources))
	for _, ref := range req.Sources {
		if ref.Kind != "" && ref.Kind != ontology.NodeKindNote {
			s.diagnostics.UnsupportedRefs++
			continue
		}
		if path := strings.TrimSpace(ref.NotePath); path != "" {
			sources = append(sources, path)
		}
	}
	sources = normalizeStrings(sources)
	if len(sources) == 0 {
		return TraverseResult{EdgesBySource: map[string][]semdb.OntologyEdgeRow{}}, nil
	}
	key := traverseCacheKey{
		structural:     req.Structural,
		relation:       strings.TrimSpace(req.Relation),
		provenance:     provenanceCacheKey(req.Provenance),
		dstType:        strings.TrimSpace(req.DstType),
		limitPerSource: req.LimitPerSource,
	}
	if cached, ok := s.traverseByKey[key]; ok && hasEdgeGroupsForSources(cached.EdgesBySource, sources) {
		s.diagnostics.EdgeHits++
		return cloneTraverseResultForSources(cached, sources), nil
	}
	baseKey := key
	baseKey.limitPerSource = 0
	if req.LimitPerSource > 0 {
		if cached, ok := s.traverseByKey[baseKey]; ok && hasEdgeGroupsForSources(cached.EdgesBySource, sources) {
			s.diagnostics.EdgeHits++
			result := cloneTraverseResultForSources(cached, sources)
			limitTraverseResult(result, req.LimitPerSource)
			return result, nil
		}
	}
	var rows []semdb.OntologyEdgeRow
	var err error
	if req.Structural {
		rows, err = s.service.Store.OntologyStructuralEdgesBySources(ctx, sources, req.Relation, 0)
	} else {
		rows, err = s.service.Store.OntologyAmbientEdgesBySources(ctx, sources, req.Relation, 0)
	}
	if err != nil {
		return TraverseResult{}, err
	}
	s.diagnostics.EdgeLoads++
	grouped := make(map[string][]semdb.OntologyEdgeRow, len(sources))
	for _, row := range rows {
		if len(req.Provenance) > 0 {
			if _, ok := req.Provenance[row.Provenance]; !ok {
				continue
			}
		}
		if req.DstType != "" && row.DstType != req.DstType {
			continue
		}
		grouped[row.SrcPath] = append(grouped[row.SrcPath], row)
	}
	for _, source := range sources {
		if grouped[source] == nil {
			grouped[source] = nil
		}
	}
	cached := s.traverseByKey[baseKey]
	if cached.EdgesBySource == nil {
		cached.EdgesBySource = map[string][]semdb.OntologyEdgeRow{}
	}
	for source, rows := range grouped {
		cached.EdgesBySource[source] = cloneEdgeRows(rows)
	}
	s.traverseByKey[baseKey] = cached
	result := cloneTraverseResultForSources(cached, sources)
	if req.LimitPerSource > 0 {
		limitTraverseResult(result, req.LimitPerSource)
		s.traverseByKey[key] = cloneTraverseResultForSources(result, sources)
	}
	return cloneTraverseResultForSources(result, sources), nil
}

// TypesByPaths returns cached ontology type rows for note paths.
func (s *Scope) TypesByPaths(ctx context.Context, pathsList []string) (map[string]semdb.OntologyNoteTypeRow, error) {
	if s == nil || s.service == nil || s.service.Store == nil {
		return map[string]semdb.OntologyNoteTypeRow{}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.typesByPathsLocked(ctx, pathsList)
}

func (s *Scope) typesByPathsLocked(ctx context.Context, pathsList []string) (map[string]semdb.OntologyNoteTypeRow, error) {
	pathsList = normalizeStrings(pathsList)
	missing := make([]string, 0, len(pathsList))
	out := make(map[string]semdb.OntologyNoteTypeRow, len(pathsList))
	for _, path := range pathsList {
		if s.typeRowsKnown[path] {
			s.diagnostics.TypeHits++
			if row, ok := s.typeRowsByPath[path]; ok {
				out[path] = row
			}
			continue
		}
		missing = append(missing, path)
	}
	if len(missing) > 0 {
		rows, err := s.service.Store.OntologyTypesByPaths(ctx, missing)
		if canceled := readCancellation(ctx, err); canceled != nil {
			return nil, canceled
		}
		if err != nil {
			return nil, err
		}
		s.diagnostics.TypeLoads++
		for _, path := range missing {
			s.typeRowsKnown[path] = true
			if row, ok := rows[path]; ok {
				s.typeRowsByPath[path] = row
				out[path] = row
			}
		}
	}
	return out, nil
}

// AssessmentsByPaths returns cached ontology validation assessments for note paths.
func (s *Scope) AssessmentsByPaths(ctx context.Context, pathsList []string) (map[string]*ontology.NoteAssessment, error) {
	if s == nil || s.service == nil || s.service.Store == nil {
		return map[string]*ontology.NoteAssessment{}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.assessmentsByPathsLocked(ctx, pathsList)
}

func (s *Scope) assessmentsByPathsLocked(ctx context.Context, pathsList []string) (map[string]*ontology.NoteAssessment, error) {
	pathsList = normalizeStrings(pathsList)
	missing := make([]string, 0, len(pathsList))
	out := make(map[string]*ontology.NoteAssessment, len(pathsList))
	for _, path := range pathsList {
		if s.assessmentKnown[path] {
			s.diagnostics.AssessmentHits++
			if assessment := s.assessmentByPath[path]; assessment != nil {
				out[path] = assessment
			}
			continue
		}
		missing = append(missing, path)
	}
	if len(missing) > 0 {
		rows, err := s.service.Store.OntologyAssessmentsByPaths(ctx, missing)
		if canceled := readCancellation(ctx, err); canceled != nil {
			return nil, canceled
		}
		if err != nil {
			return nil, err
		}
		s.diagnostics.AssessmentLoads++
		for _, path := range missing {
			s.assessmentKnown[path] = true
			row, ok := rows[path]
			if !ok {
				continue
			}
			assessment, err := ontology.AssessmentFromJSON(row.AssessmentJSON)
			if err != nil {
				return nil, err
			}
			if assessment != nil {
				s.assessmentByPath[path] = assessment
				out[path] = assessment
			}
		}
	}
	return out, nil
}

func (s *Scope) ensureNoteState(ctx context.Context) error {
	if s.noteStateLoaded {
		return nil
	}
	rows, err := s.service.Store.CurrentNoteMetadataRows(ctx)
	if err != nil {
		return err
	}
	s.noteRows = rows
	s.allNotePaths = make([]string, 0, len(rows))
	for _, row := range rows {
		s.allNotePaths = append(s.allNotePaths, row.Path)
		s.noteRowByPath[row.Path] = row
		s.noteRowKnown[row.Path] = true
	}
	_, err = s.typesByPathsLocked(ctx, s.allNotePaths)
	if err != nil {
		return err
	}
	flags, err := s.inventoryFlagsLocked(ctx)
	if err != nil {
		return err
	}
	for _, path := range s.allNotePaths {
		row, ok := flags[path]
		if !ok {
			continue
		}
		s.flagsByPath[path] = row
		if row.HasIssues {
			s.issueByPath[path] = true
		}
	}
	s.noteStateLoaded = true
	return nil
}

// inventoryFlagsLocked returns has-issues/type-ambiguity for every assessment
// row. The materialized columns are trusted only when the persisted
// materialization version is the one this binary writes; a migrated index
// that has not yet been rebuilt carries the migration's default flags, so
// during that window the flags are derived from assessment_json instead.
func (s *Scope) inventoryFlagsLocked(ctx context.Context) (map[string]semdb.OntologyAssessmentFlags, error) {
	state, err := s.service.Store.GetOntologySchemaState(ctx)
	if err != nil {
		return nil, err
	}
	if state.Ready && state.MaterializationVersion == ontology.OntologyMaterializationVersion {
		flags, err := s.service.Store.OntologyAssessmentFlags(ctx)
		if err != nil {
			return nil, err
		}
		s.diagnostics.AssessmentFlagLoads++
		return flags, nil
	}
	assessments, err := s.assessmentsByPathsLocked(ctx, s.allNotePaths)
	if err != nil {
		return nil, err
	}
	flags := make(map[string]semdb.OntologyAssessmentFlags, len(assessments))
	for path, assessment := range assessments {
		flags[path] = ontology.FlagsForAssessment(assessment)
	}
	return flags, nil
}

// AssessmentFlags returns the materialized inventory flags for every current
// note. The returned map is a copy; callers may not mutate scope state.
func (s *Scope) AssessmentFlags(ctx context.Context) (map[string]semdb.OntologyAssessmentFlags, error) {
	if s == nil || s.service == nil || s.service.Store == nil {
		return map[string]semdb.OntologyAssessmentFlags{}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureNoteState(ctx); err != nil {
		return nil, err
	}
	out := make(map[string]semdb.OntologyAssessmentFlags, len(s.flagsByPath))
	for path, flags := range s.flagsByPath {
		out[path] = flags
	}
	return out, nil
}
