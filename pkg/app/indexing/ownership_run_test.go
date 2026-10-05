package indexing

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/noteownership"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/notediscovery"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestBuildOwnershipRun_ClassifiesClassicAndCollectionFilesBeforeCode(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeOwnershipFile(t, root, "notes/a.md")
	writeOwnershipFile(t, root, "docs/report.html")
	writeOwnershipFile(t, root, "src/main.go")

	classic, err := noteownership.Discover(context.Background(), noteownership.DiscoveryInput{
		VaultDefinition: obsidian.VaultDefinition{Path: root},
		Registry:        ownershipTestRegistry(t),
		CodeRoots:       []paths.AbsPath{paths.AbsPath(filepath.Join(root, "src"))},
		CodeLanguage:    ownershipTestLanguage,
	})
	require.NoError(t, err)
	require.Equal(t, []string{
		"docs/report.html|unowned||false",
		"notes/a.md|note|markdown|false",
		"src/main.go|code||go",
	}, ownershipCandidateShapes(classic.Candidates()))

	collection, err := noteownership.Discover(context.Background(), noteownership.DiscoveryInput{
		VaultDefinition: obsidian.VaultDefinition{Root: root, Includes: []string{"notes/**/*.md", "docs/*.html"}},
		Registry:        ownershipTestRegistry(t),
		CodeRoots:       []paths.AbsPath{paths.AbsPath(filepath.Join(root, "src"))},
		CodeLanguage:    ownershipTestLanguage,
	})
	require.NoError(t, err)
	require.Equal(t, []string{
		"docs/report.html|note|html|false",
		"notes/a.md|note|markdown|false",
		"src/main.go|code||go",
	}, ownershipCandidateShapes(collection.Candidates()))
	require.NotEmpty(t, collection.Candidates()[0].AbsPath)
	require.Positive(t, collection.Candidates()[0].Size)
	require.Positive(t, collection.Candidates()[0].ModTime)
	require.Equal(t, []paths.NotePath{"notes/a.md"}, markdownCandidatePaths(collection.PresentNoteCandidates()))
	require.Equal(t, []paths.RelPath{"docs/report.html", "notes/a.md"}, noteCandidatePaths(collection.PresentNoteCandidates()))
}

func TestBuildOwnershipRun_ExplicitHTMLRequiresNarrowClaimAndIgnoreWins(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeOwnershipFile(t, root, "docs/report.html")
	writeOwnershipFile(t, root, "docs/ignored.html")

	run, err := noteownership.Discover(context.Background(), noteownership.DiscoveryInput{
		VaultDefinition: obsidian.VaultDefinition{
			Root:     root,
			Includes: []string{"docs/**"},
			Excludes: []string{"docs/ignored.html"},
		},
		Registry:     ownershipTestRegistry(t),
		CodeRoots:    []paths.AbsPath{paths.AbsPath(filepath.Join(root, "docs"))},
		CodeLanguage: ownershipTestLanguage,
	})
	require.NoError(t, err)
	require.Equal(t, []string{
		"docs/ignored.html|ignored||false",
		"docs/report.html|code||html",
	}, ownershipCandidateShapes(run.Candidates()))

	run, err = noteownership.Discover(context.Background(), noteownership.DiscoveryInput{
		VaultDefinition: obsidian.VaultDefinition{Root: root, Includes: []string{"docs/*.html"}, Excludes: []string{"docs/ignored.html"}},
		Registry:        ownershipTestRegistry(t),
		CodeRoots:       []paths.AbsPath{paths.AbsPath(filepath.Join(root, "docs"))},
		CodeLanguage:    ownershipTestLanguage,
	})
	require.NoError(t, err)
	require.Equal(t, notediscovery.Note, run.Candidates()[1].Owner)
	require.Equal(t, notediscovery.Ignored, run.Candidates()[0].Owner)
}

func TestBuildOwnershipRun_HiddenPathsAreTraversalRulesNotIgnoredCandidates(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeOwnershipFile(t, root, ".hidden.md")
	writeOwnershipFile(t, root, ".private/secret.go")
	writeOwnershipFile(t, root, "visible.md")

	run, err := noteownership.Discover(context.Background(), noteownership.DiscoveryInput{
		VaultDefinition: obsidian.VaultDefinition{Path: root, Excludes: []string{"visible.md"}},
		Registry:        ownershipTestRegistry(t),
		CodeRoots:       []paths.AbsPath{paths.AbsPath(root)},
		CodeLanguage:    ownershipTestLanguage,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"visible.md|ignored||false"}, ownershipCandidateShapes(run.Candidates()))
}

func TestBuildOwnershipRun_PrunesDefaultIgnoredDirectoriesDespiteCustomIgnore(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeOwnershipFile(t, root, ".rhizome/ignore")
	writeOwnershipFile(t, root, "vendor/library.md")
	writeOwnershipFile(t, root, "node_modules/package.go")
	writeOwnershipFile(t, root, "notes/keep.md")

	run, err := noteownership.Discover(context.Background(), noteownership.DiscoveryInput{
		VaultDefinition: obsidian.VaultDefinition{Path: root},
		Registry:        ownershipTestRegistry(t),
		CodeRoots:       []paths.AbsPath{paths.AbsPath(root)},
		CodeLanguage:    ownershipTestLanguage,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"notes/keep.md|note|markdown|false"}, ownershipCandidateShapes(run.Candidates()))
}

func TestBuildOwnershipRun_DedupesOverlappingRootsAndRetiresAbsentPersistedPaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeOwnershipFile(t, root, "src/main.go")
	writeOwnershipFile(t, root, ".hidden/.keep")

	run, err := noteownership.Discover(context.Background(), noteownership.DiscoveryInput{
		VaultDefinition:    obsidian.VaultDefinition{Path: root},
		Registry:           ownershipTestRegistry(t),
		CodeRoots:          []paths.AbsPath{paths.AbsPath(root), paths.AbsPath(filepath.Join(root, "src"))},
		CodeLanguage:       ownershipTestLanguage,
		PersistedNotePaths: []paths.NotePath{"gone.md"},
		PersistedCodePaths: []paths.CodePath{"gone.go", "src/main.go", ".hidden/.keep"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{
		".hidden/.keep|unowned||false",
		"gone.go|unowned||false",
		"gone.md|unowned||false",
		"src/main.go|code||go",
	}, ownershipCandidateShapes(run.Candidates()))
	require.Len(t, run.CodeCandidates(), 1)
	require.Len(t, run.OtherCandidates(), 3)
}

func TestBuildOwnershipRunDerivesPreviousOwnersAndOnlyReturnsChanges(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeOwnershipFile(t, root, "src/main.go")
	writeOwnershipFile(t, root, "notes/new.md")
	writeOwnershipFile(t, root, "stable.md")
	writeOwnershipFile(t, root, "docs/ignored.md")

	run, err := noteownership.Discover(context.Background(), noteownership.DiscoveryInput{
		VaultDefinition: obsidian.VaultDefinition{Path: root, Excludes: []string{"docs/ignored.md"}},
		Registry:        ownershipTestRegistry(t),
		CodeRoots:       []paths.AbsPath{paths.AbsPath(filepath.Join(root, "src"))},
		CodeLanguage:    ownershipTestLanguage,
		PersistedNotePaths: []paths.NotePath{
			"src/main.go",     // note -> code
			"gone.md",         // delete -> unowned
			"stable.md",       // unchanged note
			"docs/ignored.md", // note -> ignored/unowned target
		},
		PersistedCodePaths: []paths.CodePath{"notes/new.md"}, // code -> note
	})
	require.NoError(t, err)

	changes := run.OwnershipChangeCandidates()
	require.Equal(t, []string{
		"docs/ignored.md|note|ignored",
		"gone.md|note|unowned",
		"notes/new.md|code|note",
		"src/main.go|note|code",
	}, ownershipChangeShapes(changes))
	require.Equal(t, []paths.RelPath{"docs/ignored.md", "gone.md"}, run.RetiredPaths())
}

func TestBuildOwnershipRunRejectsConflictingPersistedOwners(t *testing.T) {
	t.Parallel()

	_, err := noteownership.Discover(context.Background(), noteownership.DiscoveryInput{
		VaultDefinition:    obsidian.VaultDefinition{Path: t.TempDir()},
		Registry:           ownershipTestRegistry(t),
		PersistedNotePaths: []paths.NotePath{"same.md"},
		PersistedCodePaths: []paths.CodePath{"same.md"},
	})
	require.ErrorContains(t, err, "conflicting note and code owners")
}

func TestBuildOwnershipRunHonorsContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := noteownership.Discover(ctx, noteownership.DiscoveryInput{
		VaultDefinition: obsidian.VaultDefinition{Path: t.TempDir()},
		Registry:        ownershipTestRegistry(t),
	})
	require.ErrorIs(t, err, context.Canceled)
}

func ownershipTestRegistry(t *testing.T) noteformat.Registry {
	t.Helper()
	registry, err := noteformat.NewRegistry(
		ownershipTestProvider{descriptor: noteformat.Descriptor{
			ID:                "markdown",
			Extensions:        []string{".md"},
			ProviderVersion:   "v1",
			ProjectionVersion: "v1",
			OwnershipPolicy:   noteformat.OwnershipDefault,
		}},
		ownershipTestProvider{descriptor: noteformat.Descriptor{
			ID:                "html",
			Extensions:        []string{".html"},
			ProviderVersion:   "v1",
			ProjectionVersion: "v1",
			OwnershipPolicy:   noteformat.OwnershipExplicitInclude,
		}},
	)
	require.NoError(t, err)
	return registry
}

type ownershipTestProvider struct{ descriptor noteformat.Descriptor }

func (p ownershipTestProvider) Descriptor() noteformat.Descriptor { return p.descriptor }

func ownershipTestLanguage(ref paths.CodePathRef) codeanchor.Lang {
	switch filepath.Ext(ref.Rel.String()) {
	case ".go":
		return codeanchor.LangGo
	case ".html":
		return "html"
	default:
		return ""
	}
}

func ownershipCandidateShapes(candidates []noteownership.Candidate) []string {
	result := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		provider := string(candidate.Provider)
		language := string(candidate.Language)
		if language == "" {
			language = "false"
		}
		result = append(result, candidate.Path.String()+"|"+ownershipOwnerName(candidate.Owner)+"|"+provider+"|"+language)
	}
	return result
}

func markdownCandidatePaths(candidates []noteownership.Candidate) []paths.NotePath {
	result := make([]paths.NotePath, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Provider == noteownership.MarkdownFormatID {
			result = append(result, paths.NotePath(candidate.Path))
		}
	}
	return result
}

func noteCandidatePaths(candidates []noteownership.Candidate) []paths.RelPath {
	result := make([]paths.RelPath, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, candidate.Path)
	}
	return result
}

func ownershipChangeShapes(candidates []noteownership.Candidate) []string {
	result := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, candidate.Path.String()+"|"+ownershipOwnerName(candidate.PreviousOwner)+"|"+ownershipOwnerName(candidate.Owner))
	}
	return result
}

func ownershipOwnerName(owner notediscovery.Owner) string {
	switch owner {
	case notediscovery.Ignored:
		return "ignored"
	case notediscovery.Note:
		return "note"
	case notediscovery.Code:
		return "code"
	case notediscovery.Unowned:
		return "unowned"
	default:
		return "unknown"
	}
}

func writeOwnershipFile(t *testing.T, root, rel string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	require.NoError(t, os.WriteFile(abs, []byte("fixture"), 0o600))
}
