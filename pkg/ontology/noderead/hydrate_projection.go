package noderead

import (
	"context"
	"errors"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"strings"
	"time"
)

func (s *Scope) embeddedRecordsFromProjection(ctx context.Context, refs []ontology.NodeRef) ([]NodeRecord, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	if s.hasOverlay() {
		overlay, missing, err := s.overlayRecordsByRefsLocked(ctx, refs)
		if err != nil {
			return nil, err
		}
		if len(missing) == 0 {
			if len(overlay) > 0 {
				s.diagnostics.RecordHits += len(overlay)
			}
			return overlay, nil
		}
		if len(overlay) > 0 {
			tail, err := s.embeddedRecordsFromProjectionNoOverlay(ctx, missing)
			if err != nil {
				return nil, err
			}
			return append(overlay, tail...), nil
		}
		refs = missing
	}
	return s.embeddedRecordsFromProjectionNoOverlay(ctx, refs)
}

func (s *Scope) embeddedRecordsFromProjectionNoOverlay(ctx context.Context, refs []ontology.NodeRef) ([]NodeRecord, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	// Group refs by host note so each note's DocumentSnapshot is loaded and parsed
	// at most once per Hydrate call. Without this, hydrating N embedded refs from
	// the same note re-reads + re-parses the file N times.
	type pendingRef struct {
		original ontology.NodeRef
		index    int
	}
	pendingByPath := make(map[string][]pendingRef, len(refs))
	projections := make([]*ontology.NodeProjection, len(refs))
	hits := 0
	for i, ref := range refs {
		ref = NormalizeRef(ref)
		if ref.IsZero() {
			continue
		}
		key := RefIdentityKey(ref)
		if s.projectionKnown[key] {
			projections[i] = s.projectionByRef[key]
			hits++
			continue
		}
		pendingByPath[ref.NotePath] = append(pendingByPath[ref.NotePath], pendingRef{original: ref, index: i})
	}
	pendingPaths := make([]string, 0, len(pendingByPath))
	for path := range pendingByPath {
		pendingPaths = append(pendingPaths, path)
	}
	if len(pendingPaths) > 0 && s.service.Store != nil {
		// Batch the normal path, but retain per-host snapshot error tolerance below.
		// A failed batch is retried per host and marks only that host unsupported.
		if canceled := readCancellation(ctx, s.ensurePathStateLocked(ctx, pendingPaths)); canceled != nil {
			return nil, canceled
		}
	}
	for path, pending := range pendingByPath {
		prepared, err := s.preparedForPathLocked(ctx, path)
		if canceled := readCancellation(ctx, err); canceled != nil {
			return nil, canceled
		}
		if err != nil {
			// Per-ref error tolerance preserves the legacy behavior of skipping
			// individual unprojectable refs without failing the whole hydrate.
			for _, p := range pending {
				s.projectionKnown[RefIdentityKey(p.original)] = true
			}
			s.diagnostics.UnsupportedRefs += len(pending)
			continue
		}
		if prepared == nil {
			for _, p := range pending {
				s.projectionKnown[RefIdentityKey(p.original)] = true
			}
			s.diagnostics.UnsupportedRefs += len(pending)
			continue
		}
		for _, p := range pending {
			projection, err := prepared.Project(p.original)
			if canceled := readCancellation(ctx, err); canceled != nil {
				return nil, canceled
			}
			if errors.Is(err, ontology.ErrAmbiguousSourceSpanIdentity) {
				return nil, err
			}
			if err != nil || projection == nil {
				s.projectionKnown[RefIdentityKey(p.original)] = true
				s.diagnostics.UnsupportedRefs++
				continue
			}
			projection = projectFallback(projection)
			projections[p.index] = projection
			s.projectionKnown[RefIdentityKey(p.original)] = true
			s.projectionByRef[RefIdentityKey(p.original)] = projection
			canonicalKey := RefIdentityKey(projection.Ref)
			s.projectionKnown[canonicalKey] = true
			s.projectionByRef[canonicalKey] = projection
		}
	}
	out := make([]NodeRecord, 0, len(refs))
	canonicalRefs := make([]ontology.NodeRef, 0, len(refs))
	for _, projection := range projections {
		if projection == nil {
			continue
		}
		canonicalRefs = append(canonicalRefs, projection.Ref)
	}
	locators, err := s.locatorsLocked(ctx, canonicalRefs)
	if err != nil {
		return nil, err
	}
	for i, projection := range projections {
		if projection == nil {
			continue
		}
		record := s.nodeRecordFromProjectionLocked(ctx, projection)
		record.resolvedFrom = append(record.resolvedFrom, NormalizeRef(refs[i]))
		if nodeLocator := locators[RefIdentityKey(projection.Ref)]; nodeLocator != nil {
			record.NodeLocator = nodeLocator
			record.LinkTarget = nodeLocator.LinkTarget
		}
		out = append(out, record)
	}
	s.diagnostics.RecordHits += hits
	if len(out) > 0 {
		s.diagnostics.RecordLoads++
	}
	return out, nil
}

func (s *Scope) nodeRecordFromProjectionLocked(ctx context.Context, projection *ontology.NodeProjection) NodeRecord {
	_ = ctx
	title := ontology.NodeTitle(projection)
	content := ""
	if projection.Snapshot != nil && projection.Ref.StartByte >= 0 && projection.Ref.EndByte > projection.Ref.StartByte && projection.Ref.EndByte <= len(projection.Snapshot.Content) {
		content = strings.TrimSpace(projection.Snapshot.Content[projection.Ref.StartByte:projection.Ref.EndByte])
	}
	// FieldValues is intentionally left nil on projection-backed records:
	// catalog field rows are produced by ontology sync, while overlay/projection
	// records are derived from preview markdown. Frontmatter dates use the
	// catalog's text spelling so staged and committed records agree.
	record := NodeRecord{
		Ref:             projection.Ref,
		Path:            projection.Ref.NotePath,
		Title:           title,
		Content:         content,
		Frontmatter:     projectionFrontmatter(projection),
		InlineProps:     projectionInlineProps(projection),
		Tags:            projectionTags(projection),
		FieldValues:     nil,
		projectedFields: captureProjectedFieldValues(projection),
		TypeName:        projection.ResolvedType,
		UpdatedAt:       s.noteRowByPath[projection.Ref.NotePath].Mtime,
		HasIssues:       s.issueByPath[projection.Ref.NotePath],
	}
	if projection.Record != nil {
		native := projection.SourceContent()
		record.Content = native.Text
		record.SourceContent = &native
	}
	return s.withProjectionSource(record, projection)
}

func captureProjectedFieldValues(projection *ontology.NodeProjection) map[string]projectedFieldValues {
	values := make(map[string]projectedFieldValues, len(projection.Fields))
	for name, binding := range projection.Fields {
		values[name] = projectedFieldValues{values: append([]string(nil), binding.Values...), present: binding.Present}
	}
	return values
}

// frontmatterWithTemporalText copies snapshot frontmatter with YAML dates and
// timestamps as metadata text, the spelling catalog-backed records carry.
// ponytail: classifies by value (midnight is a date), so an authored midnight
// timestamp reads as a date; carry YAML spelling in the snapshot if that bites.
func frontmatterWithTemporalText(in map[string]any, noteType *ontoschema.NoteType) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		var field *ontoschema.Field
		if noteType != nil {
			for _, candidate := range noteType.Fields {
				for _, name := range ontoschema.FieldSourceNames(candidate) {
					if strings.EqualFold(name, key) {
						field = candidate
						break
					}
				}
			}
		}
		out[key] = temporalText(value, field)
	}
	return out
}

func temporalText(value any, field *ontoschema.Field) any {
	switch typed := value.(type) {
	case time.Time:
		if field != nil {
			values := ontology.FieldTextValues(field, typed)
			if len(values) > 0 {
				return values[0]
			}
		}
		if metadata, err := noteformat.NewMetadataValue(typed); err == nil {
			return metadata.Export()
		}
	case []any:
		out := make([]any, len(typed))
		for index, item := range typed {
			out[index] = temporalText(item, field)
		}
		return out
	case map[string]any:
		return frontmatterWithTemporalText(typed, nil)
	}
	return value
}

func projectionInlineProps(projection *ontology.NodeProjection) map[string][]string {
	if projection == nil || len(projection.Fields) == 0 {
		return nil
	}
	out := map[string][]string{}
	for name, binding := range projection.Fields {
		if strings.TrimSpace(name) == "" || len(binding.Values) == 0 {
			continue
		}
		if field := projection.Type.ByName[name]; ontoschema.IsSectionSummary(field) {
			out[name] = ontology.SectionSummaryValues(projection, field)
		} else {
			out[name] = append([]string(nil), binding.Values...)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func projectionFrontmatter(p *ontology.NodeProjection) map[string]any {
	if p.Record != nil {
		return nil
	}
	if p.Snapshot != nil {
		return frontmatterWithTemporalText(p.Snapshot.Frontmatter, p.Type)
	}
	if p.RootSnapshot != nil {
		return frontmatterWithTemporalText(p.RootSnapshot.Metadata, p.Type)
	}
	return nil
}
func projectionTags(p *ontology.NodeProjection) []string {
	if p.Snapshot != nil {
		return snapshotTags(p.Snapshot)
	}
	if p.RootSnapshot != nil {
		return append([]string(nil), p.RootSnapshot.Tags...)
	}
	return nil
}
