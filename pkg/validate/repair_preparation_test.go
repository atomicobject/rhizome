package validate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestReadValidationDefersRepairSourceReads(t *testing.T) {
	runCtx, reader := repairPreparationFixture(t)
	scanned, authority, err := RunSuiteOncePrepared(t.Context(), ScanOptions{
		Checks: []string{CheckLinkHygiene}, RunContext: &runCtx,
	}, nil, nil)
	require.NoError(t, err)
	require.EqualValues(t, 2, reader.reads.Load(), "read validation reads each real source once")
	require.EqualValues(t, 1, reader.lists.Load(), "read validation captures one source inventory")
	require.Equal(t, 1, scanned.IssueCount)
	require.NotNil(t, scanned.FixPlan)
	require.True(t, scanned.FixPlan.PreparationRequired)
	require.Equal(t, 1, scanned.FixPlan.SafeCount)
	require.Empty(t, scanned.FixPlan.Operations)
	require.Empty(t, scanned.FixPlan.Transactions)
	require.Equal(t, 1, scanned.NextActions.SafeFixCount)

	prepared, err := PrepareRepair(t.Context(), scanned, authority)
	require.NoError(t, err)
	require.Greater(t, reader.reads.Load(), int64(2), "explicit repair validates current source authority")
	require.False(t, prepared.FixPlan.PreparationRequired)
	require.Len(t, prepared.FixPlan.Operations, 1)
	require.Equal(t, "source.md", prepared.FixPlan.Operations[0].Path)
	require.Equal(t, "See [[target]].\n", string(prepared.FixPlan.Operations[0].Content))
	require.Equal(t, SourceHash([]byte("See [[target.md]].\n")), prepared.FixPlan.Operations[0].SourceHash)
	require.Equal(t, resultIssueKeys(scanned), resultIssueKeys(prepared))
	require.Equal(t, scanned.FixPlan.Actions[0].ID, prepared.FixPlan.Actions[0].ID)
	require.Equal(t, scanned.FixPlan.Actions[0].Title, prepared.FixPlan.Actions[0].Title)
	require.Equal(t, scanned.FixPlan.Actions[0].Summary, prepared.FixPlan.Actions[0].Summary)
	require.Equal(t, scanned.FixPlan.Actions[0].IssueKeys, prepared.FixPlan.Actions[0].IssueKeys)
	require.Equal(t, scanned.NextActions.SafeFixCount, prepared.NextActions.SafeFixCount)
	require.NotEqual(t, scanned.FixPlan.Fingerprint, prepared.FixPlan.Fingerprint)
	t.Logf("real source reads: read validation=%d; after explicit repair=%d", 2, reader.reads.Load())
}

func TestRepairPreparationRejectsChangesSinceReadValidation(t *testing.T) {
	for _, change := range []struct {
		name    string
		path    string
		content string
	}{
		{"edited source", "source.md", "User changed [[target.md]].\n"},
		{"edited target aliases", "target.md", "---\naliases: [New target]\n---\n# Target\n"},
		{"added source", "new.md", "# New\n"},
		{"deleted source", "source.md", ""},
	} {
		t.Run(change.name, func(t *testing.T) {
			runCtx, _ := repairPreparationFixture(t)
			scanned, authority, err := RunSuiteOncePrepared(t.Context(), ScanOptions{
				Checks: []string{CheckLinkHygiene}, RunContext: &runCtx,
			}, nil, nil)
			require.NoError(t, err)
			if change.content == "" {
				require.NoError(t, os.Rename(filepath.Join(runCtx.VaultPath, change.path), filepath.Join(t.TempDir(), "removed.md")))
			} else {
				require.NoError(t, os.WriteFile(filepath.Join(runCtx.VaultPath, change.path), []byte(change.content), 0o644))
			}
			_, err = PrepareRepair(t.Context(), scanned, authority)
			require.ErrorContains(t, err, "changed after validation")
			if change.content == "" {
				return
			}
			content, readErr := os.ReadFile(filepath.Join(runCtx.VaultPath, change.path))
			require.NoError(t, readErr)
			require.Equal(t, change.content, string(content))
		})
	}
}

func TestRepairPreparationRejectsMissingOrChangedAuthority(t *testing.T) {
	runCtx, _ := repairPreparationFixture(t)
	scanned, authority, err := RunSuiteOncePrepared(t.Context(), ScanOptions{
		Checks: []string{CheckLinkHygiene}, RunContext: &runCtx,
	}, nil, nil)
	require.NoError(t, err)
	encoded, err := json.Marshal(scanned)
	require.NoError(t, err)
	var decoded Result
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	_, err = PrepareRepair(t.Context(), decoded, authority)
	require.ErrorContains(t, err, "in-process validation authority")

	other := authority
	other.VaultPath = t.TempDir()
	other.VaultDef.Path = other.VaultPath
	_, err = PrepareRepair(t.Context(), scanned, other)
	require.ErrorContains(t, err, "differs from captured validation authority")

	scanned.FixPlan.Actions[0].Edits[0].OldTarget = "another.md"
	_, err = PrepareRepair(t.Context(), scanned, authority)
	require.ErrorContains(t, err, "action summary changed")
}

func repairPreparationFixture(t *testing.T) (RunContext, *repairSourceCountingReader) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		"source.md": "See [[target.md]].\n", "target.md": "# Target\n",
	}))
	reader := &repairSourceCountingReader{NoteReader: &obsidian.Note{}}
	return RunContext{
		VaultDef:  obsidian.VaultDefinition{Name: "test", Path: root, Links: obsidian.LinkTypeBoth},
		VaultPath: root, NoteReader: reader, NoteMetadata: testNoteMetadata(t),
	}, reader
}

type repairSourceCountingReader struct {
	obsidian.NoteReader
	reads atomic.Int64
	lists atomic.Int64
}

func (r *repairSourceCountingReader) GetContents(def obsidian.VaultDefinition, path string) (string, error) {
	r.reads.Add(1)
	return r.NoteReader.GetContents(def, path)
}

func (r *repairSourceCountingReader) GetContentsContext(ctx context.Context, def obsidian.VaultDefinition, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return r.GetContents(def, path)
}

func (r *repairSourceCountingReader) GetNotesList(def obsidian.VaultDefinition) ([]string, error) {
	r.lists.Add(1)
	return r.NoteReader.GetNotesList(def)
}

func TestReadSummaryCannotEnterTransactionalApply(t *testing.T) {
	runCtx, reader := repairPreparationFixture(t)
	scanned, authority, err := RunSuiteOncePrepared(t.Context(), ScanOptions{
		Checks: []string{CheckLinkHygiene}, RunContext: &runCtx,
	}, nil, nil)
	require.NoError(t, err)
	_, err = ApplyFixPlan(t.Context(), authority, scanned.FixPlan, ApplyOptions{Fix: true, NonInteractive: true})
	require.ErrorContains(t, err, "requires explicit repair preparation")
	require.EqualValues(t, 2, reader.reads.Load(), "apply must reject before implicit source preparation")
	require.Equal(t, "See [[target.md]].\n", string(mustReadFile(t, filepath.Join(runCtx.VaultPath, "source.md"))))
}

func TestExplicitPreparedRepairRetainsApplySourcePreconditions(t *testing.T) {
	runCtx, _ := repairPreparationFixture(t)
	scanned, authority, err := RunSuiteOncePrepared(t.Context(), ScanOptions{
		Checks: []string{CheckLinkHygiene}, RunContext: &runCtx,
	}, nil, nil)
	require.NoError(t, err)
	prepared, err := PrepareRepair(t.Context(), scanned, authority)
	require.NoError(t, err)
	userEdit := "User changed [[target.md]].\n"
	path := filepath.Join(runCtx.VaultPath, "source.md")
	require.NoError(t, os.WriteFile(path, []byte(userEdit), 0o644))
	execution, err := ApplyFixPlan(t.Context(), authority, prepared.FixPlan, ApplyOptions{Fix: true, NonInteractive: true})
	require.ErrorContains(t, err, "stale")
	require.Zero(t, execution.AppliedTransactions)
	require.Equal(t, userEdit, string(mustReadFile(t, path)))
}

func TestRepairPreparationRejectsSchemaChangesSinceReadValidation(t *testing.T) {
	root, runtime := identifierCheckRuntime(t, `
type Example @node(paths: ["notes/*.md"]) { tags: [String!]! }
`, map[string]string{"notes/one.md": "---\ntype: Example\n---\n# One\n"})
	runCtx := RunContext{
		VaultDef: obsidian.VaultDefinition{Path: root}, VaultPath: root,
		NoteReader: &obsidian.Note{}, NoteMetadata: testNoteMetadata(t),
	}
	scanned, authority, err := RunSuiteOncePrepared(t.Context(), ScanOptions{
		Checks: []string{CheckOntology}, RunContext: &runCtx,
	}, runtime, nil)
	require.NoError(t, err)
	require.NotNil(t, scanned.FixPlan)
	require.NoError(t, os.WriteFile(filepath.Join(root, ".rhizome", "ontology", "schema.graphql"), []byte(`
type Example @node(paths: ["notes/*.md"]) { tags: [String!]!, description: String }
`), 0o644))
	_, err = PrepareRepair(t.Context(), scanned, authority)
	require.ErrorContains(t, err, "ontology schema changed after validation")
}
