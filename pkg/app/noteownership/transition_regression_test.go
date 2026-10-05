package noteownership

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	"github.com/atomicobject/rhizome/pkg/noteformat/markdown"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/notediscovery"
	"github.com/stretchr/testify/require"
)

func TestTransitionPlan_DescriptorAndFreshOwnershipCases(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	runtime := transitionRuntime(t)
	html := transitionCandidate(t, root, "notes/Release.HTML", "<p>x</p>", notediscovery.Unowned, notediscovery.Note, "html")
	plan := transitionPlan(t, root, runtime, []Candidate{html}, 71, nil)
	state := plan.Transitions()[0].Note
	require.Equal(t, "Release", state.Title)
	require.Equal(t, "html", state.FormatID)
	require.Equal(t, semdb.NoteProjectionStatusStale, state.Status)
	require.NotEmpty(t, state.ContentHash)
	_, ok := plan.Source("notes/Release.HTML")
	require.False(t, ok)
	html.PreviousOwner = notediscovery.Note
	require.Len(t, transitionPlan(t, root, runtime, []Candidate{html}, 72, nil).Transitions(), 1, "descriptor roots refresh unchanged ownership")
	markdown := transitionCandidate(t, root, "new.md", "# new", notediscovery.Unowned, notediscovery.Note, "markdown")
	code := transitionCandidate(t, root, "new.go", "package p", notediscovery.Unowned, notediscovery.Code, "")
	require.Empty(t, transitionPlan(t, root, runtime, []Candidate{markdown, code, {Path: "x.html", Owner: notediscovery.Unowned}}, 73, nil).Transitions())
}

func TestTransitionPlan_SealsCodeToMarkdownAndRetiresFormerOwners(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	runtime := transitionRuntime(t)
	markdown := transitionCandidate(t, root, "notes/Plan.MD", "---\ntitle: ignored\n---\n# Body", notediscovery.Code, notediscovery.Note, "markdown")
	code := transitionCandidate(t, root, "src/reclaimed.go", "package p", notediscovery.Note, notediscovery.Code, "")
	ignored := transitionCandidate(t, root, "notes/ignored.md", "# ignored", notediscovery.Note, notediscovery.Ignored, "")
	plan := transitionPlan(t, root, runtime, []Candidate{markdown, code, ignored, {Path: "notes/gone.md", PreviousOwner: notediscovery.Note, Owner: notediscovery.Unowned}}, 74, nil)
	require.Equal(t, []string{"notes/Plan.MD|note", "notes/gone.md|unowned", "notes/ignored.md|unowned", "src/reclaimed.go|code"}, transitionShapes(plan.Transitions()))
	source, ok := plan.Source("notes/Plan.MD")
	require.True(t, ok)
	require.Equal(t, "---\ntitle: ignored\n---\n# Body", string(source.Bytes()))
}

func TestTransitionPlan_SourceReadFailuresAndStability(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	runtime := transitionRuntime(t)
	missing := Candidate{Path: "notes/missing.HTML", PreviousOwner: notediscovery.Note, Owner: notediscovery.Note, Provider: "html", Present: true, AbsPath: paths.AbsPath(filepath.Join(root, "notes/missing.HTML")), ModTime: 20, Size: 30}
	require.Equal(t, semdb.NoteProjectionStatusFatal, transitionPlan(t, root, runtime, []Candidate{missing}, 75, nil).Transitions()[0].Note.Status)

	// An affected, already note-owned Markdown source that cannot be read is fatal and sealed without source.
	currentMissing := Candidate{Path: "notes/missing.md", PreviousOwner: notediscovery.Note, Owner: notediscovery.Note, Provider: MarkdownFormatID, Present: true, AbsPath: paths.AbsPath(filepath.Join(root, "notes", "missing.md")), ModTime: 1, Size: 1}
	snapshot, err := newSnapshot([]Candidate{currentMissing})
	require.NoError(t, err)
	plan, err := BuildTransitionPlan(context.Background(), TransitionInput{VaultPaths: transitionVault(t, root), Snapshot: snapshot, Runtime: runtime, ObservedAt: 7, AffectedPaths: map[string]struct{}{"notes/missing.md": {}}})
	require.NoError(t, err)
	require.Len(t, plan.Transitions(), 1)
	require.Equal(t, semdb.NoteProjectionStatusFatal, plan.Transitions()[0].Note.Status)
	require.Equal(t, UnreadableSourceDiagnosticCode, plan.Transitions()[0].Note.DiagnosticCode)
	_, ok := plan.Source("notes/missing.md")
	require.False(t, ok)

	candidate := transitionCandidate(t, root, "notes/current.html", "old", notediscovery.Note, notediscovery.Note, "html")
	stable := time.Unix(123, 0)
	memoryFS := &transitionFS{stats: []fs.FileInfo{transitionInfo{stable, 10}, transitionInfo{stable, 10}}, reads: [][]byte{[]byte("new source")}}
	state := transitionPlan(t, root, runtime, []Candidate{candidate}, 76, memoryFS).Transitions()[0].Note
	require.Equal(t, stable.Unix(), state.Mtime)
	require.Equal(t, int64(10), state.Size)
	require.Equal(t, "eb326958738d171e78bdff8117386308557c0f4d19783441bca3dea03314d2bc", state.ContentHash, "sha256 of the accepted read")

	// A read whose stat changes is retried; only the second, stable read is accepted.
	first, second, final := time.Unix(1, 0), time.Unix(2, 0), time.Unix(3, 0)
	memoryFS = &transitionFS{stats: []fs.FileInfo{transitionInfo{first, 5}, transitionInfo{second, 5}, transitionInfo{final, 4}, transitionInfo{final, 4}}, reads: [][]byte{[]byte("first"), []byte("next")}}
	state = transitionPlan(t, root, runtime, []Candidate{candidate}, 77, memoryFS).Transitions()[0].Note
	require.Equal(t, semdb.NoteProjectionStatusStale, state.Status)
	require.Equal(t, final.Unix(), state.Mtime)
	require.Equal(t, int64(4), state.Size)
	require.Equal(t, "c6c1c9a9c8543f1e4cd980064cf1625eeb61a90703b2464fff039f21682508b3", state.ContentHash, "sha256 of the retried read")

	memoryFS = &transitionFS{stats: []fs.FileInfo{transitionInfo{first, 5}, transitionInfo{second, 5}, transitionInfo{final, 5}, transitionInfo{time.Unix(4, 0), 5}}, reads: [][]byte{[]byte("first"), []byte("again")}}
	state = transitionPlan(t, root, runtime, []Candidate{candidate}, 78, memoryFS).Transitions()[0].Note
	require.Equal(t, semdb.NoteProjectionStatusFatal, state.Status)
	require.Equal(t, UnstableSourceDiagnosticCode, state.DiagnosticCode)
	require.Empty(t, state.ContentHash)
	require.Equal(t, int64(4), state.Mtime)
}

func TestTransitionPlan_RejectsIdentityAndProviderMismatch(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	runtime := transitionRuntime(t)
	valid := transitionCandidate(t, root, "notes/source.md", "# source", notediscovery.Code, notediscovery.Note, "markdown")
	build := func(candidate Candidate) error {
		snapshot, err := newSnapshot([]Candidate{candidate})
		require.NoError(t, err)
		_, err = BuildTransitionPlan(context.Background(), TransitionInput{VaultPaths: transitionVault(t, root), Snapshot: snapshot, Runtime: runtime, ObservedAt: 79})
		return err
	}
	require.NoError(t, build(valid))
	providerMismatch := valid
	providerMismatch.Provider = "html"
	require.EqualError(t, build(providerMismatch), `selected provider "html" does not claim path "notes/source.md"`)
	wrong := valid
	wrong.Path = "notes/claimed.md"
	require.ErrorContains(t, build(wrong), "resolves to")
}

func TestTransitionPlan_DeterministicWithoutProjectionAndCopiesResults(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	runtime, calls := countingRuntime(t)
	markdown := transitionCandidate(t, root, "z/last.md", "# last", notediscovery.Code, notediscovery.Note, "markdown")
	html := transitionCandidate(t, root, "a/Release.Candidate.html", "<p>first</p>", notediscovery.Note, notediscovery.Note, "html")
	code := transitionCandidate(t, root, "m/middle.go", "package p", notediscovery.Note, notediscovery.Code, "")
	current := transitionCandidate(t, root, "b/current.md", "# current", notediscovery.Note, notediscovery.Note, "markdown")
	candidates := []Candidate{markdown, current, code, html}
	plan := transitionPlan(t, root, runtime, candidates, 80, nil)
	// Unchanged current Markdown is not observed unless its path is affected.
	require.Equal(t, []string{"a/Release.Candidate.html|note", "m/middle.go|code", "z/last.md|note"}, transitionShapes(plan.Transitions()))
	_, ok := plan.Source("b/current.md")
	require.False(t, ok)
	require.Zero(t, *calls)
	transitions := plan.Transitions()
	transitions[0].Path = "changed"
	transitions[0].Note.Title = "changed"
	require.Equal(t, "a/Release.Candidate.html", plan.Transitions()[0].Path)
	require.Equal(t, "Release.Candidate", plan.Transitions()[0].Note.Title, "fallback title removes only the final extension")
	source, ok := plan.Source("z/last.md")
	require.True(t, ok)
	bytes := source.Bytes()
	bytes[0] = 'X'
	fresh, _ := plan.Source("z/last.md")
	require.Equal(t, byte('#'), fresh.Bytes()[0])

	snapshot, err := newSnapshot(candidates)
	require.NoError(t, err)
	affectedPlan, err := BuildTransitionPlan(context.Background(), TransitionInput{VaultPaths: transitionVault(t, root), Snapshot: snapshot, Runtime: runtime, ObservedAt: 81, AffectedPaths: map[string]struct{}{"b/current.md": {}}})
	require.NoError(t, err)
	require.Equal(t, []string{"a/Release.Candidate.html|note", "b/current.md|note", "m/middle.go|code", "z/last.md|note"}, transitionShapes(affectedPlan.Transitions()))
	currentSource, ok := affectedPlan.Source("b/current.md")
	require.True(t, ok)
	require.Equal(t, "# current", string(currentSource.Bytes()))
	require.Zero(t, *calls)
}

func transitionPlan(t *testing.T, root string, runtime noteformat.Runtime, candidates []Candidate, observed int64, filesystem filesystem) TransitionPlan {
	t.Helper()
	snapshot, err := newSnapshot(candidates)
	require.NoError(t, err)
	input := TransitionInput{VaultPaths: transitionVault(t, root), Snapshot: snapshot, Runtime: runtime, ObservedAt: observed}
	if filesystem != nil {
		plan, err := buildTransitionPlanWithFilesystem(context.Background(), input, filesystem)
		require.NoError(t, err)
		return plan
	}
	plan, err := BuildTransitionPlan(context.Background(), input)
	require.NoError(t, err)
	return plan
}
func transitionVault(t *testing.T, root string) paths.VaultPaths {
	t.Helper()
	vault, err := paths.NewVaultPaths(root)
	require.NoError(t, err)
	return vault
}
func transitionRuntime(t *testing.T) noteformat.Runtime {
	t.Helper()
	registry, err := builtin.NewRegistry()
	require.NoError(t, err)
	runtime, err := noteformat.NewRuntime(registry, markdown.New())
	require.NoError(t, err)
	return runtime
}
func transitionCandidate(t *testing.T, root, rel, content string, previous, owner notediscovery.Owner, provider noteformat.FormatID) Candidate {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	require.NoError(t, os.WriteFile(abs, []byte(content), 0o600))
	info, err := os.Stat(abs)
	require.NoError(t, err)
	return Candidate{Path: paths.RelPath(rel), PreviousOwner: previous, Owner: owner, Provider: provider, Present: true, AbsPath: paths.AbsPath(abs), ModTime: info.ModTime().Unix(), Size: info.Size()}
}
func transitionShapes(transitions []semdb.OwnershipTransition) []string {
	result := make([]string, 0, len(transitions))
	for _, transition := range transitions {
		result = append(result, transition.Path+"|"+string(transition.Target))
	}
	return result
}

type transitionFS struct {
	stats                []fs.FileInfo
	reads                [][]byte
	statCalls, readCalls int
}

func (f *transitionFS) Stat(string) (fs.FileInfo, error) {
	if f.statCalls >= len(f.stats) {
		return nil, os.ErrNotExist
	}
	value := f.stats[f.statCalls]
	f.statCalls++
	return value, nil
}
func (f *transitionFS) ReadFile(string) ([]byte, error) {
	if f.readCalls >= len(f.reads) {
		return nil, os.ErrNotExist
	}
	value := append([]byte(nil), f.reads[f.readCalls]...)
	f.readCalls++
	return value, nil
}

type transitionInfo struct {
	mtime time.Time
	size  int64
}

func (transitionInfo) Name() string         { return "source" }
func (i transitionInfo) Size() int64        { return i.size }
func (transitionInfo) Mode() fs.FileMode    { return 0 }
func (i transitionInfo) ModTime() time.Time { return i.mtime }
func (transitionInfo) IsDir() bool          { return false }
func (transitionInfo) Sys() any             { return nil }

type countingProjector struct{ calls *int }

func (p countingProjector) Descriptor() noteformat.Descriptor { return markdown.New().Descriptor() }
func (p countingProjector) Project(noteformat.AuthoredSource) (noteformat.Projection, error) {
	*p.calls++
	return noteformat.Projection{}, nil
}
func countingRuntime(t *testing.T) (noteformat.Runtime, *int) {
	t.Helper()
	registry, err := builtin.NewRegistry()
	require.NoError(t, err)
	calls := new(int)
	runtime, err := noteformat.NewRuntime(registry, countingProjector{calls})
	require.NoError(t, err)
	return runtime, calls
}
