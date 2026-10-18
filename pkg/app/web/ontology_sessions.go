package web

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/atomicobject/rhizome/pkg/ontology"

	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
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

func projectedOntologyEditTitle(projection *ontology.NodeProjection) string {
	if projection == nil || projection.Snapshot == nil {
		return ""
	}
	if projection.Ref.Kind == ontology.NodeKindNote {
		if title, ok := projection.Snapshot.Frontmatter["title"].(string); ok && strings.TrimSpace(title) != "" {
			return title
		}
		for _, section := range projection.Snapshot.Sections {
			if section != nil && section.Level == ontoschema.SectionLevelH1 && strings.TrimSpace(section.Title) != "" {
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
