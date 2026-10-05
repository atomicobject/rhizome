package validate

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildOntologyEditSessionRepairResultCreatesOneMixedSaveTransaction(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "one.md"), []byte("one\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "two.html"), []byte("<body>two</body>\n"), 0o644))
	runCtx := RunContext{VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root, NoteReader: &obsidian.Note{}}

	result, err := BuildOntologyEditSessionRepairResult(context.Background(), runCtx, "session-1", ontology.CommitPlan{Files: []ontology.FileCommitPlan{
		{NotePath: "two.html", CurrentFingerprint: hashWithoutPrefix([]byte("<body>two</body>\n")), UpdatedFingerprint: hashWithoutPrefix([]byte("<body>TWO</body>\n")), UpdatedContentPreview: "<body>TWO</body>\n", HasMaterialChange: true},
		{NotePath: "one.md", CurrentFingerprint: hashWithoutPrefix([]byte("one\n")), UpdatedFingerprint: hashWithoutPrefix([]byte("ONE\n")), UpdatedContentPreview: "ONE\n", HasMaterialChange: true},
	}})
	require.NoError(t, err)
	require.NotNil(t, result.FixPlan)
	require.Len(t, result.FixPlan.Transactions, 1, "Markdown and HTML saves from one session must commit together")
	transaction := result.FixPlan.Transactions[0]
	assert.Equal(t, []string{"one.md", "two.html"}, transaction.AffectedPaths)
	require.Len(t, result.FixPlan.Operations, 2)
	contentByPath := map[string]string{}
	for _, operation := range result.FixPlan.Operations {
		assert.Contains(t, transaction.OperationIDs, operation.ID, "every save operation must belong to the one transaction")
		contentByPath[operation.Path] = string(operation.Content)
	}
	assert.Equal(t, map[string]string{"one.md": "ONE\n", "two.html": "<body>TWO</body>\n"}, contentByPath)
	assert.Equal(t, "one\n", string(mustReadFile(t, filepath.Join(root, "one.md"))), "planning must not write")
	assert.Equal(t, "<body>two</body>\n", string(mustReadFile(t, filepath.Join(root, "two.html"))), "planning must not write")
}

func TestBuildOntologyEditSessionRepairResultRejectsStalePreview(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), []byte("changed\n"), 0o644))
	runCtx := RunContext{VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root, NoteReader: &obsidian.Note{}}

	_, err := BuildOntologyEditSessionRepairResult(context.Background(), runCtx, "session-1", ontology.CommitPlan{Files: []ontology.FileCommitPlan{{
		NotePath: "note.md", CurrentFingerprint: hashWithoutPrefix([]byte("before\n")),
		UpdatedFingerprint: hashWithoutPrefix([]byte("after\n")), UpdatedContentPreview: "after\n", HasMaterialChange: true,
	}}})
	assert.ErrorContains(t, err, "became stale")
	assert.Equal(t, "changed\n", string(mustReadFile(t, filepath.Join(root, "note.md"))))
}

func TestBuildOntologyEditSessionRepairResultRejectsForgedPreviewContentAndCancellation(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.md"), []byte("before\n"), 0o644))
	runCtx := RunContext{VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root, NoteReader: &obsidian.Note{}}
	preview := ontology.CommitPlan{Files: []ontology.FileCommitPlan{{
		NotePath: "note.md", CurrentFingerprint: hashWithoutPrefix([]byte("before\n")),
		UpdatedFingerprint: hashWithoutPrefix([]byte("different\n")), UpdatedContentPreview: "after\n", HasMaterialChange: true,
	}}}
	_, err := BuildOntologyEditSessionRepairResult(context.Background(), runCtx, "session-forged", preview)
	assert.ErrorContains(t, err, "content fingerprint does not match")

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	preview.Files[0].UpdatedFingerprint = hashWithoutPrefix([]byte("after\n"))
	_, err = BuildOntologyEditSessionRepairResult(canceled, runCtx, "session-canceled", preview)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, "before\n", string(mustReadFile(t, filepath.Join(root, "note.md"))))
}

func TestOntologyEditSessionRepairPlanPreservesModeThroughJournaledWriter(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.md")
	require.NoError(t, os.WriteFile(path, []byte("before\n"), 0o751))
	wantMode := mustRepairMode(t, path)
	runCtx := RunContext{VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root, NoteReader: &obsidian.Note{}}
	result, err := BuildOntologyEditSessionRepairResult(context.Background(), runCtx, "session-mode", ontology.CommitPlan{Files: []ontology.FileCommitPlan{{
		NotePath: "note.md", CurrentFingerprint: hashWithoutPrefix([]byte("before\n")),
		UpdatedFingerprint: hashWithoutPrefix([]byte("after\n")), UpdatedContentPreview: "after\n", HasMaterialChange: true,
	}}})
	require.NoError(t, err)

	execution, err := ApplyFixPlan(context.Background(), runCtx, result.FixPlan, Options{Fix: true, NonInteractive: true})
	require.NoError(t, err)
	require.NotNil(t, execution)
	assert.Equal(t, "after\n", string(mustReadFile(t, path)))
	assert.Equal(t, wantMode, mustRepairMode(t, path))
	assert.Empty(t, mustPendingRepairJournals(t, runCtx))
}

func TestOntologyEditSessionRepairPlanRecoversCommittedJournal(t *testing.T) {
	root := t.TempDir()
	markdownPath := filepath.Join(root, "note.md")
	htmlPath := filepath.Join(root, "report.html")
	require.NoError(t, os.WriteFile(markdownPath, []byte("before\n"), 0o640))
	markdownMode := mustRepairMode(t, markdownPath)
	require.NoError(t, os.WriteFile(htmlPath, []byte("<body>before</body>\n"), 0o750))
	htmlMode := mustRepairMode(t, htmlPath)
	runCtx := repairRunContext(t, root)
	result, err := BuildOntologyEditSessionRepairResult(context.Background(), runCtx, "session-recovery", ontology.CommitPlan{Files: []ontology.FileCommitPlan{
		{NotePath: "note.md", CurrentFingerprint: hashWithoutPrefix([]byte("before\n")), UpdatedFingerprint: hashWithoutPrefix([]byte("after\n")), UpdatedContentPreview: "after\n", HasMaterialChange: true},
		{NotePath: "report.html", CurrentFingerprint: hashWithoutPrefix([]byte("<body>before</body>\n")), UpdatedFingerprint: hashWithoutPrefix([]byte("<body>after</body>\n")), UpdatedContentPreview: "<body>after</body>\n", HasMaterialChange: true},
	}})
	require.NoError(t, err)
	refresher := &failOncePostApplyRefresher{}

	_, err = ApplyFixPlan(context.Background(), runCtx, result.FixPlan, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: refresher,
	})
	require.ErrorContains(t, err, "injected post-apply interruption")
	assert.Equal(t, "after\n", string(mustReadFile(t, markdownPath)))
	assert.Equal(t, "<body>after</body>\n", string(mustReadFile(t, htmlPath)))
	require.NotEmpty(t, mustPendingRepairJournals(t, runCtx))

	_, err = ApplyFixPlan(context.Background(), runCtx, result.FixPlan, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: refresher,
	})
	require.NoError(t, err)
	assert.Empty(t, mustPendingRepairJournals(t, runCtx))
	assert.Equal(t, markdownMode, mustRepairMode(t, markdownPath))
	assert.Equal(t, htmlMode, mustRepairMode(t, htmlPath))
}

// A save postchecks the notes it wrote; whole-vault validation follows in the
// background instead of holding the index lock inside the save.
func TestApplyRepairSessionPostchecksAnEditSessionOnlyWhereItWrote(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "edited.md"), []byte("before\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "other.md"), []byte("other\n"), 0o644))
	runCtx := repairRunContext(t, root)
	planned, err := BuildOntologyEditSessionRepairResult(context.Background(), runCtx, "session-scope", ontology.CommitPlan{Files: []ontology.FileCommitPlan{{
		NotePath: "edited.md", CurrentFingerprint: hashWithoutPrefix([]byte("before\n")),
		UpdatedFingerprint: hashWithoutPrefix([]byte("after\n")), UpdatedContentPreview: "after\n", HasMaterialChange: true,
	}}})
	require.NoError(t, err)
	runtime := &ontology.Runtime{Schema: &ontology.Schema{}, Issues: []ontology.ValidationIssue{
		{Code: "missing_required_field", NotePath: "edited.md", Message: "edited note issue"},
		{Code: "missing_required_field", NotePath: "other.md", Message: "unrelated note issue"},
		{Code: "inverse_mismatch", NotePath: "other.md", FixTarget: "edited.md", Message: "issue fixed in the edited note"},
	}}

	postcheck, execution, err := ApplyRepairSession(context.Background(), planned, runCtx, Options{
		Fix: true, NonInteractive: true,
		PostApplyRefresher: &repairPostApplyProbe{t: t, result: NewPostApplyRefreshResult(runtime, repairNoopCloseOwner{})},
	})
	require.NoError(t, err)
	require.Equal(t, 1, execution.AppliedTransactions)
	require.Len(t, postcheck.Checks, 1)
	messages := []string{}
	for _, issue := range postcheck.Checks[0].Issues {
		messages = append(messages, issue.Message)
	}
	assert.ElementsMatch(t, []string{"edited note issue", "issue fixed in the edited note"}, messages)
	assert.Equal(t, "after\n", string(mustReadFile(t, filepath.Join(root, "edited.md"))))
}

func mustPendingRepairJournals(t *testing.T, runCtx RunContext) []RepairJournalEvidence {
	t.Helper()
	journals, err := DetectPendingRepairJournals(runCtx)
	require.NoError(t, err)
	return journals
}

func hashWithoutPrefix(content []byte) string {
	return SourceHash(content)[len("sha256:"):]
}
