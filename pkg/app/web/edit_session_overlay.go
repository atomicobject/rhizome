package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/paths"
)

// stagedReadRequest is the JSON body of a staged read: the POST form of a read
// route, which keeps the route's query parameters and adds the edit session.
// Only a body can carry the session snapshot the server may need to restore.
type stagedReadRequest struct {
	EditSession *EditSessionReadRequest `json:"editSession,omitempty"`
}

// readOverlayForStagedRead returns the overlay for a staged read. GET reads
// committed state; POST decodes a stagedReadRequest. On failure it writes the
// error response and returns false.
func (s *Server) readOverlayForStagedRead(w http.ResponseWriter, r *http.Request) (*ontologyquery.ReadOverlay, bool) {
	if r.Method != http.MethodPost {
		return nil, true
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPublicViewExecuteBodyBytes)
	var req stagedReadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		status := http.StatusBadRequest
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			status = http.StatusRequestEntityTooLarge
		}
		writePublicError(w, status, PublicErrorBadRequest, err)
		return nil, false
	}
	overlay, err := s.readOverlayForEditSession(r.Context(), req.EditSession)
	if err != nil {
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
		return nil, false
	}
	return overlay, true
}

// stagedNoteFacts projects the staged source of a note the overlay touches
// through the registered provider for the overlay's format, so staged reads
// derive tags and fragment targets the way the indexer does. ok is false when
// the overlay leaves the note committed or the format cannot project.
func (s *Server) stagedNoteFacts(overlay *ontologyquery.ReadOverlay, notePath string) (noteformat.ProjectionFacts, bool) {
	content, ok := overlay.StagedContent(notePath)
	if !ok {
		return noteformat.ProjectionFacts{}, false
	}
	formats, err := s.noteMetadata.FormatRuntime()
	if err != nil {
		return noteformat.ProjectionFacts{}, false
	}
	provider, ok := formats.Provider(overlay.SourceFormat)
	if !ok {
		return noteformat.ProjectionFacts{}, false
	}
	cleanPath, err := paths.CleanNotePath(notePath)
	if err != nil {
		return noteformat.ProjectionFacts{}, false
	}
	source, err := noteformat.NewAuthoredSource(cleanPath, provider.Descriptor(), []byte(content), 0)
	if err != nil {
		return noteformat.ProjectionFacts{}, false
	}
	projection, err := formats.Project(source)
	if err != nil {
		return noteformat.ProjectionFacts{}, false
	}
	return projection.Facts, true
}

func (s *Server) readOverlayForEditSession(ctx context.Context, req *EditSessionReadRequest) (*ontologyquery.ReadOverlay, error) {
	if req == nil || req.IsZero() {
		return nil, nil
	}
	sessionID := strings.TrimSpace(req.RequestedID())
	if sessionID == "" && req.Snapshot != nil {
		sessionID = strings.TrimSpace(req.Snapshot.SessionID)
	}
	if sessionID == "" {
		return nil, fmt.Errorf("editSession.sessionId is required")
	}
	defs, err := s.ontologyDefinitions()
	if err != nil {
		return nil, err
	}
	if defs == nil || defs.schema == nil {
		return nil, fmt.Errorf("ontology is unavailable")
	}
	session, err := s.ensureOntologyEditSession(ctx, defs, sessionID, req.Snapshot)
	if err != nil {
		return nil, err
	}
	if err := session.lockLive(); err != nil {
		return nil, err
	}
	plan, conflicts, err := s.previewSessionEditor(ctx, session, defs)
	if err != nil {
		session.mu.Unlock()
		return nil, err
	}
	status := ontologyEditSessionStatus(session.Ops, plan, conflicts)
	key := ontologyReadOverlayCacheKey(session.Ops, defs.schema.Hash, plan, status, len(conflicts) > 0, session.Rebased)
	if overlay, ok := session.cachedReadOverlayLocked(key); ok {
		session.mu.Unlock()
		return overlay, nil
	}
	if len(conflicts) > 0 {
		// Conflicts are reported by the session routes and resolved there;
		// reads keep serving staged state meanwhile.
		plan, err = stagedPlanForConflictedPaths(ctx, session.Editor, plan, conflicts)
		if err != nil {
			session.mu.Unlock()
			return nil, err
		}
	}
	previous := session.Overlay
	overlay := s.markdownReadOverlayFromCommitPlan(plan, status, session.Rebased)
	if overlay != nil {
		overlay.Conflicted = len(conflicts) > 0
		index, err := buildReadOverlayIndexForSessionRevision(ctx, defs.schema, overlay, previous, session.Ops, key)
		if err != nil {
			session.mu.Unlock()
			return nil, err
		}
		overlay.Index = index
	}
	session.storeReadOverlayLocked(key, overlay, len(session.Ops))
	session.mu.Unlock()
	return overlay, nil
}

// stagedPlanForConflictedPaths swaps each conflicted note's plan entry for its
// replay against the session base, which is what the author staged. A
// conflicted replay against disk stops early and would show disk content.
// ponytail: a relation conflict on a note that also drifted on disk shows the
// base replay without that drift until the conflict is resolved.
func stagedPlanForConflictedPaths(ctx context.Context, editor *ontology.EditSession, plan ontology.CommitPlan, conflicts []ontology.ConflictReport) (ontology.CommitPlan, error) {
	if editor == nil {
		return plan, nil
	}
	// A base replay that itself fails still returns the files it finished;
	// the rest keep their disk replay.
	staged, _ := editor.Preview(ctx)
	if err := ctx.Err(); err != nil {
		return plan, err
	}
	stagedByPath := make(map[string]ontology.FileCommitPlan, len(staged.Files))
	for _, file := range staged.Files {
		stagedByPath[file.NotePath] = file
	}
	conflicted := make(map[string]bool, len(conflicts))
	for _, conflict := range conflicts {
		conflicted[conflict.NotePath] = true
	}
	out := plan
	out.Files = append([]ontology.FileCommitPlan(nil), plan.Files...)
	for index, file := range out.Files {
		if replay, ok := stagedByPath[file.NotePath]; ok && conflicted[file.NotePath] {
			out.Files[index].UpdatedContentPreview = replay.UpdatedContentPreview
			out.Files[index].HasMaterialChange = replay.HasMaterialChange
		}
	}
	return out, nil
}

func buildReadOverlayIndexForSessionRevision(ctx context.Context, schema *ontology.Schema, overlay *ontologyquery.ReadOverlay, previous *ontologyEditSessionReadOverlayCache, ops []OntologyEditOp, key ontologyEditSessionReadOverlayCacheKey) (*noderead.ReadOverlayIndex, error) {
	if previous == nil || previous.Overlay == nil || previous.Overlay.Index == nil {
		return noderead.BuildReadOverlayIndex(ctx, schema, overlay)
	}
	if previous.Key.SchemaHash != key.SchemaHash ||
		previous.Key.BaseFingerprintsHash != key.BaseFingerprintsHash ||
		previous.Key.Status != key.Status ||
		previous.Key.Rebased != key.Rebased ||
		previous.Key.Conflicted != key.Conflicted ||
		len(ops) != previous.OpsLen+1 {
		return noderead.BuildReadOverlayIndex(ctx, schema, overlay)
	}
	path, ok := simpleIncrementalOverlayOpPath(ops[len(ops)-1])
	if !ok {
		return noderead.BuildReadOverlayIndex(ctx, schema, overlay)
	}
	return noderead.BuildReadOverlayIndexReplacingPaths(ctx, schema, overlay, previous.Overlay.Index, []string{path})
}

func simpleIncrementalOverlayOpPath(op OntologyEditOp) (string, bool) {
	switch strings.TrimSpace(op.Kind) {
	case "setField", "setLinkField":
	default:
		return "", false
	}
	path := strings.TrimSpace(modifiedOpNotePath(op))
	if path == "" {
		return "", false
	}
	return path, true
}

// markdownReadOverlayFromCommitPlan passes only Markdown sources to the
// Markdown-only noderead overlay index. Projectable providers are not a
// Markdown compatibility contract.
func (s *Server) markdownReadOverlayFromCommitPlan(plan ontology.CommitPlan, status OntologyEditSessionStatus, rebased bool) *ontologyquery.ReadOverlay {
	overlay := readOverlayFromCommitPlan(plan, status, rebased, s.markdownCompatibilityFormat)
	if overlay != nil {
		overlay.SourceFormat = markdownCompatibilityFormatID
	}
	return overlay
}

func readOverlayFromCommitPlan(plan ontology.CommitPlan, status OntologyEditSessionStatus, rebased bool, includePath func(string) bool) *ontologyquery.ReadOverlay {
	files := make(map[string]string, len(plan.Files))
	touched := make([]string, 0, len(plan.Files))
	for _, file := range plan.Files {
		if !file.HasMaterialChange {
			continue
		}
		notePath := strings.TrimSpace(file.NotePath)
		if notePath == "" || !includePath(notePath) {
			continue
		}
		files[notePath] = file.UpdatedContentPreview
		touched = append(touched, notePath)
	}
	if len(files) == 0 {
		return nil
	}
	return &ontologyquery.ReadOverlay{
		UpdatedContentByPath: files,
		TouchedPaths:         touched,
		Status:               string(status),
		Rebased:              rebased,
	}
}
