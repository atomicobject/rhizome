package operations

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/noteownership"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/paths"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/pkg/vault/notediscovery"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

// configuredPathKind is the configured, exclusive identity of a vault path.
// It is independent of filename suffix inference so an explicitly owned,
// descriptor-only note never enters the code lane.
type configuredPathKind struct {
	Owner       notediscovery.Owner
	Provider    string
	Projectable bool
}

func classifyConfiguredPath(vaultDef obsidian.VaultDefinition, absPath string, indexer notemeta.Indexer) (configuredPathKind, error) {
	root := vaultDef.BasePath()
	if root == "" {
		return configuredPathKind{}, fmt.Errorf("vault root is required")
	}
	vaultPaths, err := paths.NewVaultPaths(root)
	if err != nil {
		return configuredPathKind{}, err
	}
	rel, err := vaultPaths.RelStrict(absPath)
	if err != nil {
		return configuredPathKind{}, err
	}
	runtime, err := indexer.FormatRuntime()
	if err != nil {
		return configuredPathKind{}, err
	}
	selector, err := noteownership.CompileSelector(noteownership.SelectorInput{
		VaultDefinition: vaultDef,
		Registry:        runtime.Registry(),
		CodeRoots:       []paths.AbsPath{paths.AbsPath(vaultPaths.Root())},
		CodeLanguage: func(ref paths.CodePathRef) codeanchor.Lang {
			return codeanchor.Lang(coderefs.DetectLanguage(ref.Rel.String()))
		},
	})
	if err != nil {
		return configuredPathKind{}, err
	}
	selection, err := selector.Select(rel)
	if err != nil {
		return configuredPathKind{}, err
	}
	provider := string(selection.Provider)
	return configuredPathKind{
		Owner:       selection.Owner,
		Provider:    provider,
		Projectable: provider != "" && runtime.CanProject(selection.Provider),
	}, nil
}

func unsupportedProjectionMessage(path, provider, operation string) string {
	return fmt.Sprintf("%s is a note owned by format %q, but %s projection is not supported", path, provider, operation)
}

// Docs:
// - [Vault cache service (Service)](docs/reference/analysis/Vault cache service (Service).md)

// FileContext describes context for a single file (note or code).
type FileContext struct {
	Path          string
	FileType      string // "note" or "code"
	IsDir         bool
	LinkedNotes   []LinkedNoteContext
	AncestorDocs  []AncestorDocContext
	SubmoduleDocs []SubmoduleDocContext
	Truncated     bool
	AnchorMatches []codeanchor.AnchorMatchTrace
	// ReturnedNotePaths and ReturnedDocPaths list the items actually emitted
	// after filtering/exclusion. Helpful for stateless deduping in callers.
	ReturnedNotePaths []string
	ReturnedDocPaths  []string
	Rationale         []codeanchor.Rationale
	OntologyNodes     []OntologyNodeContext
	NodeDiagnostics   []noderead.NodeResolveDiagnostic
}

// SubmoduleDocContext represents a discovered CONTEXT.md in a subdirectory.
type SubmoduleDocContext struct {
	Path      string
	Depth     int
	Content   string
	Truncated bool
}

// LinkedNoteContext represents a note referenced from a code file.
type LinkedNoteContext struct {
	Path        string
	Fragment    string
	RawTarget   string
	Title       string
	Kind        string
	Line        int
	Snippet     string
	Frontmatter map[string]interface{}
}

type OntologyNodeContext struct {
	Input         string
	SourcePath    string
	Line          int
	Snippet       string
	Ref           ontology.NodeRef
	TypeName      string
	Title         string
	SourceLocator string
	Wikilink      string
	Markdown      string
	Status        ontology.NodeLocatorStatus
	Content       string
	RequiresFix   bool
}

// AncestorDocContext represents a discovered CONTEXT.md (or configured) file.
type AncestorDocContext struct {
	Dir       string
	Pattern   string
	Path      string
	Content   string
	Truncated bool
}

// FileContextParams controls file_context behavior.
type FileContextParams struct {
	Context              context.Context
	IndexedReadOnly      bool
	VaultDef             obsidian.VaultDefinition
	ProjectRoot          string
	CodeRefRoot          string
	DocPatterns          []string
	MaxEmptyLevels       int
	ContextBudget        int
	ExpandNoteLinks      []string
	ExpandNoteLinksLimit int
	CodeRefsByFile       map[string][]coderefs.CodeRef
	NotePathCache        *obsidian.NotePathCache
	NoteReader           obsidian.NoteReader
	CodeAnchor           *codeanchor.Service
	SkipAnchorRefresh    bool
	SessionStore         *semdb.Store
	NodeReadScope        *noderead.Scope
	EnsureLinkTarget     ontology.EnsureLinkTargetMode
	// AnchorKinds limits which code anchor kinds are included when non-empty.
	AnchorKinds []codeanchor.AnchorKind
	// SubmoduleDepth controls how deep to search for module docs in subdirectories.
	// Only applies when target is a directory. 0 = disabled (default).
	SubmoduleDepth int
	// ExcludeNotePaths and ExcludeDocPaths allow callers to drop items they've
	// already returned in previous requests.
	ExcludeNotePaths []string
	ExcludeDocPaths  []string
}

// BuildFileContext builds code-file context (ancestor docs + linked notes).
// Caller is responsible for providing sensible defaults in params.
func BuildFileContext(target string, params FileContextParams) (FileContext, error) {
	ctx := params.Context
	if ctx == nil {
		ctx = context.Background()
	}
	absPath := paths.ResolveSymlinks(target).String()
	if absPath == "" {
		absPath = target
	}
	isDir := false
	if info, statErr := os.Stat(absPath); statErr == nil {
		isDir = info.IsDir()
	}
	fileCtx := FileContext{
		Path:     target,
		FileType: "code",
		IsDir:    isDir,
	}

	vaultRoot := params.VaultDef.BasePath()
	vaultPaths, _ := paths.NewVaultPaths(vaultRoot)

	excludeNotes := toRelSet(params.ExcludeNotePaths, vaultPaths, true)
	excludeDocs := toRelSet(params.ExcludeDocPaths, vaultPaths, false)

	root := params.ProjectRoot
	if root == "" {
		root = vaultRoot
	}
	if root == "" {
		root = filepath.Dir(absPath)
	}
	root = paths.ResolveSymlinks(root).String()
	rootPaths, _ := paths.NewVaultPaths(root)

	codeRefRoot := params.CodeRefRoot
	if codeRefRoot == "" {
		codeRefRoot = root
	}
	codeRefRoot = paths.ResolveSymlinks(codeRefRoot).String()
	codeRefPaths, _ := paths.NewVaultPaths(codeRefRoot)

	docPatterns := obsidian.NormalizeDocPatterns(params.DocPatterns)
	maxEmpty := params.MaxEmptyLevels
	if maxEmpty <= 0 {
		maxEmpty = repoconfig.FileContextConfigDefaults.MaxEmptyLevels
	}
	budget := params.ContextBudget
	if budget <= 0 {
		budget = repoconfig.FileContextConfigDefaults.ContextBudget
	}
	expandExpr := params.ExpandNoteLinks
	if len(expandExpr) == 0 {
		expandExpr = repoconfig.FileContextConfigDefaults.ExpandNoteLinks
	}
	expandLimit := params.ExpandNoteLinksLimit
	if expandLimit <= 0 {
		expandLimit = repoconfig.FileContextConfigDefaults.ExpandNoteLinksLimit
	}

	docStart := absPath
	if isDir {
		// collectAncestorDocs starts at filepath.Dir(absFilePath); use a fake filename so Dir() is the directory itself.
		docStart = filepath.Join(absPath, "__dir__")
	}
	ancestorDocs, truncated := collectAncestorDocs(docStart, rootPaths, docPatterns, maxEmpty, budget)
	if len(excludeDocs) > 0 && len(ancestorDocs) > 0 {
		filtered := ancestorDocs[:0]
		for _, d := range ancestorDocs {
			if _, skip := excludeDocs[d.Path]; skip {
				continue
			}
			filtered = append(filtered, d)
		}
		ancestorDocs = filtered
	}
	fileCtx.AncestorDocs = ancestorDocs
	fileCtx.Truncated = truncated

	// Collect submodule docs if target is a directory and SubmoduleDepth > 0
	if isDir && params.SubmoduleDepth > 0 {
		submoduleDocs, subTruncated := collectSubmoduleDocs(absPath, rootPaths, docPatterns, params.SubmoduleDepth, SubmoduleDocsBudget)
		if len(excludeDocs) > 0 && len(submoduleDocs) > 0 {
			filtered := submoduleDocs[:0]
			for _, d := range submoduleDocs {
				if _, skip := excludeDocs[d.Path]; skip {
					continue
				}
				filtered = append(filtered, d)
			}
			submoduleDocs = filtered
		}
		fileCtx.SubmoduleDocs = submoduleDocs
		if subTruncated {
			fileCtx.Truncated = true
		}
	}

	indexedCoderefBranch := !isDir && params.IndexedReadOnly
	indexedNoteCacheBranch := params.IndexedReadOnly
	noteCache := params.NotePathCache
	if noteCache == nil && params.NoteReader != nil && !params.IndexedReadOnly {
		if notes, err := params.NoteReader.GetNotesList(params.VaultDef); err == nil {
			var aliasesByPath map[string][]string
			if provider, ok := params.NoteReader.(obsidian.NoteEntriesProvider); ok {
				if entries, snapErr := provider.NoteEntriesSnapshot(ctx); snapErr == nil {
					aliasesByPath = obsidian.AliasesFromNoteEntries(entries)
				}
			}
			noteCache = obsidian.BuildNotePathCacheWithAliases(notes, aliasesByPath)
		}
	}

	if !isDir {
		if params.SessionStore != nil && vaultRoot != "" {
			if rel, relErr := vaultPaths.RelCodeStrict(absPath); relErr == nil && rel.String() != "" {
				rationale, ratErr := params.SessionStore.RationaleForPath(ctx, string(paths.NormalizeCode(rel.String())))
				if ratErr == nil {
					fileCtx.Rationale = prioritizeRationaleForContext(rationale)
				}
			}
		}
		if refs := params.CodeRefsByFile; len(refs) > 0 {
			if relForRefs, relErr := codeRefPaths.RelCodeStrict(absPath); relErr == nil && relForRefs.String() != "" {
				if codeRefs := refs[relForRefs.String()]; len(codeRefs) > 0 {
					fileCtx.LinkedNotes = buildLinkedNotes(codeRefs, params.NoteReader)
				}
			}
		} else if indexedCoderefBranch && params.SessionStore != nil && vaultRoot != "" {
			if relForIndex, relErr := vaultPaths.RelCodeStrict(absPath); relErr == nil && relForIndex.String() != "" {
				codeRefs, scopedCache, coderefTruncated, coderefErr := indexedCoderefsForRequestedFile(ctx, params.SessionStore, relForIndex.String(), absPath)
				if coderefErr != nil {
					return fileCtx, coderefErr
				}
				if coderefTruncated {
					fileCtx.Truncated = true
				}
				if len(codeRefs) > 0 {
					fileCtx.LinkedNotes = buildLinkedNotes(codeRefs, params.NoteReader)
				}
				if noteCache == nil {
					noteCache = scopedCache
				}
			}
		}
	}

	if params.CodeAnchor != nil {
		var allowedKinds map[codeanchor.AnchorKind]bool
		if len(params.AnchorKinds) > 0 {
			allowedKinds = make(map[codeanchor.AnchorKind]bool, len(params.AnchorKinds))
			for _, k := range params.AnchorKinds {
				if k != "" {
					allowedKinds[k] = true
				}
			}
		}
		fileCtx.LinkedNotes, fileCtx.AnchorMatches = mergeCodeAnchor(ctx, fileCtx.LinkedNotes, params.CodeAnchor, absPath, params.VaultDef, vaultPaths, allowedKinds, params.SkipAnchorRefresh)
	}

	fileCtx.LinkedNotes = expandLinkedNoteFrontmatter(fileCtx.LinkedNotes, params.NoteReader)

	if indexedNoteCacheBranch {
		contents := make([]string, 0, len(fileCtx.AncestorDocs))
		for _, doc := range fileCtx.AncestorDocs {
			contents = append(contents, doc.Content)
		}
		var cacheErr error
		var cacheTruncated bool
		noteCache, cacheTruncated, cacheErr = extendIndexedNotePathCache(ctx, params.SessionStore, noteCache, contents)
		if cacheErr != nil {
			return fileCtx, cacheErr
		}
		if cacheTruncated {
			fileCtx.Truncated = true
		}
	}

	// Default behavior: include lightweight stubs for notes linked from ancestor docs
	// (CONTEXT.md, etc) so callers gain awareness without full recursive expansion.
	if noteCache != nil {
		fileCtx.LinkedNotes = mergeAncestorDocLinkedNotes(fileCtx.AncestorDocs, fileCtx.LinkedNotes, params.NoteReader, noteCache, excludeNotes)
	}

	// Hub/MOC expansion: if a linked note matches expandExpr, include its outgoing
	// wikilinks as additional linked notes (stubs: blessed frontmatter only).
	if indexedNoteCacheBranch && params.NoteReader != nil && expandLimit > 0 && len(expandExpr) > 0 {
		contents := indexedExpansionSourceContents(fileCtx.LinkedNotes, params.NoteReader, params.VaultDef, expandExpr)
		var cacheErr error
		var cacheTruncated bool
		noteCache, cacheTruncated, cacheErr = extendIndexedNotePathCache(ctx, params.SessionStore, noteCache, contents)
		if cacheErr != nil {
			return fileCtx, cacheErr
		}
		if cacheTruncated {
			fileCtx.Truncated = true
		}
	}
	if noteCache != nil && params.NoteReader != nil && expandLimit > 0 && len(expandExpr) > 0 {
		fileCtx.LinkedNotes = expandFromMatchedNotes(fileCtx.LinkedNotes, params.NoteReader, params.VaultDef, noteCache, excludeNotes, expandExpr, expandLimit)
	}

	if len(excludeNotes) > 0 && len(fileCtx.LinkedNotes) > 0 {
		filtered := fileCtx.LinkedNotes[:0]
		for _, ln := range fileCtx.LinkedNotes {
			if _, skip := excludeNotes[ln.Path]; skip {
				continue
			}
			filtered = append(filtered, ln)
		}
		fileCtx.LinkedNotes = filtered
	}
	if params.EnsureLinkTarget == ontology.EnsureLinkTargetApply && params.NodeReadScope == nil {
		for _, note := range fileCtx.LinkedNotes {
			if note.Fragment != "" {
				return fileCtx, fmt.Errorf("ensure-link apply requires a ready live ontology index")
			}
		}
	}
	if params.NodeReadScope != nil {
		fileCtx.OntologyNodes, fileCtx.NodeDiagnostics = resolveLinkedOntologyNodes(ctx, params.NodeReadScope, fileCtx.LinkedNotes, params.EnsureLinkTarget)
		if params.EnsureLinkTarget == ontology.EnsureLinkTargetApply {
			for _, diagnostic := range fileCtx.NodeDiagnostics {
				if diagnostic.Code == "resolve_failed" {
					return fileCtx, fmt.Errorf("ensure-link apply: %s", diagnostic.Message)
				}
			}
		}
	}

	fileCtx.ReturnedNotePaths = uniqueNotePaths(fileCtx.LinkedNotes)
	fileCtx.ReturnedDocPaths = uniqueDocPaths(fileCtx.AncestorDocs)

	return fileCtx, nil
}
