package web

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/paths"
)

// diffOntologyEditSessionResponse returns a per-note, per-op diff view of an
// edit session baselined against the session's original captured state. It
// reuses the session's live editor (or, for a restored snapshot, seeds base
// fingerprints from the snapshot) so PreviewCurrent compares against the
// baseline when the ops were staged — not against whatever happens to be on
// disk right now. That keeps staleness and rebased signals honest if the
// underlying file has drifted since staging.
// Docs: [[ontology-edit-replay-conflict-contract]].
func (s *Server) diffOntologyEditSessionResponse(ctx context.Context, sessionID string, snapshot *OntologyEditSessionSnapshot) (ModifiedNotesResponse, error) {
	_, defs, err := s.ontologyContext()
	if err != nil {
		return ModifiedNotesResponse{}, err
	}
	if defs == nil || defs.schema == nil {
		return ModifiedNotesResponse{}, fmt.Errorf("ontology schema is unavailable")
	}
	session, err := s.ensureOntologyEditSession(ctx, defs, sessionID, snapshot)
	if err != nil {
		return ModifiedNotesResponse{}, err
	}

	resp := ModifiedNotesResponse{
		SessionID: sessionID,
		UpdatedAt: time.Now(),
		Notes:     []ModifiedNoteEntry{},
	}

	if err := session.lockLive(); err != nil {
		return ModifiedNotesResponse{}, err
	}
	defer session.mu.Unlock()
	ops := cloneOntologyEditOps(session.Ops)
	if len(ops) == 0 {
		return resp, nil
	}
	plan, conflicts, err := s.previewSessionEditor(ctx, session, defs)
	if err != nil {
		return ModifiedNotesResponse{}, err
	}

	fileByPath := make(map[string]ontology.FileCommitPlan, len(plan.Files))
	for _, file := range plan.Files {
		fileByPath[file.NotePath] = file
	}

	opsByPath := make(map[string][]OntologyEditOp, len(ops))
	pathOrder := make([]string, 0)
	for _, op := range ops {
		notePath := modifiedOpNotePath(op)
		if notePath == "" {
			continue
		}
		if _, ok := opsByPath[notePath]; !ok {
			pathOrder = append(pathOrder, notePath)
		}
		opsByPath[notePath] = append(opsByPath[notePath], op)
	}
	sort.Strings(pathOrder)

	titlesByPath, typesByPath := s.lookupNoteMetadata(ctx, pathOrder)
	nodeScope := s.nodeReadScope(ctx, defs)
	nodeTitles := modifiedOpNodeTitles(ctx, nodeScope, ops)

	totals := ModifiedNotesTotals{}
	entries := make([]ModifiedNoteEntry, 0, len(pathOrder))
	for _, notePath := range pathOrder {
		notePathOps := opsByPath[notePath]
		totals.Ops += len(notePathOps)

		projectionCache := map[string]*ontology.NodeProjection{}
		opViews := make([]ModifiedNoteOpView, 0, len(notePathOps))
		for _, op := range notePathOps {
			view := newModifiedNoteOpView(op)
			view.NodeTitle = nodeTitles[noderead.RefIdentityKey(view.NodeRef)]
			switch view.Kind {
			case "setField":
				totals.SetField++
				if binding := lookupFieldBinding(ctx, s, defs, nodeScope, op, projectionCache); binding != nil && len(binding.Values) > 0 {
					view.PreviousValue = binding.Values[0]
				}
			case "setLinkField":
				totals.SetLinkField++
				if binding := lookupFieldBinding(ctx, s, defs, nodeScope, op, projectionCache); binding != nil {
					view.PreviousValues = append([]string(nil), binding.Values...)
				}
			case "setNarrative", "insertNarrative":
				totals.SetNarrative++
			case "addEmbeddedNode":
				totals.AddEmbedded++
			case "deleteNode":
				totals.Delete++
			case "reorderCollection":
				totals.Reorder++
				if collection := lookupCollectionBinding(ctx, s, defs, nodeScope, op, projectionCache); collection != nil {
					prev := make([]string, 0, len(collection.Items))
					for _, item := range collection.Items {
						prev = append(prev, item.Ref.Fragment)
					}
					view.PreviousFragments = prev
				}
			}
			opViews = append(opViews, view)
		}

		filePlan := fileByPath[notePath]
		entries = append(entries, ModifiedNoteEntry{
			Path:               notePath,
			Title:              titlesByPath[notePath],
			ResolvedType:       typesByPath[notePath],
			Diff:               filePlan.Diff,
			BaseFingerprint:    filePlan.BaseFingerprint,
			CurrentFingerprint: filePlan.CurrentFingerprint,
			UpdatedFingerprint: filePlan.UpdatedFingerprint,
			Rebased:            filePlan.Rebased,
			HasMaterialChange:  filePlan.HasMaterialChange,
			Ops:                opViews,
		})
	}

	totals.Notes = len(entries)
	resp.Totals = totals
	resp.Notes = entries
	resp.Conflicts = attachOntologyConflictOperationIDs(conflicts, ops)
	resp.Rebased = planHasRebased(plan)
	resp.StalePaths = stalePathsFromPlan(plan)
	return resp, nil
}

// lookupNoteMetadata resolves titles and resolved ontology types for the given
// note paths in one round trip per table, so the diff loop avoids N+1 Intel
// queries.
func (s *Server) lookupNoteMetadata(ctx context.Context, notePaths []string) (map[string]string, map[string]string) {
	titles := map[string]string{}
	types := map[string]string{}
	if len(notePaths) == 0 {
		return titles, types
	}
	intel := s.runtime.Intel()
	if intel == nil {
		return titles, types
	}
	if rows, err := intel.CurrentNoteMetadataRowsByPaths(ctx, notePaths); err == nil {
		for path, row := range rows {
			titles[path] = row.Title
		}
	}
	if rows, err := intel.OntologyTypesByPaths(ctx, notePaths); err == nil {
		for path, row := range rows {
			types[path] = row.TypeName
		}
	}
	return titles, types
}

// modifiedOpNodeTitles names the committed nodes that ops inside a note
// target, keyed by ref identity. One batched hydration warms the scope; the
// per-ref reads after it are cache hits that keep each title tied to its ref.
func modifiedOpNodeTitles(ctx context.Context, scope *noderead.Scope, ops []OntologyEditOp) map[string]string {
	titles := map[string]string{}
	if scope == nil {
		return titles
	}
	refs := make([]ontology.NodeRef, 0, len(ops))
	for _, op := range ops {
		if ref, err := ontologyNodeRefFromOp(op); err == nil && ref.Kind != ontology.NodeKindNote {
			refs = append(refs, ref)
		}
	}
	summary := noderead.HydrateOptions{Profile: noderead.HydrateSummary}
	if _, err := scope.Hydrate(ctx, refs, summary); err != nil {
		return titles
	}
	for _, ref := range refs {
		if records, err := scope.Hydrate(ctx, []ontology.NodeRef{ref}, summary); err == nil && len(records) == 1 {
			titles[noderead.RefIdentityKey(ref)] = strings.TrimSpace(records[0].Title)
		}
	}
	return titles
}

func newModifiedNoteOpView(op OntologyEditOp) ModifiedNoteOpView {
	ref, _ := ontologyNodeRefFromOp(op)
	return ModifiedNoteOpView{
		ID:               strings.TrimSpace(op.ID),
		Kind:             strings.TrimSpace(op.Kind),
		NodeRef:          ref,
		Field:            strings.TrimSpace(op.Field),
		Value:            op.Value,
		Values:           append([]string(nil), op.Values...),
		Markdown:         op.Markdown,
		PreviousMarkdown: op.PreviousMarkdown,
		RangeStart:       op.RangeStart,
		RangeEnd:         op.RangeEnd,
		Heading:          op.Heading,
		Body:             op.Body,
		BlockID:          strings.TrimSpace(op.BlockID),
		Collection:       strings.TrimSpace(op.Collection),
		OrderedFragments: append([]string(nil), op.OrderedFragments...),
		OldTarget:        op.OldTarget,
		NewTarget:        op.NewTarget,
		Property:         strings.TrimSpace(op.Property),
		Level:            strings.TrimSpace(op.Level),
	}
}

func modifiedOpNotePath(op OntologyEditOp) string {
	raw := strings.TrimSpace(op.Path)
	if raw == "" {
		return ""
	}
	notePath, _, _ := splitLinkFragment(raw)
	if notePath == "" {
		notePath = raw
	}
	return string(paths.NormalizeNotePath(notePath))
}

// lookupFieldBinding projects the op's node from the current on-disk content
// and returns the current field binding, caching per-ref projections to avoid
// repeated disk reads when one note has many ops.
func lookupFieldBinding(ctx context.Context, s *Server, defs *ontologyDefinitions, scope *noderead.Scope, op OntologyEditOp, cache map[string]*ontology.NodeProjection) *ontology.FieldBinding {
	field := strings.TrimSpace(op.Field)
	if field == "" {
		return nil
	}
	projection := projectForOp(ctx, s, defs, scope, op, cache)
	if projection == nil {
		return nil
	}
	binding, ok := projection.Fields[field]
	if !ok {
		return nil
	}
	return &binding
}

func lookupCollectionBinding(ctx context.Context, s *Server, defs *ontologyDefinitions, scope *noderead.Scope, op OntologyEditOp, cache map[string]*ontology.NodeProjection) *ontology.CollectionBinding {
	collection := strings.TrimSpace(op.Collection)
	if collection == "" {
		return nil
	}
	projection := projectForOp(ctx, s, defs, scope, op, cache)
	if projection == nil {
		return nil
	}
	binding, ok := projection.Collections[collection]
	if !ok {
		return nil
	}
	return &binding
}

func projectForOp(ctx context.Context, s *Server, defs *ontologyDefinitions, scope *noderead.Scope, op OntologyEditOp, cache map[string]*ontology.NodeProjection) *ontology.NodeProjection {
	ref, err := ontologyNodeRefFromOp(op)
	if err != nil {
		return nil
	}
	key := canonicalNodeRefKey(ref)
	if projection, ok := cache[key]; ok {
		return projection
	}
	projection, err := s.resolveNodeProjection(ctx, defs, scope, ref)
	if err != nil {
		cache[key] = nil
		return nil
	}
	cache[key] = projection
	return projection
}
