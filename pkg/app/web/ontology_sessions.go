package web

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/validate"
)

type ontologyEditSession struct {
	mu         sync.Mutex
	deleted    atomic.Bool
	ID         string
	Revision   uint64
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Ops        []OntologyEditOp
	Editor     *ontology.EditSession
	Rebased    bool
	StalePaths []string
	Overlay    *ontologyEditSessionReadOverlayCache
	Receipts   map[string]ontologyEditRequestReceipt
}

type ontologyEditSessionReadOverlayCache struct {
	Key     ontologyEditSessionReadOverlayCacheKey
	Overlay *ontologyquery.ReadOverlay
	OpsLen  int
}

type ontologyEditRequestReceipt struct {
	RequestHash string
	Response    OntologyEditSessionResponse
}

type ontologyEditSessionReadOverlayCacheKey struct {
	OpsHash              string
	SchemaHash           string
	BaseFingerprintsHash string
	Status               OntologyEditSessionStatus
	Rebased              bool
	Conflicted           bool
}

func (s *Server) createOntologyEditSession(now time.Time, preferredID string, ops []OntologyEditOp, editor *ontology.EditSession) (*ontologyEditSession, error) {
	id := strings.TrimSpace(preferredID)
	if id == "" {
		id = newOntologyEditSessionID()
	}
	session := &ontologyEditSession{
		ID:        id,
		Revision:  1,
		CreatedAt: now,
		UpdatedAt: now,
		Ops:       cloneOntologyEditOps(ops),
		Editor:    editor,
		Receipts:  make(map[string]ontologyEditRequestReceipt),
	}
	s.editSessionsMu.Lock()
	defer s.editSessionsMu.Unlock()
	if _, exists := s.editSessions[session.ID]; exists {
		return nil, fmt.Errorf("edit session %q already exists", session.ID)
	}
	if coordinator := s.runtime.RepairPathCoordinator(); coordinator != nil {
		if err := coordinator.SetManualPaths(s.validationVaultIdentity(), session.ID, touchedPathsFromOps(ops)); err != nil {
			return nil, err
		}
	}
	s.editSessions[session.ID] = session
	return session, nil
}

func (s *Server) ontologyEditSession(id string) (*ontologyEditSession, bool) {
	s.editSessionsMu.RLock()
	defer s.editSessionsMu.RUnlock()
	session, ok := s.editSessions[id]
	return session, ok
}

func (s *Server) deleteOntologyEditSession(id string) bool {
	s.editSessionsMu.Lock()
	session, ok := s.editSessions[id]
	if !ok {
		s.editSessionsMu.Unlock()
		return false
	}
	// Mark the session before waiting for an operation that already holds its
	// pointer. That operation will fail its live-session check rather than
	// recreating reservations after this deletion completes.
	session.deleted.Store(true)
	delete(s.editSessions, id)
	session.mu.Lock()
	if coordinator := s.runtime.RepairPathCoordinator(); coordinator != nil {
		coordinator.ReleaseManualSession(s.validationVaultIdentity(), id)
	}
	session.mu.Unlock()
	s.editSessionsMu.Unlock()
	return true
}

func (session *ontologyEditSession) lockLive() error {
	session.mu.Lock()
	if session.deleted.Load() {
		session.mu.Unlock()
		return fmt.Errorf("edit session %q not found", session.ID)
	}
	return nil
}

func (s *Server) ensureOntologyEditSession(ctx context.Context, defs *ontologyDefinitions, sessionID string, snapshot *OntologyEditSessionSnapshot) (*ontologyEditSession, error) {
	if session, ok := s.ontologyEditSession(sessionID); ok {
		return session, nil
	}
	if snapshot == nil {
		return nil, fmt.Errorf("edit session %q not found", sessionID)
	}
	if snapshot.Version != 3 {
		return nil, fmt.Errorf("unsupported edit session snapshot version %d", snapshot.Version)
	}
	if err := validateOntologyEditSnapshotBases(snapshot); err != nil {
		return nil, err
	}
	restoredID := strings.TrimSpace(snapshot.SessionID)
	if restoredID == "" {
		restoredID = sessionID
	}
	if restoredID != sessionID {
		return nil, fmt.Errorf("session snapshot id %q does not match requested session %q", restoredID, sessionID)
	}
	// Persisted operations were already canonical when they were accepted.
	// Install their original source evidence before preview resolves targets;
	// resolving or pruning against current disk would erase recovery conflicts.
	canonicalOps := cloneOntologyEditOps(snapshot.Ops)
	editor, err := s.replayOntologyEditOpsWithBases(defs, canonicalOps, snapshot.BaseDocuments)
	if err != nil {
		return nil, err
	}
	restored, err := s.createOntologyEditSession(time.Now(), restoredID, canonicalOps, editor)
	if err != nil {
		return nil, err
	}
	restored.mu.Lock()
	if snapshot.Revision > restored.Revision {
		restored.Revision = snapshot.Revision
	}
	restored.mu.Unlock()
	return restored, nil
}

func newOntologyEditSessionID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("sess-%d", time.Now().UnixNano())
	}
	return "sess-" + hex.EncodeToString(buf)
}

func cloneOntologyEditOps(in []OntologyEditOp) []OntologyEditOp {
	if len(in) == 0 {
		return nil
	}
	out := make([]OntologyEditOp, 0, len(in))
	for _, op := range in {
		cloned := op
		cloned.Values = append([]string(nil), op.Values...)
		cloned.OrderedFragments = append([]string(nil), op.OrderedFragments...)
		if op.FieldValue != nil {
			fieldValue := *op.FieldValue
			fieldValue.Items = append([]string(nil), op.FieldValue.Items...)
			cloned.FieldValue = &fieldValue
		}
		if op.Expected != nil {
			expected := *op.Expected
			if op.Expected.Field != nil {
				fieldValue := *op.Expected.Field
				fieldValue.Items = append([]string(nil), op.Expected.Field.Items...)
				expected.Field = &fieldValue
			}
			cloned.Expected = &expected
		}
		out = append(out, cloned)
	}
	return out
}

func validateOntologyEditSnapshotBases(snapshot *OntologyEditSessionSnapshot) error {
	if snapshot == nil {
		return nil
	}
	wanted := make(map[string]struct{})
	for _, op := range snapshot.Ops {
		raw := strings.TrimSpace(strings.SplitN(op.Path, "#", 2)[0])
		clean, err := paths.CleanNotePath(raw)
		if err != nil {
			return fmt.Errorf("invalid snapshot operation path %q: %w", raw, err)
		}
		wanted[clean.String()] = struct{}{}
	}
	seen := make(map[string]struct{}, len(snapshot.BaseDocuments))
	for _, base := range snapshot.BaseDocuments {
		clean, err := paths.CleanNotePath(base.NotePath)
		if err != nil {
			return fmt.Errorf("invalid snapshot base path %q: %w", base.NotePath, err)
		}
		if _, duplicate := seen[clean.String()]; duplicate {
			return fmt.Errorf("snapshot contains multiple bases for %s", clean.String())
		}
		verified, err := buildMarkdownDocumentSnapshotCompat(clean.String(), base.Content, time.Time{})
		if err != nil || verified.ContentFingerprint != base.Fingerprint {
			return fmt.Errorf("snapshot base fingerprint mismatch for %s", clean.String())
		}
		seen[clean.String()] = struct{}{}
	}
	for notePath := range wanted {
		if _, ok := seen[notePath]; !ok {
			return fmt.Errorf("snapshot is missing the verified base for %s", notePath)
		}
	}
	return nil
}

func (s *Server) canonicalizeOntologyEditOps(ctx context.Context, defs *ontologyDefinitions, ops []OntologyEditOp) ([]OntologyEditOp, error) {
	// Docs: [[ontology-edit-replay-conflict-contract]]
	// requires web sessions to persist canonical node identity in addition to
	// the author-facing locator that came from the UI.
	out := cloneOntologyEditOps(ops)
	nodeScope := s.nodeReadScope(ctx, defs)
	for i := range out {
		ref, err := ontologyNodeRefFromOp(out[i])
		if err != nil {
			return nil, err
		}
		cleanPath, err := paths.CleanNotePath(ref.NotePath)
		if err != nil {
			return nil, err
		}
		ref.NotePath = cleanPath.String()
		out[i].Path = ref.NotePath
		if ref.Fragment != "" {
			out[i].Path += "#" + strings.TrimPrefix(ref.Fragment, "#")
		}
		needsProjection := ref.Kind != ontology.NodeKindNote || strings.TrimSpace(ref.Fragment) != "" || strings.TrimSpace(out[i].Kind) == "setLinkField"
		if !needsProjection {
			continue
		}
		projection, err := s.resolveNodeProjection(ctx, defs, nodeScope, ref)
		if err != nil {
			return nil, err
		}
		out[i].NodeID = projection.Ref.NodeID
		out[i].Structural = projection.Ref.Structural
		if strings.TrimSpace(out[i].Kind) == "setLinkField" {
			if err := s.validateOntologyEditLinkTargets(ctx, defs, nodeScope, projection, out[i]); err != nil {
				return nil, err
			}
		}
	}
	for i := range out {
		ensureOntologyEditOpID(&out[i])
	}
	return out, nil
}

func (s *Server) validateOntologyEditLinkTargets(ctx context.Context, defs *ontologyDefinitions, scope *noderead.Scope, owner *ontology.NodeProjection, op OntologyEditOp) error {
	if owner == nil || owner.Type == nil {
		return fmt.Errorf("relation owner has no resolved ontology type")
	}
	field := owner.Type.ByName[strings.TrimSpace(op.Field)]
	if field == nil || field.Kind != ontology.FieldKindLink {
		return fmt.Errorf("field %s is not a relation on %s", op.Field, owner.Ref.String())
	}
	values, mode, err := ontologyEditFieldValues(op)
	if err != nil || mode == "unset" {
		return err
	}
	for _, value := range values {
		target := strings.TrimSpace(value)
		resolverInput := target
		if !strings.HasPrefix(resolverInput, "[[") || !strings.HasSuffix(resolverInput, "]]") {
			resolverInput = "[[" + resolverInput + "]]"
		}
		resolved, err := scope.Resolve(ctx, noderead.ResolveRequest{
			Targets:  []noderead.NodeTarget{{Input: resolverInput}},
			FromPath: owner.Ref.NotePath,
			Hydrate:  noderead.HydrateOptions{Profile: noderead.HydrateSummary},
		})
		if err != nil {
			return fmt.Errorf("resolve relation target %q: %w", value, err)
		}
		if len(resolved.Resolved) != 1 {
			return fmt.Errorf("relation target %q did not resolve uniquely", value)
		}
		canonical := resolved.Resolved[0]
		projection, err := s.resolveNodeProjection(ctx, defs, scope, canonical.Ref)
		if err != nil {
			return fmt.Errorf("resolve relation target %q: %w", value, err)
		}
		targetType := projection.Type
		if targetType == nil {
			targetType = defs.schema.Types[canonical.Record.TypeName]
		}
		actualType := canonical.Record.TypeName
		if targetType != nil {
			actualType = targetType.Name
		}
		if !ontology.TypeMatchesOrImplements(defs.schema, actualType, field.TypeName) {
			resolvedType := canonical.Record.TypeName
			if targetType != nil {
				resolvedType = targetType.Name
			}
			return fmt.Errorf("relation field %s requires %s, but %q resolves to %s", field.Name, field.TypeName, value, resolvedType)
		}
	}
	return nil
}

func ensureOntologyEditOpID(op *OntologyEditOp) {
	if op == nil || strings.TrimSpace(op.ID) != "" {
		return
	}
	identity := ontologyEditOperationTarget(*op)
	switch strings.TrimSpace(op.Kind) {
	case "setField", "setLinkField", "setNarrative", "setSource":
	default:
		encoded, _ := json.Marshal(op)
		identity = string(encoded)
	}
	sum := sha256.Sum256([]byte(identity))
	op.ID = "op-" + hex.EncodeToString(sum[:8])
}

// canonicalizeOntologyEditOpsForSessionLocked requires session.mu. The caller
// keeps that lock through appending the canonical operations so preview lineage
// cannot be applied to a different session state.
func (s *Server) canonicalizeOntologyEditOpsForSessionLocked(ctx context.Context, defs *ontologyDefinitions, session *ontologyEditSession, ops []OntologyEditOp) ([]OntologyEditOp, error) {
	out := make([]OntologyEditOp, 0, len(ops))
	for _, op := range ops {
		canonical, err := s.canonicalizeOntologyEditOps(ctx, defs, []OntologyEditOp{op})
		if err == nil {
			out = append(out, canonical[0])
			continue
		}
		kind := strings.TrimSpace(op.Kind)
		if kind != "setField" && kind != "setLinkField" && kind != "setRootMetadataList" {
			return nil, err
		}
		projection, lineage, previewScope, previewErr := s.projectOntologyEditOpFromSessionLocked(ctx, defs, session, op)
		if previewErr != nil {
			return nil, err
		}
		if kind == "setLinkField" {
			if validationErr := s.validateOntologyEditLinkTargets(ctx, defs, previewScope, projection, op); validationErr != nil {
				return nil, validationErr
			}
		}
		baseRef, ok := matchingSessionBaseRef(lineage, projection.Ref)
		if !ok {
			return nil, err
		}
		canonicalOp := op
		canonicalOp.Path = baseRef.NotePath
		if baseRef.Fragment != "" {
			canonicalOp.Path += "#" + baseRef.Fragment
		}
		canonicalOp.NodeID = baseRef.NodeID
		canonicalOp.Structural = baseRef.Structural
		ensureOntologyEditOpID(&canonicalOp)
		out = append(out, canonicalOp)
	}
	return out, nil
}

func (s *Server) projectOntologyEditOpFromSessionLocked(ctx context.Context, defs *ontologyDefinitions, session *ontologyEditSession, op OntologyEditOp) (*ontology.NodeProjection, []ontology.PreviewRefLineage, *noderead.Scope, error) {
	if session == nil || defs == nil || defs.schema == nil {
		return nil, nil, nil, fmt.Errorf("edit session preview is unavailable")
	}
	ref, err := ontologyNodeRefFromOp(op)
	if err != nil {
		return nil, nil, nil, err
	}
	if session.Editor == nil {
		return nil, nil, nil, fmt.Errorf("edit session preview is unavailable")
	}
	plan, lineage, err := session.Editor.PreviewWithRefLineage(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	overlay := s.markdownReadOverlayFromCommitPlan(plan, OntologyEditSessionStatusDirty, false)
	if overlay == nil {
		return nil, nil, nil, fmt.Errorf("edit session preview does not include %s", ref.NotePath)
	}
	scope := s.nodeReadScopeWithOverlay(ctx, defs, overlay)
	projection, err := scope.Projection(ctx, ref)
	if err == nil && (projection == nil || projection.Ref.IsZero()) {
		err = fmt.Errorf("edit session preview did not resolve %s", ref.String())
	}
	return projection, lineage, scope, err
}

func matchingSessionBaseRef(lineage []ontology.PreviewRefLineage, current ontology.NodeRef) (ontology.NodeRef, bool) {
	var match ontology.NodeRef
	for _, entry := range lineage {
		candidate := entry.Original
		preview := entry.Preview
		if candidate.NotePath != current.NotePath || candidate.Kind != current.Kind ||
			preview.NotePath != current.NotePath || preview.Kind != current.Kind ||
			strings.TrimSpace(preview.Structural) == "" || preview.Structural != current.Structural {
			continue
		}
		if match.NotePath != "" && (match.NodeID != candidate.NodeID || match.Structural != candidate.Structural || match.Fragment != candidate.Fragment) {
			return ontology.NodeRef{}, false
		}
		match = candidate
	}
	return match, match.NotePath != ""
}

func (s *Server) reduceOntologyEditOps(ctx context.Context, defs *ontologyDefinitions, ops []OntologyEditOp) ([]OntologyEditOp, error) {
	if len(ops) == 0 {
		return nil, nil
	}
	nodeScope := s.nodeReadScope(ctx, defs)
	out := make([]OntologyEditOp, 0, len(ops))
	replaceable := map[string]int{}
	for _, op := range ops {
		op = cloneOntologyEditOps([]OntologyEditOp{op})[0]
		key, ok := replaceableOntologyEditOpKey(op)
		if !ok {
			out = append(out, op)
			continue
		}
		if index, exists := replaceable[key]; exists {
			previous := out[index]
			if strings.TrimSpace(op.ID) != "" && ontologyEditOperationTarget(previous) != ontologyEditOperationTarget(op) {
				return nil, fmt.Errorf("operation id %q was reused for a different edit target", op.ID)
			}
			if previous.PreviousMarkdown != "" {
				op.PreviousMarkdown = previous.PreviousMarkdown
			}
			if previous.Expected != nil {
				op.Expected = previous.Expected
			}
			out[index] = op
			continue
		}
		replaceable[key] = len(out)
		out = append(out, op)
	}
	pruned := out[:0]
	for _, op := range out {
		if ok, err := s.ontologyEditOpMatchesCurrentValue(ctx, defs, nodeScope, op); err != nil {
			return nil, err
		} else if ok {
			continue
		}
		pruned = append(pruned, op)
	}
	if len(pruned) == 0 {
		return nil, nil
	}
	return cloneOntologyEditOps(pruned), nil
}

func ontologyEditOperationTarget(op OntologyEditOp) string {
	ref, _ := ontologyNodeRefFromOp(op)
	parts := []string{
		strings.TrimSpace(op.Kind), canonicalNodeRefKey(ref), strings.TrimSpace(op.Field),
		strings.TrimSpace(op.Collection),
	}
	if strings.TrimSpace(op.Kind) != "setNarrative" {
		parts = append(parts, fmt.Sprintf("%d:%d", op.RangeStart, op.RangeEnd))
	}
	return strings.Join(parts, "|")
}

func attachOntologyConflictOperationIDs(conflicts []ontology.ConflictReport, ops []OntologyEditOp) []ontology.ConflictReport {
	attached := append([]ontology.ConflictReport(nil), conflicts...)
	for index := range attached {
		if attached[index].OperationID != "" {
			continue
		}
		for _, op := range ops {
			if strings.TrimSpace(op.ID) == "" || modifiedOpNotePath(op) != attached[index].NotePath {
				continue
			}
			if attached[index].Field != "" && strings.TrimSpace(op.Field) != attached[index].Field {
				continue
			}
			if attached[index].NodeRef != "" {
				ref, err := ontologyNodeRefFromOp(op)
				if err != nil || ref.String() != attached[index].NodeRef {
					continue
				}
			}
			attached[index].OperationID = op.ID
			break
		}
	}
	return attached
}

func replaceableOntologyEditOpKey(op OntologyEditOp) (string, bool) {
	if id := strings.TrimSpace(op.ID); id != "" {
		return "id|" + id, true
	}
	kind := strings.TrimSpace(op.Kind)
	switch kind {
	case "setField", "setLinkField", "setRootMetadataList":
	default:
		return "", false
	}
	field := strings.TrimSpace(op.Field)
	if field == "" {
		return "", false
	}
	ref, err := ontologyNodeRefFromOp(op)
	if err != nil {
		return "", false
	}
	return strings.Join([]string{kind, canonicalNodeRefKey(ref), field}, "|"), true
}

func (s *Server) ontologyEditOpMatchesCurrentValue(ctx context.Context, defs *ontologyDefinitions, nodeScope *noderead.Scope, op OntologyEditOp) (bool, error) {
	kind := strings.TrimSpace(op.Kind)
	if kind != "setField" && kind != "setLinkField" && kind != "setRootMetadataList" {
		return false, nil
	}
	field := strings.TrimSpace(op.Field)
	if field == "" {
		return false, nil
	}
	ref, err := ontologyNodeRefFromOp(op)
	if err != nil {
		return false, nil
	}
	projection, err := s.resolveNodeProjection(ctx, defs, nodeScope, ref)
	if err != nil {
		return false, nil
	}
	binding, ok := projection.Fields[field]
	if !ok {
		if strings.EqualFold(field, "title") {
			return len(ontologyEditOpValues(op)) == 1 && oneLineOntologyEditTitleValue(ontologyEditOpValues(op)[0]) == projectedOntologyEditTitle(projection), nil
		}
		return false, nil
	}
	desiredKind := ontologyEditOpFieldValueKind(op)
	if desiredKind == "unset" {
		return !binding.Present, nil
	}
	currentValues := binding.Values
	if desiredKind == "scalar" && binding.Present && len(currentValues) == 0 {
		currentValues = []string{""}
	}
	return binding.Present && stringSlicesEqual(currentValues, ontologyEditOpValues(op)), nil
}

func ontologyEditOpFieldValueKind(op OntologyEditOp) string {
	if op.FieldValue != nil {
		switch strings.ToLower(strings.TrimSpace(op.FieldValue.Kind)) {
		case "unset", "scalar", "list":
			return strings.ToLower(strings.TrimSpace(op.FieldValue.Kind))
		}
	}
	if strings.TrimSpace(op.Kind) == "setLinkField" || strings.TrimSpace(op.Kind) == "setRootMetadataList" || op.Values != nil {
		return "list"
	}
	return "scalar"
}

func ontologyEditOpValues(op OntologyEditOp) []string {
	if op.FieldValue != nil {
		switch strings.ToLower(strings.TrimSpace(op.FieldValue.Kind)) {
		case "unset":
			return nil
		case "scalar":
			return []string{op.FieldValue.Scalar}
		case "list":
			return append([]string(nil), op.FieldValue.Items...)
		}
	}
	if strings.TrimSpace(op.Kind) == "setLinkField" || strings.TrimSpace(op.Kind) == "setRootMetadataList" || op.Values != nil {
		return append([]string(nil), op.Values...)
	}
	return []string{op.Value}
}

func stringSlicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func projectedOntologyEditTitle(projection *ontology.NodeProjection) string {
	if projection == nil || projection.Snapshot == nil {
		return ""
	}
	if projection.Ref.Kind == ontology.NodeKindNote {
		if title, ok := projection.Snapshot.Frontmatter["title"].(string); ok && strings.TrimSpace(title) != "" {
			return title
		}
		for _, section := range projection.Snapshot.Sections {
			if section != nil && section.Level == ontology.SectionLevelH1 && strings.TrimSpace(section.Title) != "" {
				return section.Title
			}
		}
		return strings.TrimSuffix(filepath.Base(projection.Ref.NotePath), filepath.Ext(projection.Ref.NotePath))
	}
	if span := projection.Snapshot.SourceSpansByID[projection.Ref.NodeID]; span != nil {
		return span.Title
	}
	if section := projection.Snapshot.SectionsByID[projection.Ref.NodeID]; section != nil {
		return section.Title
	}
	return ""
}

func oneLineOntologyEditTitleValue(value string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(value, "\n", " ")), " ")
}

func (s *Server) buildOntologyEditSessionResponse(ctx context.Context, session *ontologyEditSession, defs *ontologyDefinitions, revalidate bool, includePlan bool) (OntologyEditSessionResponse, error) {
	if err := session.lockLive(); err != nil {
		return OntologyEditSessionResponse{}, err
	}
	defer session.mu.Unlock()
	return s.buildOntologyEditSessionResponseLocked(ctx, session, defs, revalidate, includePlan)
}

// buildOntologyEditSessionResponseLocked requires session.mu. Keeping response
// construction under the same lease as a mutation prevents a later request
// from being mistaken for the acknowledgement of an earlier client revision.
func (s *Server) buildOntologyEditSessionResponseLocked(ctx context.Context, session *ontologyEditSession, defs *ontologyDefinitions, revalidate bool, includePlan bool) (OntologyEditSessionResponse, error) {
	response := OntologyEditSessionResponse{
		SessionID:              session.ID,
		Revision:               session.Revision,
		Ops:                    cloneOntologyEditOps(session.Ops),
		TouchedPaths:           touchedPathsFromOps(session.Ops),
		TouchedNodes:           touchedNodesFromOps(session.Ops),
		TouchedNodeRefs:        touchedNodeRefsFromOps(session.Ops),
		ChangedFieldsByNode:    changedFieldsByNode(session.Ops),
		ChangedFieldsByNodeRef: changedFieldsByNodeRef(session.Ops),
		CollectionChanges:      collectionChangesFromOps(session.Ops),
		HasUncommittedChanges:  len(session.Ops) > 0,
		CreatedAt:              session.CreatedAt,
		UpdatedAt:              session.UpdatedAt,
	}
	if !revalidate {
		if session.Editor != nil && len(response.Ops) > 0 {
			plan, err := session.Editor.Preview(ctx)
			if err != nil {
				return OntologyEditSessionResponse{}, err
			}
			response.BaseFingerprints = baseFingerprintsFromPlan(plan)
			response.BaseDocuments = session.Editor.BaseDocuments()
		}
		response.Rebased = session.Rebased
		response.StalePaths = append([]string(nil), session.StalePaths...)
		if len(response.Ops) == 0 {
			response.Status = OntologyEditSessionStatusClean
		} else if response.Rebased {
			response.Status = OntologyEditSessionStatusRebased
		} else {
			response.Status = OntologyEditSessionStatusDirty
		}
		return response, nil
	}
	plan, conflicts, err := s.previewSessionEditor(ctx, session, defs)
	if err != nil {
		return OntologyEditSessionResponse{}, err
	}
	response.Status = ontologyEditSessionStatus(response.Ops, plan, conflicts)
	response.BaseFingerprints = baseFingerprintsFromPlan(plan)
	response.BaseDocuments = session.Editor.BaseDocuments()
	response.StalePaths = stalePathsFromPlan(plan)
	response.Rebased = planHasRebased(plan)
	response.Conflicts = attachOntologyConflictOperationIDs(conflicts, response.Ops)
	if includePlan {
		response.Plan = &plan
	}
	return response, nil
}

func (session *ontologyEditSession) invalidateReadOverlayCacheLocked() {
	session.Overlay = nil
}

func (session *ontologyEditSession) cachedReadOverlayLocked(key ontologyEditSessionReadOverlayCacheKey) (*ontologyquery.ReadOverlay, bool) {
	if session.Overlay == nil || session.Overlay.Key != key {
		return nil, false
	}
	return session.Overlay.Overlay, true
}

func (session *ontologyEditSession) storeReadOverlayLocked(key ontologyEditSessionReadOverlayCacheKey, overlay *ontologyquery.ReadOverlay, opsLen int) {
	session.Overlay = &ontologyEditSessionReadOverlayCache{
		Key:     key,
		Overlay: overlay,
		OpsLen:  opsLen,
	}
}

func ontologyReadOverlayCacheKey(ops []OntologyEditOp, schemaHash string, plan ontology.CommitPlan, status OntologyEditSessionStatus, conflicted bool, rebased bool) ontologyEditSessionReadOverlayCacheKey {
	return ontologyEditSessionReadOverlayCacheKey{
		OpsHash:              ontologyEditOpsHash(ops),
		SchemaHash:           strings.TrimSpace(schemaHash),
		BaseFingerprintsHash: stringMapHash(baseFingerprintsFromPlan(plan)),
		Status:               status,
		Rebased:              rebased,
		Conflicted:           conflicted,
	}
}

func ontologyEditOpsHash(ops []OntologyEditOp) string {
	data, err := json.Marshal(cloneOntologyEditOps(ops))
	if err != nil {
		return fmt.Sprintf("ops:%d", len(ops))
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func stringMapHash(values map[string]string) string {
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, key := range keys {
		_, _ = h.Write([]byte(key))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(values[key]))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func ontologyEditSessionStatus(ops []OntologyEditOp, plan ontology.CommitPlan, conflicts []ontology.ConflictReport) OntologyEditSessionStatus {
	switch {
	case len(conflicts) > 0:
		return OntologyEditSessionStatusConflicted
	case planHasRebased(plan):
		return OntologyEditSessionStatusRebased
	case len(ops) == 0:
		return OntologyEditSessionStatusClean
	default:
		return OntologyEditSessionStatusDirty
	}
}

func touchedPathsFromOps(ops []OntologyEditOp) []string {
	set := map[string]struct{}{}
	for _, op := range ops {
		path, _, _ := splitLinkFragment(strings.TrimSpace(op.Path))
		if path == "" {
			path = strings.TrimSpace(op.Path)
		}
		if path != "" {
			set[path] = struct{}{}
		}
	}
	return sortedStringSet(set)
}

func touchedNodesFromOps(ops []OntologyEditOp) []string {
	set := map[string]struct{}{}
	for _, op := range ops {
		if path := strings.TrimSpace(op.Path); path != "" {
			set[path] = struct{}{}
		}
	}
	return sortedStringSet(set)
}

func touchedNodeRefsFromOps(ops []OntologyEditOp) []ontology.NodeRef {
	if len(ops) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]ontology.NodeRef, 0, len(ops))
	for _, op := range ops {
		ref, err := ontologyNodeRefFromOp(op)
		if err != nil {
			continue
		}
		key := canonicalNodeRefKey(ref)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Slice(out, func(i, j int) bool {
		return canonicalNodeRefKey(out[i]) < canonicalNodeRefKey(out[j])
	})
	return out
}

func canonicalNodeRefKey(ref ontology.NodeRef) string {
	return strings.Join([]string{
		strings.TrimSpace(ref.NotePath),
		strings.TrimSpace(ref.Fragment),
		strings.TrimSpace(ref.NodeID),
		string(ref.Kind),
		strings.TrimSpace(ref.Structural),
	}, "|")
}

func changedFieldsByNode(ops []OntologyEditOp) map[string][]string {
	return changedFieldsByKey(ops, func(op OntologyEditOp) string {
		return strings.TrimSpace(op.Path)
	})
}

func changedFieldsByNodeRef(ops []OntologyEditOp) map[string][]string {
	return changedFieldsByKey(ops, func(op OntologyEditOp) string {
		ref, err := ontologyNodeRefFromOp(op)
		if err != nil {
			return ""
		}
		return canonicalNodeRefKey(ref)
	})
}

func changedFieldsByKey(ops []OntologyEditOp, keyForOp func(OntologyEditOp) string) map[string][]string {
	out := map[string][]string{}
	seen := map[string]map[string]struct{}{}
	for _, op := range ops {
		key := keyForOp(op)
		if key == "" {
			continue
		}
		field := strings.TrimSpace(op.Field)
		if field == "" {
			continue
		}
		if seen[key] == nil {
			seen[key] = map[string]struct{}{}
		}
		if _, ok := seen[key][field]; ok {
			continue
		}
		seen[key][field] = struct{}{}
		out[key] = append(out[key], field)
	}
	for key := range out {
		sort.Strings(out[key])
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func collectionChangesFromOps(ops []OntologyEditOp) []OntologyCollectionChange {
	out := make([]OntologyCollectionChange, 0)
	for _, op := range ops {
		switch strings.TrimSpace(op.Kind) {
		case "addEmbeddedNode", "reorderCollection", "deleteNode":
			collection := strings.TrimSpace(op.Collection)
			if collection == "" && strings.TrimSpace(op.Kind) == "deleteNode" {
				continue
			}
			ref, _ := ontologyNodeRefFromOp(op)
			var refPtr *ontology.NodeRef
			if !ref.IsZero() {
				refCopy := ref
				refPtr = &refCopy
			}
			out = append(out, OntologyCollectionChange{
				Path:             strings.TrimSpace(op.Path),
				Ref:              refPtr,
				Collection:       collection,
				OrderedFragments: append([]string(nil), op.OrderedFragments...),
			})
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func baseFingerprintsFromPlan(plan ontology.CommitPlan) map[string]string {
	if len(plan.Files) == 0 {
		return nil
	}
	out := make(map[string]string, len(plan.Files))
	for _, file := range plan.Files {
		out[file.NotePath] = file.BaseFingerprint
	}
	return out
}

func stalePathsFromPlan(plan ontology.CommitPlan) []string {
	set := map[string]struct{}{}
	for _, file := range plan.Files {
		if file.Rebased {
			set[file.NotePath] = struct{}{}
		}
	}
	return sortedStringSet(set)
}

func planHasRebased(plan ontology.CommitPlan) bool {
	for _, file := range plan.Files {
		if file.Rebased {
			return true
		}
	}
	return false
}

func sortedStringSet(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func mergeStringSets(values ...[]string) []string {
	set := map[string]struct{}{}
	for _, group := range values {
		for _, value := range group {
			if strings.TrimSpace(value) == "" {
				continue
			}
			set[value] = struct{}{}
		}
	}
	return sortedStringSet(set)
}

func (s *Server) previewSessionEditor(ctx context.Context, session *ontologyEditSession, defs *ontologyDefinitions) (ontology.CommitPlan, []ontology.ConflictReport, error) {
	// Docs: [[ontology-edit-replay-conflict-contract]] owns rebase behavior.
	if session.Editor == nil {
		editor, err := s.replayOntologyEditOps(defs, session.Ops)
		if err != nil {
			return ontology.CommitPlan{}, nil, err
		}
		session.Editor = editor
	}
	plan, conflicts, err := session.Editor.PreviewCurrent(ctx)
	if err != nil {
		return ontology.CommitPlan{}, nil, err
	}
	session.Rebased = planHasRebased(plan)
	session.StalePaths = stalePathsFromPlan(plan)
	if len(conflicts) > 0 {
		return plan, conflicts, nil
	}
	relationConflicts, err := s.validateOntologyRelationOpsAgainstPlan(ctx, defs, session.Ops, plan)
	if err != nil {
		return ontology.CommitPlan{}, nil, err
	}
	if len(relationConflicts) > 0 {
		return plan, relationConflicts, nil
	}
	return plan, nil, nil
}

func (s *Server) validateOntologyRelationOpsAgainstPlan(ctx context.Context, defs *ontologyDefinitions, ops []OntologyEditOp, plan ontology.CommitPlan) ([]ontology.ConflictReport, error) {
	if defs == nil || defs.schema == nil {
		return nil, fmt.Errorf("ontology schema is unavailable")
	}
	overlay := s.markdownReadOverlayFromCommitPlan(plan, OntologyEditSessionStatusDirty, false)
	targetScope := s.nodeReadScopeWithOverlay(ctx, defs, overlay)
	ownerScope := s.nodeReadScope(ctx, defs)
	conflicts := make([]ontology.ConflictReport, 0)
	for _, op := range ops {
		if strings.TrimSpace(op.Kind) != "setLinkField" {
			continue
		}
		ref, err := ontologyNodeRefFromOp(op)
		if err != nil {
			return nil, err
		}
		// The operation ref is canonical to the durable source. Resolve its
		// owning field there; earlier staged edits may have changed the
		// unanchored node's structural fingerprint in the overlay. Relation
		// targets still resolve against the completed plan so staged target
		// changes participate in validation.
		owner, err := ownerScope.Projection(ctx, ref)
		if err == nil {
			err = s.validateOntologyEditLinkTargets(ctx, defs, targetScope, owner, op)
		}
		if err == nil {
			continue
		}
		conflicts = append(conflicts, ontology.ConflictReport{
			Kind:        ontology.ConflictKindUnsupportedTarget,
			OperationID: op.ID,
			NotePath:    ref.NotePath,
			NodeRef:     ref.String(),
			Field:       op.Field,
			Message:     err.Error(),
		})
	}
	return conflicts, nil
}

func (s *Server) replayOntologyEditOps(defs *ontologyDefinitions, ops []OntologyEditOp) (*ontology.EditSession, error) {
	bases, err := editBaseDocumentsFromOps(ops)
	if err != nil {
		return nil, err
	}
	return s.replayOntologyEditOpsWithBases(defs, ops, bases)
}

func (s *Server) replayOntologyEditOpsWithBases(defs *ontologyDefinitions, ops []OntologyEditOp, bases []ontology.EditBaseDocument) (*ontology.EditSession, error) {
	if defs == nil || defs.schema == nil {
		return nil, fmt.Errorf("ontology schema is unavailable")
	}
	editSession := s.newOntologyEditSession(defs.schema)
	for _, op := range ops {
		if err := applyOntologyEditOp(editSession, defs.schema, op); err != nil {
			return nil, err
		}
	}
	if len(bases) > 0 {
		if err := editSession.RestoreBases(bases); err != nil {
			return nil, err
		}
	}
	if len(ops) > 0 {
		if _, err := editSession.Preview(context.Background()); err != nil {
			return nil, err
		}
	}
	return editSession, nil
}

func applyOntologyEditOp(editSession *ontology.EditSession, schema *ontology.Schema, op OntologyEditOp) error {
	switch strings.TrimSpace(op.Kind) {
	case "setSource":
		notePath := strings.TrimSpace(op.Path)
		witness := op.PreviousMarkdown
		if op.Expected != nil {
			witness = op.Expected.SourceContent
		}
		if op.Expected == nil || strings.TrimSpace(op.Expected.SourceHash) == "" {
			return fmt.Errorf("setSource requires a verified full-file source witness")
		}
		if err := validateBoundedSourceEdit(witness, op.Markdown, schema); err != nil {
			return err
		}
		return editSession.StageFileTransform(notePath, func(content string) (string, error) {
			if content != witness {
				return "", fmt.Errorf("source changed while applying setSource for %s", notePath)
			}
			return op.Markdown, nil
		})
	case "appendAlias":
		notePath := strings.TrimSpace(op.Path)
		value := op.Value
		return editSession.StageFileTransform(notePath, func(content string) (string, error) {
			return validate.ApplyAliasAppendInMemory(content, value)
		})
	case "setFrontmatter":
		notePath := strings.TrimSpace(op.Path)
		property := strings.TrimSpace(op.Property)
		if property == "" {
			property = strings.TrimSpace(op.Field)
		}
		value := op.Value
		values := append([]string(nil), op.Values...)
		return editSession.StageFileTransform(notePath, func(content string) (string, error) {
			return validate.ApplySetFrontmatterInMemory(content, property, value, values)
		})
	case "rewriteBrokenLink":
		notePath := strings.TrimSpace(op.Path)
		oldTarget := op.OldTarget
		newTarget := op.NewTarget
		return editSession.StageFileTransform(notePath, func(content string) (string, error) {
			updated, _ := validate.ApplyRewriteLinkInMemory(content, oldTarget, newTarget)
			return updated, nil
		})
	case "addSectionScaffold":
		notePath := strings.TrimSpace(op.Path)
		level := strings.TrimSpace(op.Level)
		if level == "" {
			level = strings.TrimSpace(op.Property)
		}
		heading := op.Value
		if strings.TrimSpace(heading) == "" {
			heading = op.Heading
		}
		return editSession.StageFileTransform(notePath, func(content string) (string, error) {
			return validate.AddSectionScaffold(content, level, heading), nil
		})
	}
	ref, err := ontologyNodeRefFromOp(op)
	if err != nil {
		return err
	}
	switch strings.TrimSpace(op.Kind) {
	case "setField":
		values, mode, err := ontologyEditFieldValues(op)
		if err != nil {
			return err
		}
		field := strings.TrimSpace(op.Field)
		if op.FieldValue != nil && op.Expected != nil && op.Expected.Field != nil {
			expected, expectedMode, err := ontologyEditFieldValues(OntologyEditOp{FieldValue: op.Expected.Field})
			if err != nil {
				return fmt.Errorf("invalid expected field witness: %w", err)
			}
			return editSession.SetFieldValueWithWitness(ref, field, values, expected, expectedMode, mode == "list", false, mode == "unset")
		}
		switch mode {
		case "unset":
			return editSession.UnsetField(ref, field)
		case "list":
			return editSession.SetScalarListField(ref, field, values)
		default:
			return editSession.SetScalarField(ref, field, values[0])
		}
	case "setLinkField":
		values, mode, err := ontologyEditFieldValues(op)
		if err != nil {
			return err
		}
		field := strings.TrimSpace(op.Field)
		if op.FieldValue != nil && op.Expected != nil && op.Expected.Field != nil {
			expected, expectedMode, err := ontologyEditFieldValues(OntologyEditOp{FieldValue: op.Expected.Field})
			if err != nil {
				return fmt.Errorf("invalid expected field witness: %w", err)
			}
			return editSession.SetFieldValueWithWitness(ref, field, values, expected, expectedMode, true, true, mode == "unset")
		}
		if mode == "unset" {
			return editSession.UnsetLinkField(ref, field)
		}
		return editSession.SetLinkField(ref, field, values)
	case "setRootMetadataList":
		values, mode, err := ontologyEditFieldValues(op)
		if err != nil {
			return err
		}
		if op.FieldValue != nil && op.Expected != nil && op.Expected.Field != nil {
			expected, expectedMode, err := ontologyEditFieldValues(OntologyEditOp{FieldValue: op.Expected.Field})
			if err != nil {
				return fmt.Errorf("invalid expected field witness: %w", err)
			}
			return editSession.SetRootMetadataListWithWitness(ref, strings.TrimSpace(op.Field), values, expected, expectedMode, mode == "unset")
		}
		if mode == "unset" {
			return editSession.UnsetRootMetadataList(ref, strings.TrimSpace(op.Field))
		}
		return editSession.SetRootMetadataList(ref, strings.TrimSpace(op.Field), values)
	case "setNarrative":
		return editSession.SetNarrative(ref, op.RangeStart, op.RangeEnd, op.PreviousMarkdown, op.Markdown)
	case "insertNarrative":
		return editSession.InsertNarrative(ref, op.Markdown)
	case "ensureBlockID":
		ref.Kind = ontology.NodeKindEmbedded
		return editSession.EnsureBlockID(ref, strings.TrimSpace(op.BlockID))
	case "setBlockID":
		ref.Kind = ontology.NodeKindEmbedded
		return editSession.SetBlockID(ref, strings.TrimSpace(op.BlockID))
	case "addEmbeddedNode":
		return editSession.AddEmbeddedNode(ref, strings.TrimSpace(op.Collection), strings.TrimSpace(op.Heading), op.Body, strings.TrimSpace(op.BlockID))
	case "deleteNode":
		return editSession.DeleteNode(ref)
	case "reorderCollection":
		return editSession.ReorderCollection(ref, strings.TrimSpace(op.Collection), append([]string(nil), op.OrderedFragments...))
	default:
		return fmt.Errorf("unsupported ontology edit op kind %q", op.Kind)
	}
}

func ontologyEditFieldValues(op OntologyEditOp) ([]string, string, error) {
	if op.FieldValue != nil {
		switch strings.ToLower(strings.TrimSpace(op.FieldValue.Kind)) {
		case "unset":
			return nil, "unset", nil
		case "scalar":
			return []string{op.FieldValue.Scalar}, "scalar", nil
		case "list":
			return append([]string(nil), op.FieldValue.Items...), "list", nil
		default:
			return nil, "", fmt.Errorf("unsupported ontology edit field value kind %q", op.FieldValue.Kind)
		}
	}
	if strings.TrimSpace(op.Kind) == "setLinkField" || strings.TrimSpace(op.Kind) == "setRootMetadataList" {
		return append([]string(nil), op.Values...), "list", nil
	}
	if op.Values != nil {
		return append([]string(nil), op.Values...), "list", nil
	}
	return []string{op.Value}, "scalar", nil
}

func ontologyNodeRefFromOp(op OntologyEditOp) (ontology.NodeRef, error) {
	path := strings.TrimSpace(op.Path)
	if path == "" {
		return ontology.NodeRef{}, fmt.Errorf("op path is required")
	}
	notePath, fragment, fragmentType := splitLinkFragment(path)
	if notePath == "" {
		notePath = path
	}
	ref := ontology.NodeRef{
		NotePath:   notePath,
		NodeID:     strings.TrimSpace(op.NodeID),
		Structural: strings.TrimSpace(op.Structural),
		Kind:       ontology.NodeKindNote,
	}
	if strings.TrimSpace(fragment) == "" {
		if nodeIDPath, nodeIDFragment, nodeIDFragmentType := splitLinkFragment(ref.NodeID); strings.TrimSpace(nodeIDFragment) != "" {
			if strings.TrimSpace(nodeIDPath) != "" {
				ref.NotePath = nodeIDPath
			}
			ref.Fragment = joinAnchorFragment(strings.TrimSpace(nodeIDFragment), nodeIDFragmentType)
			ref.Kind = ontology.NodeKindSection
			if nodeIDFragmentType == "block" || isItemFragment(nodeIDFragment) {
				ref.Kind = ontology.NodeKindEmbedded
			}
		}
		return ref, nil
	}
	ref.Fragment = joinAnchorFragment(strings.TrimSpace(fragment), fragmentType)
	ref.Kind = ontology.NodeKindSection
	if fragmentType == "block" || isItemFragment(fragment) {
		ref.Kind = ontology.NodeKindEmbedded
	}
	return ref, nil
}

func isItemFragment(fragment string) bool {
	return strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(fragment, "#")), "item-")
}

func (s *Server) refreshTouchedWorkspaces(ctx context.Context, refs []ontology.NodeRef) ([]NodeWorkspaceResponse, error) {
	out := make([]NodeWorkspaceResponse, 0, len(refs))
	seen := map[string]struct{}{}
	for _, ref := range refs {
		workspace, err := s.nodeWorkspace(ctx, ref, defaultNodeWorkspaceIncludes())
		if err != nil {
			if ref.Kind == ontology.NodeKindNote || strings.TrimSpace(ref.NotePath) == "" {
				return nil, err
			}
			fallback := ontology.NodeRef{
				NotePath: ref.NotePath,
				Kind:     ontology.NodeKindNote,
			}
			fallbackKey := canonicalNodeRefKey(fallback)
			if _, ok := seen[fallbackKey]; ok {
				continue
			}
			workspace, err = s.nodeWorkspace(ctx, fallback, defaultNodeWorkspaceIncludes())
			if err != nil {
				return nil, err
			}
			seen[fallbackKey] = struct{}{}
			out = append(out, workspace)
			continue
		}
		key := canonicalNodeRefKey(workspace.Node.Ref)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, workspace)
	}
	return out, nil
}

func applySessionStatusToWorkspaces(workspaces []NodeWorkspaceResponse, status OntologyEditSessionStatus, stalePaths []string, rebased bool) {
	if len(workspaces) == 0 {
		return
	}
	staleSet := map[string]struct{}{}
	for _, path := range stalePaths {
		if path == "" {
			continue
		}
		staleSet[path] = struct{}{}
	}
	sessionState := ""
	switch {
	case status == OntologyEditSessionStatusDirty,
		status == OntologyEditSessionStatusRebased,
		status == OntologyEditSessionStatusConflicted,
		status == OntologyEditSessionStatusStale:
		sessionState = string(status)
	case rebased:
		sessionState = string(OntologyEditSessionStatusRebased)
	}
	freshnessState := ""
	switch {
	case status == OntologyEditSessionStatusRebased,
		status == OntologyEditSessionStatusConflicted,
		status == OntologyEditSessionStatusStale:
		freshnessState = sessionState
	case rebased:
		freshnessState = string(OntologyEditSessionStatusRebased)
	}
	for idx := range workspaces {
		if sessionState != "" {
			workspaces[idx].Status.Session.State = sessionState
		}
		if freshnessState == "" {
			continue
		}
		if len(staleSet) == 0 && rebased {
			workspaces[idx].Status.Freshness.State = freshnessState
			continue
		}
		if _, ok := staleSet[workspaces[idx].Node.NotePath]; ok {
			workspaces[idx].Status.Freshness.State = freshnessState
		}
	}
}
