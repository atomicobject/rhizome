package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/validate"
)

type ontologyCommitReceipt struct {
	SessionID      string                      `json:"sessionId"`
	RequestID      string                      `json:"requestId"`
	SubmissionHash string                      `json:"submissionHash"`
	Response       OntologyEditSessionResponse `json:"response"`
}

func (s *Server) readOntologyCommitReceipt(sessionID, requestID string, expectedRevision uint64, snapshot *OntologyEditSessionSnapshot) (OntologyEditSessionResponse, bool, error) {
	path, err := s.ontologyCommitReceiptPath(sessionID, requestID)
	if err != nil || path == "" {
		return OntologyEditSessionResponse{}, false, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return OntologyEditSessionResponse{}, false, nil
	}
	if err != nil {
		return OntologyEditSessionResponse{}, false, err
	}
	var receipt ontologyCommitReceipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		return OntologyEditSessionResponse{}, false, fmt.Errorf("read edit commit receipt: %w", err)
	}
	if receipt.SessionID != sessionID || receipt.RequestID != strings.TrimSpace(requestID) {
		return OntologyEditSessionResponse{}, false, fmt.Errorf("edit commit receipt identity mismatch")
	}
	if snapshot != nil && receipt.SubmissionHash != ontologyCommitSubmissionHash(sessionID, expectedRevision, snapshot.Ops, snapshot.BaseDocuments) {
		return OntologyEditSessionResponse{}, false, fmt.Errorf("commit request id %q was reused for a different submission", requestID)
	}
	return receipt.Response, true, nil
}

func (s *Server) writeOntologyCommitReceipt(ctx context.Context, sessionID, requestID, submissionHash string, response OntologyEditSessionResponse) error {
	artifact, err := s.ontologyCommitReceiptArtifact(sessionID, requestID, submissionHash, response)
	if err != nil || artifact == nil {
		return err
	}
	return validate.PublishRepairCompletionArtifact(ctx, validate.RunContext{
		VaultDef: s.cfg.VaultDef, VaultPath: s.cfg.VaultPath,
	}, &validate.RepairCompletionArtifact{Path: artifact.Path, Content: artifact.Content})
}

func (s *Server) ontologyCommitReceiptArtifact(sessionID, requestID, submissionHash string, response OntologyEditSessionResponse) (*ontology.CommitCompletionArtifact, error) {
	path, err := s.ontologyCommitReceiptPath(sessionID, requestID)
	if err != nil || path == "" {
		return nil, err
	}
	payload, err := json.Marshal(ontologyCommitReceipt{
		SessionID: sessionID, RequestID: strings.TrimSpace(requestID), SubmissionHash: submissionHash,
		Response: compactOntologyCommitReceiptResponse(response),
	})
	if err != nil {
		return nil, err
	}
	return &ontology.CommitCompletionArtifact{Path: path, Content: payload}, nil
}

func compactOntologyCommitReceiptResponse(response OntologyEditSessionResponse) OntologyEditSessionResponse {
	return OntologyEditSessionResponse{
		SessionID:              response.SessionID,
		Revision:               response.Revision,
		Status:                 response.Status,
		Outcome:                response.Outcome,
		TouchedPaths:           append([]string(nil), response.TouchedPaths...),
		TouchedNodes:           append([]string(nil), response.TouchedNodes...),
		TouchedNodeRefs:        append([]ontology.NodeRef(nil), response.TouchedNodeRefs...),
		RefLineage:             append([]ontology.PreviewRefLineage(nil), response.RefLineage...),
		ChangedFieldsByNode:    cloneStringSliceMap(response.ChangedFieldsByNode),
		ChangedFieldsByNodeRef: cloneStringSliceMap(response.ChangedFieldsByNodeRef),
		CollectionChanges:      append([]OntologyCollectionChange(nil), response.CollectionChanges...),
		StalePaths:             append([]string(nil), response.StalePaths...),
		Rebased:                response.Rebased,
		HasUncommittedChanges:  response.HasUncommittedChanges,
		Warnings:               append([]string(nil), response.Warnings...),
		CreatedAt:              response.CreatedAt,
		UpdatedAt:              response.UpdatedAt,
	}
}

func cloneStringSliceMap(values map[string][]string) map[string][]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string][]string, len(values))
	for key, items := range values {
		cloned[key] = append([]string(nil), items...)
	}
	return cloned
}

func ontologyCommitSubmissionHash(sessionID string, expectedRevision uint64, ops []OntologyEditOp, bases []ontology.EditBaseDocument) string {
	payload, err := json.Marshal(struct {
		SessionID        string                      `json:"sessionId"`
		ExpectedRevision uint64                      `json:"expectedRevision"`
		Ops              []OntologyEditOp            `json:"ops"`
		BaseDocuments    []ontology.EditBaseDocument `json:"baseDocuments"`
	}{sessionID, expectedRevision, cloneOntologyEditOps(ops), bases})
	if err != nil {
		return "invalid-submission"
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func (s *Server) ontologyCommitReceiptPath(sessionID, requestID string) (string, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return "", nil
	}
	root := strings.TrimSpace(s.cfg.VaultPath)
	if root == "" {
		root = s.cfg.VaultDef.BasePath()
	}
	if root == "" {
		return "", fmt.Errorf("vault root is unavailable")
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve vault root for edit receipt: %w", err)
	}
	digest := sha256.Sum256([]byte(sessionID + "\x00" + requestID))
	return filepath.Join(resolvedRoot, ".rhizome", "edit-receipts", hex.EncodeToString(digest[:])+".json"), nil
}
