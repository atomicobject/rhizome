package validate

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestRunSuiteOnceSharesOneSourceSnapshotAcrossChecks(t *testing.T) {
	reader := &countingValidationReader{contents: map[string]string{
		"notes/a.md": "# A\n\n[[missing target]]\n[[notes/b.md]]\n",
		"notes/b.md": "# B\n",
	}}
	root := t.TempDir()
	// The repair planner hashes sources on disk, so mirror the reader there.
	require.NoError(t, writeFixtureFiles(root, reader.contents))
	runCtx := &RunContext{
		VaultDef:     obsidian.VaultDefinition{Name: "snapshot", Path: root, Links: obsidian.LinkTypeBoth},
		VaultPath:    root,
		NoteReader:   reader,
		NoteMetadata: testNoteMetadata(t),
	}
	// The tests selected here both enumerate and reread the entire note set.
	// The suite snapshot makes those reads in-memory after one source capture.
	result, authority, err := RunSuiteOnce(t.Context(), Options{
		Checks: []string{CheckBrokenLinks, CheckLinkHygiene}, RunContext: runCtx,
	})
	require.NoError(t, err)
	require.Len(t, result.Checks, 2)
	byName := map[string]CheckResult{}
	for _, check := range result.Checks {
		byName[check.Name] = check
	}
	require.Equal(t, []string{"missing target"}, sortedIssueTargets(byName[CheckBrokenLinks].Issues))
	require.Equal(t, []string{"wikilink_target_has_md_extension"}, issueCodes(byName[CheckLinkHygiene].Issues))
	require.Equal(t, "notes/a.md", byName[CheckLinkHygiene].Issues[0].Path)
	require.Same(t, reader, authority.NoteReader, "repair authority must retain the live reader")
	require.Equal(t, 1, reader.listCalls())
	require.Equal(t, len(reader.contents), reader.contentCalls())
}

type countingValidationReader struct {
	mu       sync.Mutex
	contents map[string]string
	lists    int
	reads    int
}

func (r *countingValidationReader) GetContents(_ obsidian.VaultDefinition, path string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reads++
	return r.contents[path], nil
}

func (r *countingValidationReader) GetNotesList(obsidian.VaultDefinition) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lists++
	paths := make([]string, 0, len(r.contents))
	for path := range r.contents {
		paths = append(paths, path)
	}
	return paths, nil
}

func (*countingValidationReader) GetModTime(obsidian.VaultDefinition, string) (time.Time, error) {
	return time.Unix(1, 0), nil
}

func (*countingValidationReader) Title(string) (string, bool) { return "", false }

func (r *countingValidationReader) listCalls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lists
}

func (r *countingValidationReader) contentCalls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reads
}

func TestBrokenLinkCandidateRankingStopsOnCancellation(t *testing.T) {
	paths := make([]string, 0, 100)
	for index := range 100 {
		paths = append(paths, "notes/semantic-code-index-spine-"+string(rune('a'+index%26))+".md")
	}
	// Cancellation lands after ranking has begun, not at the entry guard.
	ctx := &cancelAfterErrChecks{remaining: 12, done: make(chan struct{})}
	exact, fuzzy, err := newBrokenLinkMatcherIndex(paths).findCandidateTiers(ctx, "semantic code index spine current")
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, exact)
	require.Empty(t, fuzzy)
}

// cancelAfterErrChecks cancels on the Nth Err call and closes Done at the
// same moment, so it behaves like a real canceled context.
type cancelAfterErrChecks struct {
	remaining int
	done      chan struct{}
}

func (*cancelAfterErrChecks) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c *cancelAfterErrChecks) Done() <-chan struct{}     { return c.done }
func (*cancelAfterErrChecks) Value(any) any               { return nil }
func (c *cancelAfterErrChecks) Err() error {
	if c.remaining <= 0 {
		return context.Canceled
	}
	c.remaining--
	if c.remaining == 0 {
		close(c.done)
		return context.Canceled
	}
	return nil
}

func TestPreparedOntologyPostcheckCapturesOnlyAffectedSources(t *testing.T) {
	for _, tc := range []struct {
		name   string
		checks []string
		reads  int
	}{
		{"ontology", []string{CheckOntology}, 1},
		{"additional check", []string{CheckOntology, CheckLinkHygiene}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := &countingValidationReader{contents: map[string]string{
				"notes/edited.md": "# Edited\n",
				"notes/other.md":  "# Other\n",
			}}
			root := t.TempDir()
			require.NoError(t, writeFixtureFiles(root, reader.contents))
			runCtx := &RunContext{
				VaultDef:  obsidian.VaultDefinition{Name: "snapshot", Path: root, Links: obsidian.LinkTypeBoth},
				VaultPath: root, NoteReader: reader, NoteMetadata: testNoteMetadata(t),
			}
			runtime := &ontology.Runtime{Schema: &ontology.Schema{}, Issues: []ontology.ValidationIssue{
				{Code: "inverse_mismatch", NotePath: "notes/other.md", FixTarget: "notes/edited.md", Message: "dependent issue"},
			}}
			result, authority, err := RunSuiteOncePrepared(t.Context(), Options{
				Checks: tc.checks, RunContext: runCtx, postApplyJournalValidation: true,
				postcheckPaths: []string{"notes/edited.md"},
			}, runtime, nil)
			require.NoError(t, err)
			require.Same(t, reader, authority.NoteReader)
			require.Equal(t, tc.reads, reader.contentCalls())
			for _, check := range result.Checks {
				if check.Name == CheckOntology {
					require.Len(t, check.Issues, 1)
					require.Equal(t, "dependent issue", check.Issues[0].Message)
				}
			}
		})
	}
}
