package actions

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/noteownership"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/pkg/vault/frontmatter"
	"github.com/atomicobject/rhizome/pkg/vault/notediscovery"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/bmatcuk/doublestar/v4"
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
		maxEmpty = obsidian.FileContextConfigDefaults.MaxEmptyLevels
	}
	budget := params.ContextBudget
	if budget <= 0 {
		budget = obsidian.FileContextConfigDefaults.ContextBudget
	}
	expandExpr := params.ExpandNoteLinks
	if len(expandExpr) == 0 {
		expandExpr = obsidian.FileContextConfigDefaults.ExpandNoteLinks
	}
	expandLimit := params.ExpandNoteLinksLimit
	if expandLimit <= 0 {
		expandLimit = obsidian.FileContextConfigDefaults.ExpandNoteLinksLimit
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

const indexedFileContextCoderefLimit = 100
const indexedFileContextLinkTargetLimit = 25

// indexedCoderefsForRequestedFile uses the durable code-to-note edges as an
// exact target whitelist, then rescans only the explicitly requested source
// file. The rescan restores occurrence details that are intentionally absent
// from the compact index row without reopening the vault-wide note inventory.
func indexedCoderefsForRequestedFile(ctx context.Context, store *semdb.Store, codePath, absPath string) ([]coderefs.CodeRef, *obsidian.NotePathCache, bool, error) {
	if store == nil || strings.TrimSpace(codePath) == "" || strings.TrimSpace(absPath) == "" {
		return nil, nil, false, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, false, err
	}
	links, err := store.IndexedCodeNoteLinksForFile(ctx, codePath, indexedFileContextCoderefLimit)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, nil, false, ctxErr
		}
		return nil, nil, false, nil
	}
	truncated := len(links) >= indexedFileContextCoderefLimit
	if len(links) == 0 {
		return nil, nil, truncated, nil
	}

	notePaths := make([]string, 0, len(links))
	allowed := make(map[string]struct{}, len(links))
	for _, link := range links {
		notePath := string(paths.NormalizeNote(link.NotePath))
		if notePath == "" {
			continue
		}
		if _, exists := allowed[notePath]; exists {
			continue
		}
		allowed[notePath] = struct{}{}
		notePaths = append(notePaths, notePath)
	}
	if len(notePaths) == 0 {
		return nil, nil, truncated, nil
	}

	aliasesByPath := make(map[string][]string)
	if rows, aliasErr := store.CurrentNotePropertyValues(ctx, notePaths, []string{"aliases"}, semdb.NotePropertySourceFrontmatter); aliasErr == nil {
		for _, row := range rows {
			alias := strings.TrimSpace(row.ValueText)
			if alias != "" {
				aliasesByPath[row.NotePath] = append(aliasesByPath[row.NotePath], alias)
			}
		}
	} else if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, nil, truncated, ctxErr
	}
	scopedCache := obsidian.BuildNotePathCacheWithAliases(notePaths, aliasesByPath)
	if err := ctx.Err(); err != nil {
		return nil, scopedCache, truncated, err
	}
	content, err := os.ReadFile(absPath)
	if err != nil {
		return nil, scopedCache, truncated, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, scopedCache, truncated, err
	}
	refs, err := coderefs.ScanFile(codePath, content, scopedCache)
	if err != nil {
		return nil, scopedCache, truncated, nil
	}
	filtered := refs[:0]
	for _, ref := range refs {
		if _, ok := allowed[string(paths.NormalizeNote(ref.Target))]; ok {
			filtered = append(filtered, ref)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, scopedCache, truncated, err
	}
	return filtered, scopedCache, truncated, nil
}

// extendIndexedNotePathCache resolves only wikilinks extracted from the small
// set of documents selected for this request. It keeps ancestor and hub/MOC
// expansion behavior without reopening the vault-wide note inventory.
func extendIndexedNotePathCache(ctx context.Context, store *semdb.Store, cache *obsidian.NotePathCache, contents []string) (*obsidian.NotePathCache, bool, error) {
	if store == nil {
		return cache, false, nil
	}
	if err := ctx.Err(); err != nil {
		return cache, false, err
	}
	targets, truncated := indexedWikilinkTargets(contents, indexedFileContextLinkTargetLimit)
	if len(targets) == 0 {
		return cache, truncated, nil
	}

	pathSet := make(map[string]struct{})
	aliasesByPath := make(map[string][]string)
	if cache != nil {
		for notePath := range cache.NotePaths {
			pathSet[notePath] = struct{}{}
		}
		for _, notePath := range cache.Paths {
			pathSet[notePath] = struct{}{}
		}
		for alias, notePaths := range cache.Aliases {
			for _, notePath := range notePaths {
				aliasesByPath[notePath] = append(aliasesByPath[notePath], alias)
			}
		}
	}

	resolved, err := store.ResolveStoredNoteLinks(ctx, targets)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return cache, truncated, ctxErr
		}
		return cache, truncated, nil
	}
	directPaths := make([]string, 0, len(resolved))
	for _, notePath := range resolved {
		pathSet[notePath] = struct{}{}
		directPaths = append(directPaths, notePath)
	}
	directCache := obsidian.BuildNotePathCache(directPaths)

	// Path/title matches outrank aliases in NotePathCache. Add every targeted
	// alias claimant so ambiguous aliases remain unresolved.
	for _, target := range targets {
		if _, ok := directCache.ResolveNote(target); ok {
			continue
		}
		notePaths, aliasErr := store.CurrentNotePathsByPropertyValue(
			ctx,
			"aliases",
			indexedAliasLookupValue(target),
			semdb.NotePropertySourceFrontmatter,
		)
		if aliasErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return cache, truncated, ctxErr
			}
			continue
		}
		for _, notePath := range notePaths {
			pathSet[notePath] = struct{}{}
			aliasesByPath[notePath] = append(aliasesByPath[notePath], target)
		}
	}

	notePaths := make([]string, 0, len(pathSet))
	for notePath := range pathSet {
		notePaths = append(notePaths, notePath)
	}
	sort.Strings(notePaths)
	if rows, aliasErr := store.CurrentNotePropertyValues(ctx, notePaths, []string{"aliases"}, semdb.NotePropertySourceFrontmatter); aliasErr == nil {
		for _, row := range rows {
			if alias := strings.TrimSpace(row.ValueText); alias != "" {
				aliasesByPath[row.NotePath] = append(aliasesByPath[row.NotePath], alias)
			}
		}
	} else if ctxErr := ctx.Err(); ctxErr != nil {
		return cache, truncated, ctxErr
	}
	return obsidian.BuildNotePathCacheWithAliases(notePaths, aliasesByPath), truncated, nil
}

func indexedAliasLookupValue(target string) string {
	target = strings.TrimSpace(target)
	if ext := filepath.Ext(target); strings.EqualFold(ext, ".md") {
		target = strings.TrimSuffix(target, ext)
	}
	return strings.ToLower(strings.TrimSpace(target))
}

func indexedWikilinkTargets(contents []string, limit int) ([]string, bool) {
	if limit <= 0 {
		return nil, false
	}
	seen := make(map[string]struct{}, limit)
	targets := make([]string, 0, min(limit, len(contents)))
	for _, content := range contents {
		for _, match := range wikilinkPattern.FindAllStringSubmatch(content, -1) {
			if len(match) < 2 {
				continue
			}
			target := strings.TrimSpace(match[1])
			if pipe := strings.Index(target, "|"); pipe >= 0 {
				target = target[:pipe]
			}
			if hash := strings.Index(target, "#"); hash >= 0 {
				target = target[:hash]
			}
			target = strings.TrimSpace(target)
			if target == "" {
				continue
			}
			if _, ok := seen[target]; ok {
				continue
			}
			seen[target] = struct{}{}
			if len(targets) >= limit {
				return targets, true
			}
			targets = append(targets, target)
		}
	}
	return targets, false
}

func indexedExpansionSourceContents(linked []LinkedNoteContext, noteMgr obsidian.NoteReader, vaultDef obsidian.VaultDefinition, matchExpr []string) []string {
	if len(linked) == 0 || noteMgr == nil {
		return nil
	}
	_, expr, err := ParseInputsWithExpression(matchExpr)
	if err != nil || expr == nil {
		return nil
	}
	contents := make([]string, 0, min(len(linked), indexedFileContextLinkTargetLimit))
	for _, src := range linked {
		if src.Path == "" || !linkedNoteMatches(src, expr) {
			continue
		}
		content, readErr := noteMgr.GetContents(vaultDef, src.Path)
		if readErr != nil || content == "" {
			continue
		}
		contents = append(contents, content)
		if len(contents) >= indexedFileContextLinkTargetLimit {
			break
		}
	}
	return contents
}

func prioritizeRationaleForContext(rationales []codeanchor.Rationale) []codeanchor.Rationale {
	if len(rationales) <= 1 {
		return rationales
	}
	out := append([]codeanchor.Rationale(nil), rationales...)
	sort.SliceStable(out, func(i, j int) bool {
		pi := rationaleContextPriority(out[i].Kind)
		pj := rationaleContextPriority(out[j].Kind)
		if pi != pj {
			return pi < pj
		}
		return out[i].StartLine < out[j].StartLine
	})
	return out
}

func rationaleContextPriority(kind codeanchor.RationaleKind) int {
	switch kind {
	case codeanchor.RationaleWhy, codeanchor.RationaleRationale, codeanchor.RationaleImportant:
		return 0
	case codeanchor.RationaleNote:
		return 1
	default:
		return 2
	}
}

func buildLinkedNotes(refs []coderefs.CodeRef, noteMgr obsidian.NoteReader) []LinkedNoteContext {
	if len(refs) == 0 {
		return nil
	}
	if noteMgr == nil {
		noteMgr = &obsidian.Note{}
	}

	seen := make(map[string]bool)
	out := make([]LinkedNoteContext, 0, len(refs))
	for _, ref := range refs {
		key := ref.Target + "#" + ref.Fragment + "|" + string(ref.Kind) + "|" + ref.SourceFile + "|" + strconv.Itoa(ref.Line)
		if seen[key] {
			continue
		}
		seen[key] = true

		fm := blessedFrontmatter(noteMgr, ref.Target)
		out = append(out, LinkedNoteContext{
			Path:        ref.Target,
			Fragment:    strings.TrimSpace(ref.Fragment),
			RawTarget:   strings.TrimSpace(ref.RawTarget),
			Title:       titleFromPath(ref.Target),
			Kind:        string(ref.Kind),
			Line:        ref.Line,
			Snippet:     ref.Snippet,
			Frontmatter: fm,
		})
	}
	return out
}

func resolveLinkedOntologyNodes(ctx context.Context, scope *noderead.Scope, notes []LinkedNoteContext, ensure ontology.EnsureLinkTargetMode) ([]OntologyNodeContext, []noderead.NodeResolveDiagnostic) {
	if scope == nil || len(notes) == 0 {
		return nil, nil
	}
	targets := make([]noderead.NodeTarget, 0)
	byInput := map[string]LinkedNoteContext{}
	for _, note := range notes {
		if strings.TrimSpace(note.Fragment) == "" {
			continue
		}
		input := strings.TrimSpace(note.RawTarget)
		if input == "" {
			input = note.Path + "#" + note.Fragment
		}
		if !strings.HasPrefix(input, "[[") && !strings.Contains(input, "](") {
			input = note.Path + "#" + note.Fragment
		}
		targets = append(targets, noderead.NodeTarget{Input: input})
		byInput[input] = note
	}
	if len(targets) == 0 {
		return nil, nil
	}
	result, err := scope.Resolve(ctx, noderead.ResolveRequest{
		Targets:          targets,
		EnsureLinkTarget: ensure,
		Hydrate:          noderead.HydrateOptions{Profile: noderead.HydrateSummary},
	})
	if err != nil {
		return nil, []noderead.NodeResolveDiagnostic{{Code: "resolve_failed", Message: err.Error()}}
	}
	out := make([]OntologyNodeContext, 0, len(result.Resolved))
	for _, resolved := range result.Resolved {
		source := byInput[resolved.Input]
		locator := resolved.Locator
		record := resolved.Record
		ctx := OntologyNodeContext{
			Input:         resolved.Input,
			SourcePath:    source.Path,
			Line:          source.Line,
			Snippet:       source.Snippet,
			Ref:           resolved.Ref,
			TypeName:      firstNonEmptyString(record.TypeName, resolved.Ref.TypeName),
			Title:         firstNonEmptyString(record.Title, locator.SourceLocator, resolved.Ref.String()),
			SourceLocator: locator.SourceLocator,
			Status:        locator.Status,
			Content:       record.Content,
			RequiresFix:   locator.Status == ontology.NodeLocatorRequiresFix,
		}
		if locator.LinkTarget != nil {
			ctx.Wikilink = locator.LinkTarget.Wikilink
			ctx.Markdown = locator.LinkTarget.Markdown
		}
		out = append(out, ctx)
	}
	return out, result.Diagnostics
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func blessedFrontmatter(noteMgr obsidian.NoteReader, notePath string) map[string]interface{} {
	if noteMgr == nil || notePath == "" {
		return nil
	}
	fact, ok := NoteFactsFromReader(noteMgr).LookupFact(notePath)
	if !ok {
		return nil
	}
	return frontmatter.FilterBlessed(fact.Frontmatter)
}

func mergeCodeAnchor(ctx context.Context, existing []LinkedNoteContext, svc *codeanchor.Service, absPath string, vaultDef obsidian.VaultDefinition, vaultPaths paths.VaultPaths, allowedKinds map[codeanchor.AnchorKind]bool, skipRefresh bool) ([]LinkedNoteContext, []codeanchor.AnchorMatchTrace) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if !skipRefresh && svc.HasDirtyAnchors() {
		// Best-effort to refresh scopes when ad-hoc indexing just ran.
		_ = svc.RecomputeAnchorScopes(ctx)
	}
	fc, err := svc.NotesForFile(ctx, absPath)
	if err != nil {
		return existing, nil
	}
	seen := make(map[string]bool, len(existing))
	for _, ln := range existing {
		seen[ln.Path+"|"+ln.Kind] = true
	}
	// Prefer per-anchor attribution when available.
	if len(allowedKinds) > 0 && len(fc.AnchorKinds) > 0 {
		allowedLabels := make(map[string]struct{}, len(fc.AnchorKinds))
		for label, kind := range fc.AnchorKinds {
			if allowedKinds[kind] {
				allowedLabels[label] = struct{}{}
			}
		}
		if len(allowedLabels) == 0 {
			return existing, nil
		}
		if len(fc.AnchorNotes) > 0 {
			filtered := fc.AnchorNotes[:0]
			for _, an := range fc.AnchorNotes {
				if _, ok := allowedLabels[an.Label]; ok {
					filtered = append(filtered, an)
				}
			}
			fc.AnchorNotes = filtered
		}
		if len(fc.Anchors) > 0 {
			filtered := fc.Anchors[:0]
			for _, label := range fc.Anchors {
				if _, ok := allowedLabels[label]; ok {
					filtered = append(filtered, label)
				}
			}
			fc.Anchors = filtered
		}
		if len(fc.Trace) > 0 {
			filtered := fc.Trace[:0]
			for _, tr := range fc.Trace {
				if _, ok := allowedLabels[tr.AnchorLabel]; ok {
					filtered = append(filtered, tr)
				}
			}
			fc.Trace = filtered
		}
		if len(fc.AnchorNotes) == 0 && len(fc.Anchors) == 0 {
			return existing, nil
		}
	}
	if len(fc.AnchorNotes) > 0 {
		for _, an := range fc.AnchorNotes {
			label := strings.TrimSpace(an.Label)
			if label == "" {
				continue
			}
			kind := "anchor:" + label
			for _, note := range an.Notes {
				normPath := normalizeNotePath(note.Path, vaultDef, vaultPaths)
				if normPath == "" {
					normPath = note.Path
				}
				key := normPath + "|" + kind
				if seen[key] {
					continue
				}
				seen[key] = true
				existing = append(existing, LinkedNoteContext{
					Path:  normPath,
					Title: note.Title,
					Kind:  kind,
				})
			}
		}
		return existing, fc.Trace
	}

	// Fallback (older stores/services): emit anchors without per-note attribution.
	kind := "anchor"
	if len(fc.Anchors) > 0 {
		kind = "anchor:" + strings.Join(fc.Anchors, ",")
	}
	for _, note := range fc.Notes {
		normPath := normalizeNotePath(note.Path, vaultDef, vaultPaths)
		if normPath == "" {
			normPath = note.Path
		}
		key := normPath + "|" + kind
		if seen[key] {
			continue
		}
		seen[key] = true
		existing = append(existing, LinkedNoteContext{
			Path:  normPath,
			Title: note.Title,
			Kind:  kind,
		})
	}
	return existing, fc.Trace
}

func expandLinkedNoteFrontmatter(notes []LinkedNoteContext, noteMgr obsidian.NoteReader) []LinkedNoteContext {
	if noteMgr == nil || len(notes) == 0 {
		return notes
	}
	for i := range notes {
		if notes[i].Frontmatter != nil {
			continue
		}
		fm := blessedFrontmatter(noteMgr, notes[i].Path)
		notes[i].Frontmatter = fm
	}
	return notes
}

var wikilinkPattern = regexp.MustCompile(`!?\[\[([^\]]+)\]\]`)

func normalizeNotePath(path string, vaultDef obsidian.VaultDefinition, vaultPaths paths.VaultPaths) string {
	if path == "" {
		return ""
	}
	path = string(paths.Normalize(path))
	if filepath.IsAbs(path) && vaultDef.BasePath() != "" && vaultPaths.Root() != "" {
		if rel, err := vaultPaths.RelStrict(path); err == nil && rel.String() != "" {
			path = rel.String()
		}
	}
	return string(paths.Normalize(path))
}

func expandFromMatchedNotes(linked []LinkedNoteContext, noteMgr obsidian.NoteReader, vaultDef obsidian.VaultDefinition, cache *obsidian.NotePathCache, exclude map[string]struct{}, matchExpr []string, limit int) []LinkedNoteContext {
	if len(linked) == 0 || noteMgr == nil || cache == nil || limit <= 0 {
		return linked
	}
	_, expr, err := ParseInputsWithExpression(matchExpr)
	if err != nil || expr == nil {
		return linked
	}

	seen := make(map[string]struct{}, len(linked))
	for _, ln := range linked {
		if ln.Path != "" {
			seen[ln.Path] = struct{}{}
		}
	}

	added := 0
	const hardCap = 25

	for _, src := range linked {
		if src.Path == "" {
			continue
		}
		if src.Frontmatter == nil {
			src.Frontmatter = blessedFrontmatter(noteMgr, src.Path)
		}
		if !linkedNoteMatches(src, expr) {
			continue
		}

		content, err := noteMgr.GetContents(vaultDef, src.Path)
		if err != nil || content == "" {
			continue
		}

		kind := "linkedFromNote"
		if hasLinkedNoteTag(src, "type/hub") {
			kind = "linkedFromHub"
		} else if hasLinkedNoteTag(src, "maps-of-content") || hasLinkedNoteTag(src, "type/moc") {
			kind = "linkedFromMOC"
		}

		matches := wikilinkPattern.FindAllStringSubmatch(content, -1)
		for _, m := range matches {
			if len(m) < 2 {
				continue
			}
			target := strings.TrimSpace(m[1])
			if target == "" {
				continue
			}
			if pipe := strings.Index(target, "|"); pipe >= 0 {
				target = target[:pipe]
			}
			if hash := strings.Index(target, "#"); hash >= 0 {
				target = target[:hash]
			}
			target = strings.TrimSpace(target)
			if target == "" {
				continue
			}

			resolved, ok := cache.ResolveNote(target)
			if !ok || resolved == "" {
				continue
			}
			resolved = string(paths.NormalizeNotePath(resolved))
			if _, skip := exclude[resolved]; skip {
				continue
			}
			if _, ok := seen[resolved]; ok {
				continue
			}
			seen[resolved] = struct{}{}

			fm := blessedFrontmatter(noteMgr, resolved)
			linked = append(linked, LinkedNoteContext{
				Path:        resolved,
				Title:       titleFromPath(resolved),
				Kind:        kind,
				Snippet:     src.Title,
				Frontmatter: fm,
			})

			added++
			if added >= limit || added >= hardCap {
				return linked
			}
		}
	}

	return linked
}

func linkedNoteMatches(note LinkedNoteContext, expr *InputExpression) bool {
	if expr == nil {
		return false
	}
	switch expr.Type {
	case exprLeaf:
		if expr.Input == nil {
			return false
		}
		in := expr.Input
		switch in.Type {
		case InputTypeTag:
			return hasLinkedNoteTag(note, in.Value)
		case InputTypeFind:
			q := strings.ToLower(strings.TrimSpace(in.Value))
			if q == "" {
				return false
			}
			return strings.Contains(strings.ToLower(note.Title), q) || strings.Contains(strings.ToLower(note.Path), q)
		case InputTypeFile:
			pat := strings.TrimSpace(in.Value)
			if pat == "" {
				return false
			}
			ok, _ := doublestar.Match(filepath.ToSlash(pat), filepath.ToSlash(note.Path))
			if ok {
				return true
			}
			// Also allow exact match on basename for convenience.
			return strings.EqualFold(filepath.Base(pat), filepath.Base(note.Path))
		case InputTypeProperty:
			if note.Frontmatter == nil || in.Property == "" {
				return false
			}
			v, ok := note.Frontmatter[in.Property]
			if !ok {
				return false
			}
			want := strings.TrimSpace(in.Value)
			return strings.EqualFold(strings.TrimSpace(fmt.Sprint(v)), want)
		default:
			return false
		}
	case exprAnd:
		return linkedNoteMatches(note, expr.Left) && linkedNoteMatches(note, expr.Right)
	case exprOr:
		return linkedNoteMatches(note, expr.Left) || linkedNoteMatches(note, expr.Right)
	case exprNot:
		return !linkedNoteMatches(note, expr.Left)
	default:
		return false
	}
}

func hasLinkedNoteTag(note LinkedNoteContext, tag string) bool {
	tag = strings.ToLower(strings.TrimSpace(tag))
	if tag == "" || note.Frontmatter == nil {
		return false
	}
	raw, ok := note.Frontmatter["tags"]
	if !ok {
		return false
	}
	switch t := raw.(type) {
	case []string:
		for _, v := range t {
			if strings.ToLower(strings.TrimSpace(v)) == tag {
				return true
			}
		}
	case []interface{}:
		for _, v := range t {
			if s, ok := v.(string); ok && strings.ToLower(strings.TrimSpace(s)) == tag {
				return true
			}
		}
	case string:
		// Support comma-separated.
		for _, v := range strings.Split(t, ",") {
			if strings.ToLower(strings.TrimSpace(v)) == tag {
				return true
			}
		}
	}
	return false
}

func mergeAncestorDocLinkedNotes(docs []AncestorDocContext, linked []LinkedNoteContext, noteMgr obsidian.NoteReader, cache *obsidian.NotePathCache, exclude map[string]struct{}) []LinkedNoteContext {
	if len(docs) == 0 || noteMgr == nil || cache == nil {
		return linked
	}
	linkedSet := make(map[string]struct{}, len(linked))
	for _, ln := range linked {
		if ln.Path != "" {
			linkedSet[ln.Path] = struct{}{}
		}
	}
	seen := make(map[string]struct{})
	for p := range linkedSet {
		seen[p] = struct{}{}
	}

	// Hard cap to avoid runaway expansion from very link-heavy CONTEXT docs.
	const stubLimit = 25

	for _, doc := range docs {
		if doc.Content == "" {
			continue
		}
		matches := wikilinkPattern.FindAllStringSubmatch(doc.Content, -1)
		for _, m := range matches {
			if len(m) < 2 {
				continue
			}
			target := strings.TrimSpace(m[1])
			if target == "" {
				continue
			}
			if pipe := strings.Index(target, "|"); pipe >= 0 {
				target = target[:pipe]
			}
			if hash := strings.Index(target, "#"); hash >= 0 {
				target = target[:hash]
			}
			target = strings.TrimSpace(target)
			if target == "" {
				continue
			}

			// Only wikilinks are supported here; CONTEXT.md is outside the vault,
			// so markdown link relative resolution isn't meaningful.
			resolved, ok := cache.ResolveNote(target)
			if !ok || resolved == "" {
				continue
			}
			resolved = string(paths.NormalizeNotePath(resolved))
			if _, skip := exclude[resolved]; skip {
				continue
			}
			if _, ok := seen[resolved]; ok {
				continue
			}
			seen[resolved] = struct{}{}

			fm := blessedFrontmatter(noteMgr, resolved)
			linked = append(linked, LinkedNoteContext{
				Path:        resolved,
				Title:       titleFromPath(resolved),
				Kind:        "ancestorDoc",
				Frontmatter: fm,
			})
			if len(seen) >= stubLimit+len(linkedSet) {
				return linked
			}
		}
	}
	return linked
}

func collectAncestorDocs(absFilePath string, rootPaths paths.VaultPaths, patterns []string, maxEmpty, budget int) ([]AncestorDocContext, bool) {
	var docs []AncestorDocContext
	truncated := false

	startDir := filepath.Dir(absFilePath)
	root := rootPaths.Root()
	if root == "" {
		root = startDir
	}
	emptyStreak := 0

	for dir := startDir; ; {
		match, ok := findDocMatchInDir(dir, patterns, rootPaths)
		if ok {
			content, err := os.ReadFile(match.AbsPath)
			if err == nil {
				emptyStreak = 0
				docs = append(docs, AncestorDocContext{
					Dir:     match.Dir,
					Pattern: match.Pattern,
					Path:    match.Path,
					Content: string(content),
				})
			} else {
				ok = false
			}
		}
		if !ok {
			emptyStreak++
			if maxEmpty > 0 && emptyStreak >= maxEmpty {
				break
			}
		}
		if dir == root {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir || !isSubdir(parent, rootPaths) {
			break
		}
		dir = parent
	}

	// Apply proximity-first budgeting (docs collected from closest to farthest).
	// This favors nearby docs and may truncate or drop higher-level CONTEXT files
	// once the budget is exhausted.
	remaining := budget
	for i := 0; i < len(docs); i++ {
		if remaining <= 0 {
			docs = docs[:i]
			truncated = true
			break
		}
		if len(docs[i].Content) > remaining {
			content := docs[i].Content[:remaining]
			if idx := strings.LastIndex(content, "\n"); idx > 0 {
				content = content[:idx+1]
			}
			if !strings.HasSuffix(content, "\n") {
				content += "\n"
			}
			docs[i].Content = content + "[... truncated ...]"
			docs[i].Truncated = true
			truncated = true
			remaining = 0
			docs = docs[:i+1]
			break
		}
		remaining -= len(docs[i].Content)
	}
	if remaining == 0 && len(docs) > 0 {
		truncated = true
	}

	return docs, truncated
}

func isSubdir(dir string, rootPaths paths.VaultPaths) bool {
	if dir == "" || rootPaths.Root() == "" {
		return false
	}
	_, err := rootPaths.RelStrict(dir)
	return err == nil
}

// SubmoduleDocsBudget is the dedicated budget for submodule docs (25000 chars).
const SubmoduleDocsBudget = 25000

// collectSubmoduleDocs finds CONTEXT.md files in subdirectories up to maxDepth.
// Returns docs ordered by depth (shallower first) for proximity-first budgeting.
func collectSubmoduleDocs(dirPath string, rootPaths paths.VaultPaths, patterns []string, maxDepth, budget int) ([]SubmoduleDocContext, bool) {
	if maxDepth <= 0 || dirPath == "" {
		return nil, false
	}
	patterns = obsidian.NormalizeDocPatterns(patterns)
	if len(patterns) == 0 {
		return nil, false
	}

	type candidate struct {
		absPath string
		relPath string
		depth   int
	}
	var candidates []candidate

	// Walk subdirectories up to maxDepth
	_ = filepath.WalkDir(dirPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if path == dirPath {
			return nil // skip the root directory itself
		}
		if !d.IsDir() {
			return nil // only interested in directories
		}

		// Calculate depth from dirPath
		relFromStart, relErr := filepath.Rel(dirPath, path)
		if relErr != nil {
			return nil
		}
		depth := strings.Count(relFromStart, string(filepath.Separator)) + 1
		if depth > maxDepth {
			return filepath.SkipDir // don't descend further
		}

		// Check for doc match in this directory
		match, ok := findDocMatchInDir(path, patterns, rootPaths)
		if ok {
			candidates = append(candidates, candidate{
				absPath: match.AbsPath,
				relPath: match.Path,
				depth:   depth,
			})
		}
		return nil
	})

	if len(candidates) == 0 {
		return nil, false
	}

	// Sort by depth (shallower first) for proximity-first budgeting
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].depth != candidates[j].depth {
			return candidates[i].depth < candidates[j].depth
		}
		return candidates[i].relPath < candidates[j].relPath
	})

	// Read content and apply budget
	var docs []SubmoduleDocContext
	truncated := false
	remaining := budget

	for _, c := range candidates {
		if remaining <= 0 {
			truncated = true
			break
		}

		content, err := os.ReadFile(c.absPath)
		if err != nil {
			continue
		}

		doc := SubmoduleDocContext{
			Path:  c.relPath,
			Depth: c.depth,
		}

		if len(content) > remaining {
			// Truncate at last newline
			truncContent := string(content[:remaining])
			if idx := strings.LastIndex(truncContent, "\n"); idx > 0 {
				truncContent = truncContent[:idx+1]
			}
			if !strings.HasSuffix(truncContent, "\n") {
				truncContent += "\n"
			}
			doc.Content = truncContent + "[... truncated ...]"
			doc.Truncated = true
			truncated = true
			remaining = 0
		} else {
			doc.Content = string(content)
			remaining -= len(content)
		}

		docs = append(docs, doc)

		if remaining <= 0 {
			break
		}
	}

	return docs, truncated
}

func titleFromPath(path string) string {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	return base
}

func toRelSet(items []string, vaultPaths paths.VaultPaths, note bool) map[string]struct{} {
	if len(items) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(items))
	for _, s := range items {
		if strings.TrimSpace(s) == "" {
			continue
		}
		clean := filepath.ToSlash(filepath.Clean(s))
		if vaultPaths.Root() != "" && filepath.IsAbs(s) {
			if note {
				if rel, err := vaultPaths.RelNotePathStrict(s); err == nil && rel.String() != "" {
					out[rel.String()] = struct{}{}
					continue
				}
			} else {
				if rel, err := vaultPaths.RelStrict(s); err == nil && rel.String() != "" {
					out[rel.String()] = struct{}{}
					continue
				}
			}
		}
		if note {
			clean = string(paths.NormalizeNotePath(clean))
		} else {
			clean = string(paths.Normalize(clean))
		}
		if clean != "" {
			out[clean] = struct{}{}
		}
	}
	return out
}

func uniqueNotePaths(notes []LinkedNoteContext) []string {
	if len(notes) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(notes))
	out := make([]string, 0, len(notes))
	for _, n := range notes {
		if n.Path == "" {
			continue
		}
		if _, ok := set[n.Path]; ok {
			continue
		}
		set[n.Path] = struct{}{}
		out = append(out, n.Path)
	}
	return out
}

func uniqueDocPaths(docs []AncestorDocContext) []string {
	if len(docs) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(docs))
	out := make([]string, 0, len(docs))
	for _, d := range docs {
		if d.Path == "" {
			continue
		}
		if _, ok := set[d.Path]; ok {
			continue
		}
		set[d.Path] = struct{}{}
		out = append(out, d.Path)
	}
	return out
}
