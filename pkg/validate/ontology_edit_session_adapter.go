package validate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
)

const FixKindOntologyEditSessionCommit = "ontology_edit_session_commit"

// BuildOntologyEditSessionRepairResult adapts a reviewed EditSession preview
// into the journaled repair engine's immutable input. It rereads every current
// source once and binds the complete-file precondition to the preview hash.
// All files share the edit-session identity and therefore commit in one
// transaction with one recovery journal.
func BuildOntologyEditSessionRepairResult(
	ctx context.Context,
	runCtx RunContext,
	sessionID string,
	preview ontology.CommitPlan,
) (Result, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return Result{}, fmt.Errorf("ontology edit session id is required")
	}
	files := append([]ontology.FileCommitPlan(nil), preview.Files...)
	sort.Slice(files, func(i, j int) bool { return files[i].NotePath < files[j].NotePath })
	seenPaths := make(map[string]struct{}, len(files))
	material := make([]ontology.FileCommitPlan, 0, len(files))
	for _, file := range files {
		path := strings.TrimSpace(file.NotePath)
		if path == "" {
			return Result{}, fmt.Errorf("ontology edit preview contains an empty note path")
		}
		if _, duplicate := seenPaths[path]; duplicate {
			return Result{}, fmt.Errorf("ontology edit preview contains duplicate path %q", path)
		}
		seenPaths[path] = struct{}{}
		if file.HasMaterialChange {
			material = append(material, file)
		}
	}
	if len(material) == 0 {
		return Result{OK: true, SelectedChecks: []string{CheckOntology}, Checks: []CheckResult{{Name: CheckOntology, OK: true}}}, nil
	}

	identity := "edit-session:v1:" + stableEditSessionHash(sessionID)
	issueKey := "issue:v1:" + stableEditSessionHash(identity+"\x00"+commitPlanStableInput(material))
	action := FixAction{
		Check: CheckOntology, IssueCode: FixKindOntologyEditSessionCommit,
		Kind: FixKindOntologyEditSessionCommit, Safety: FixSafetySafe,
		Title: "Save ontology edit session", IssueKeys: []string{issueKey},
	}
	action.ID = "action:v1:" + stableEditSessionHash(identity+"\x00"+issueKey)
	operations := make([]RepairOperation, 0, len(material))
	for _, file := range material {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		updatedHash := strings.TrimPrefix(SourceHash([]byte(file.UpdatedContentPreview)), "sha256:")
		if updatedHash != strings.TrimSpace(file.UpdatedFingerprint) {
			return Result{}, fmt.Errorf("ontology edit preview content fingerprint does not match for %s", file.NotePath)
		}
		current, err := readOntologyEditSource(runCtx, file.NotePath)
		if err != nil {
			return Result{}, fmt.Errorf("read ontology edit source %s: %w", file.NotePath, err)
		}
		currentHash := SourceHash([]byte(current))
		if strings.TrimPrefix(currentHash, "sha256:") != strings.TrimSpace(file.CurrentFingerprint) {
			return Result{}, fmt.Errorf("ontology edit preview became stale for %s; replan before applying", file.NotePath)
		}
		operations = append(operations, RepairOperation{
			ID:        "operation:v1:" + stableEditSessionHash(identity+"\x00"+file.NotePath),
			ActionID:  action.ID,
			ActionIDs: []string{action.ID}, IssueKey: issueKey, IssueKeys: []string{issueKey},
			RequiredChecks: []string{CheckOntology}, Kind: RepairOperationWrite,
			Path: file.NotePath, SourceHash: currentHash,
			Expected:   []ExpectedText{{StartByte: 0, EndByte: len(current), Text: current, Replacement: file.UpdatedContentPreview}},
			Identities: []string{identity}, Content: []byte(file.UpdatedContentPreview),
			Lifecycle: ClassifyLifecycleEdit(LifecycleEdit{Before: []byte(current), After: []byte(file.UpdatedContentPreview)}),
		})
	}
	plan, err := FinalizeRepairPlan(RepairPlan{Actions: []FixAction{action}, Operations: operations})
	if err != nil {
		return Result{}, err
	}
	return Result{
		OK: true, SelectedChecks: []string{CheckOntology},
		Checks: []CheckResult{{Name: CheckOntology, OK: true}}, FixPlan: &plan,
	}, nil
}

func readOntologyEditSource(runCtx RunContext, notePath string) (string, error) {
	canonical, err := paths.CleanNotePath(notePath)
	if err != nil {
		return "", err
	}
	vaultPaths, err := paths.NewVaultPaths(runCtx.VaultDef.BasePath())
	if err != nil {
		return "", err
	}
	abs, err := vaultPaths.AbsNotePath(canonical)
	if err != nil {
		return "", err
	}
	content, err := os.ReadFile(abs.String())
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func commitPlanStableInput(files []ontology.FileCommitPlan) string {
	var builder strings.Builder
	for _, file := range files {
		builder.WriteString(file.NotePath)
		builder.WriteByte(0)
		builder.WriteString(file.CurrentFingerprint)
		builder.WriteByte(0)
		builder.WriteString(file.UpdatedFingerprint)
		builder.WriteByte(0)
	}
	return builder.String()
}

func stableEditSessionHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
