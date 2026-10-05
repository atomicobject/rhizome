package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/app/noteownership"
	"github.com/atomicobject/rhizome/pkg/app/oneshotruntime"
	"github.com/atomicobject/rhizome/pkg/app/unifiedsearch"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/guide"
	"github.com/atomicobject/rhizome/pkg/ontology/idalloc"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/pkg/vault/identity"
	"github.com/atomicobject/rhizome/pkg/vault/notediscovery"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type ontologyQueryRuntime struct {
	live       *bootstrap.LiveRuntime
	runtime    *ontology.Runtime
	service    *ontology.Service
	noteReader obsidian.NoteReader
	schema     *ontology.Schema
	execSchema *ontologyquery.ExecutableSchema
	// noteFormatRuntime is a focused runtime-test seam. Production always uses
	// the immutable format runtime owned by LiveRuntime's metadata indexer.
	noteFormatRuntime *noteformat.Runtime
}

func loadOntologyExecutableSchema(vaultPath string) (*ontology.Schema, *ontologyquery.ExecutableSchema, error) {
	schema, err := ontology.LoadSchema(vaultPath)
	if err != nil {
		return nil, nil, err
	}
	execSchema, err := ontologyquery.BuildExecutableSchema(schema)
	if err != nil {
		return nil, nil, err
	}
	return schema, execSchema, nil
}

func readOntologyQueryInput(rawQuery, queryFile string) (string, error) {
	hasQuery := strings.TrimSpace(rawQuery) != ""
	hasFile := strings.TrimSpace(queryFile) != ""
	switch {
	case hasQuery == hasFile:
		return "", fmt.Errorf("exactly one of --query or --file is required")
	case hasFile:
		data, err := os.ReadFile(queryFile)
		if err != nil {
			return "", err
		}
		return string(data), nil
	default:
		return rawQuery, nil
	}
}

func readOntologyVariablesInput(rawJSON, variablesFile string) (map[string]any, error) {
	hasJSON := strings.TrimSpace(rawJSON) != ""
	hasFile := strings.TrimSpace(variablesFile) != ""
	switch {
	case hasJSON && hasFile:
		return nil, fmt.Errorf("only one of --variables-json or --variables-file may be provided")
	case hasFile:
		data, err := os.ReadFile(variablesFile)
		if err != nil {
			return nil, err
		}
		return parseOntologyVariablesJSON(data)
	case hasJSON:
		return parseOntologyVariablesJSON([]byte(rawJSON))
	default:
		return nil, nil
	}
}

func parseOntologyVariablesJSON(data []byte) (map[string]any, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	var variables map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&variables); err != nil {
		return nil, fmt.Errorf("variables must be a JSON object: %w", err)
	}
	if variables == nil {
		return nil, fmt.Errorf("variables must be a JSON object")
	}
	return variables, nil
}

func prepareOntologyQuery(cmdCtx context.Context, rawQuery string, variables map[string]any) (*ontologyQueryRuntime, *ontologyquery.PreparedQuery, func(), error) {
	vaultDef, err := vaultDefOrDefault()
	if err != nil {
		return nil, nil, nil, err
	}

	schema, execSchema, err := loadOntologyExecutableSchema(vaultDef.BasePath())
	if err != nil {
		return nil, nil, nil, err
	}
	prepared, errs := ontologyquery.PrepareWithVariables(execSchema, rawQuery, variables)
	if len(errs) > 0 {
		return &ontologyQueryRuntime{schema: schema, execSchema: execSchema}, nil, nil, ontologyQueryErrors(errs)
	}

	live, err := buildOntologyQueryLiveRuntime(cmdCtx, oneshotruntime.OntologyQueryPlan(prepared.UsesSemantic, prepared.UsesSearch))
	if err != nil {
		return nil, nil, nil, err
	}
	cleanup := func() { _ = live.Close() }

	noteReader := &obsidian.Note{}
	runtime, err := ontology.EnsureFreshRuntimeWithStore(cmdCtx, live.NoteMetadataIndexer(), live.VaultDef, noteReader, live.IntelStore())
	if err != nil {
		cleanup()
		return nil, nil, nil, err
	}
	if runtime == nil || runtime.Schema == nil || runtime.Store == nil {
		cleanup()
		return nil, nil, nil, fmt.Errorf("ontology is unavailable")
	}

	return &ontologyQueryRuntime{
		live:       live,
		runtime:    runtime,
		service:    ontology.NewService(live.VaultDef, noteReader, live.IntelStore(), runtime.Schema),
		noteReader: noteReader,
		schema:     runtime.Schema,
		execSchema: execSchema,
	}, prepared, cleanup, nil
}

// buildOntologyQueryLiveRuntime applies the request-derived plan after query
// preparation. Current projection intentionally remains a live, writer-owned
// operation; its one-shot options only remove background work and capabilities
// that cannot affect the compiled query result.
func buildOntologyQueryLiveRuntime(ctx context.Context, plan oneshotruntime.Plan) (*bootstrap.LiveRuntime, error) {
	runtime, err := oneshotruntime.Build(ctx, plan, func(buildCtx context.Context, requirements bootstrap.RuntimeRequirements) (oneshotruntime.Runtime, error) {
		live, err := bootstrap.NewLiveRuntime(buildCtx, ontologyQueryLiveOptions(requirements))
		if err != nil {
			return nil, err
		}
		return &liveRuntimePlanAdapter{LiveRuntime: live}, nil
	})
	if err != nil {
		return nil, err
	}
	if err := awaitOntologyQueryRuntime(ctx, runtime, plan); err != nil {
		runtime.Close()
		return nil, err
	}
	adapter, ok := runtime.(*liveRuntimePlanAdapter)
	if !ok || adapter.LiveRuntime == nil {
		if runtime != nil {
			runtime.Close()
		}
		return nil, fmt.Errorf("ontology one-shot runtime factory returned an incompatible runtime")
	}
	return adapter.LiveRuntime, nil
}

// awaitOntologyQueryRuntime keeps the baseline error identity at this
// machine-facing boundary. The generic one-shot await helper adds phase
// context, which would change existing CLI JSON error text.
func awaitOntologyQueryRuntime(ctx context.Context, runtime oneshotruntime.Runtime, plan oneshotruntime.Plan) error {
	for _, readiness := range plan.Readiness {
		var err error
		switch readiness {
		case oneshotruntime.ReadinessCodeIndex:
			err = runtime.WaitForCodeIndex(ctx)
		case oneshotruntime.ReadinessSemantic:
			err = runtime.WaitForSemantic(ctx)
		default:
			return fmt.Errorf("ontology query has unsupported readiness %q", readiness)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func ontologyQueryLiveOptions(requirements bootstrap.RuntimeRequirements) bootstrap.LiveOptions {
	return bootstrap.LiveOptions{
		VaultName:           vaultName,
		Debug:               debug,
		SkipCacheWarmup:     true,
		DisableWatchHub:     true,
		DisableLeaderWork:   true,
		DisableSessionStore: true,
		// Current projection is a live, writer-owned operation. Preserve the
		// baseline metadata initialization/reset behavior for semantic queries.
		QueryProvidersOnly: false,
		Requirements:       requirements,
	}
}

func (rt *ontologyQueryRuntime) deps() ontologyquery.Deps {
	deps := ontologyquery.Deps{
		VaultDef:   rt.live.VaultDef,
		NoteReader: rt.noteReader,
		Store:      rt.runtime.Store,
		Service:    rt.service,
	}
	if rt.noteFormatRuntime != nil {
		deps.NoteFormats = *rt.noteFormatRuntime
	} else if formats, err := rt.live.NoteMetadataIndexer().FormatRuntime(); err == nil {
		deps.NoteFormats = formats
	}
	if searcher := rt.semanticSearcher(); searcher != nil {
		deps.SemanticSearcher = searcher
	}
	if rt.live != nil {
		_, codeProvider := rt.live.CodeEmbeddings()
		deps.NoteSearcher = unifiedsearch.NoteSearcher{Runtime: unifiedsearch.Options{
			VaultPath:    rt.live.VaultDef.BasePath(),
			VaultDef:     rt.live.VaultDef,
			IntelStore:   rt.live.IntelStore(),
			NoteProvider: rt.live.NoteProvider(),
			CodeProvider: codeProvider,
		}}
	}
	deps.OntologyRuntime = ontologyQueryRuntimeProviders{rt: rt}
	deps.CodeRuntime = ontologyQueryRuntimeProviders{rt: rt}
	return deps
}

func (rt *ontologyQueryRuntime) semanticSearcher() ontologyquery.SemanticSearcher {
	if rt == nil || rt.live == nil {
		return nil
	}
	intelStore := rt.live.IntelStore()
	noteProvider := rt.live.NoteProvider()
	if intelStore == nil || noteProvider == nil {
		return nil
	}
	return &semantic.Searcher{
		NoteProvider: noteProvider,
		IntelStore:   intelStore,
	}
}

func ontologyQueryErrors(errs []ontologyquery.Error) error {
	if len(errs) == 0 {
		return nil
	}
	data, err := json.Marshal(map[string]any{"errors": errs})
	if err != nil {
		return fmt.Errorf("ontology query failed")
	}
	return fmt.Errorf("%s", string(data))
}

type ontologyQueryRuntimeProviders struct {
	rt *ontologyQueryRuntime
}

func (p ontologyQueryRuntimeProviders) AuthoringGuide(ctx context.Context, req ontologyquery.OntologyAuthoringGuideRequest) (ontologyquery.OntologyAuthoringGuide, error) {
	typeName := strings.TrimSpace(req.Type)
	if typeName == "" {
		return ontologyquery.OntologyAuthoringGuide{
			Type:      req.Type,
			Available: false,
			Warnings:  []ontologyquery.RuntimeWarning{{Code: "type_required", Message: "authoringGuide requires a non-empty type"}},
		}, nil
	}
	if p.rt == nil || p.rt.schema == nil {
		return ontologyquery.OntologyAuthoringGuide{
			Type:      req.Type,
			Available: false,
			Warnings:  []ontologyquery.RuntimeWarning{{Code: "ontology_runtime_unavailable", Message: "ontology schema is unavailable"}},
		}, nil
	}
	requested := []string{typeName}
	lookup := guide.IDLookup(func(typeName string) (string, bool, error) {
		if p.rt.runtime == nil || p.rt.runtime.Store == nil {
			return "", false, nil
		}
		res, err := idalloc.Allocate(ctx, p.rt.schema, p.rt.runtime.Store, typeName)
		if err != nil {
			return "", false, nil
		}
		return res.Next, true, nil
	})
	text, err := guide.RenderMarkdownWithLookups(p.rt.schema, requested, ontologyCompanionResolver(p.rt.noteReader), lookup)
	if err != nil {
		return ontologyquery.OntologyAuthoringGuide{}, err
	}
	return ontologyquery.OntologyAuthoringGuide{
		Type:      typeName,
		Markdown:  text,
		Available: true,
	}, nil
}

func (p ontologyQueryRuntimeProviders) NextID(ctx context.Context, request ontologyquery.OntologyNextIDRequest) (ontologyquery.OntologyNextID, error) {
	typeName := strings.TrimSpace(request.Type)
	if p.rt == nil || p.rt.schema == nil || p.rt.runtime == nil || p.rt.runtime.Store == nil {
		return ontologyquery.OntologyNextID{
			Type:      typeName,
			Available: false,
			ErrorCode: "runtime_unavailable",
			Error:     "ontology runtime store is unavailable",
			Warnings:  []ontologyquery.RuntimeWarning{{Code: "runtime_unavailable", Message: "ontology runtime store is unavailable"}},
		}, nil
	}
	res, err := idalloc.AllocateRequest(ctx, p.rt.schema, p.rt.runtime.Store, idalloc.Request{
		Type:  typeName,
		Count: request.Count,
		Paths: request.Paths,
	})
	if err != nil {
		code := idalloc.ErrorCode(err)
		next := ontologyquery.OntologyNextID{
			Type:      typeName,
			Available: false,
			ErrorCode: code,
			Error:     err.Error(),
			Warnings:  []ontologyquery.RuntimeWarning{{Code: code, Message: err.Error()}},
		}
		if res != nil {
			next.IdentifierField = res.IdentifierField
			next.Strategy = res.Strategy
			next.Source = res.Source
			next.Prefix = res.Prefix
			next.Separator = res.Separator
			next.Pad = res.Pad
			next.Count = res.Count
			next.Paths = res.Paths
		}
		return next, nil
	}
	return ontologyquery.OntologyNextID{
		Type:            res.Type,
		IdentifierField: res.IdentifierField,
		Strategy:        res.Strategy,
		Source:          res.Source,
		Prefix:          res.Prefix,
		Separator:       res.Separator,
		Pad:             res.Pad,
		Next:            res.Next,
		IDs:             res.IDs,
		Count:           res.Count,
		Last:            res.Last,
		CurrentMax:      res.CurrentMax,
		CurrentMaxValue: res.CurrentMaxValue,
		CurrentMaxOwner: res.CurrentMaxOwner,
		OwnersScanned:   res.OwnersScanned,
		SharedWith:      res.SharedWith,
		OwnersMatched:   res.OwnersMatched,
		OwnersSkipped:   res.OwnersSkipped,
		Notes:           res.Notes,
		Paths:           res.Paths,
		Allocations:     ontologyIDAllocations(res.Allocations),
		Available:       true,
	}, nil
}

func ontologyIDAllocations(values []idalloc.Allocation) []ontologyquery.OntologyIDAllocation {
	out := make([]ontologyquery.OntologyIDAllocation, 0, len(values))
	for _, value := range values {
		out = append(out, ontologyquery.OntologyIDAllocation{
			Path:          value.Path,
			ID:            value.ID,
			Base:          value.Base,
			Disambiguator: value.Disambiguator,
		})
	}
	return out
}

func (p ontologyQueryRuntimeProviders) CurrentUser(ctx context.Context) (ontologyquery.OntologyCurrentUser, error) {
	if p.rt == nil || p.rt.live == nil {
		return ontologyquery.OntologyCurrentUser{
			Configured: false,
			Found:      false,
			ErrorCode:  "runtime_unavailable",
			Error:      "ontology runtime is unavailable",
			Warnings:   []ontologyquery.RuntimeWarning{{Code: "runtime_unavailable", Message: "ontology runtime is unavailable"}},
		}, nil
	}
	result, err := resolveCurrentUserWithRuntime(ctx, p.rt)
	if err != nil {
		return ontologyquery.OntologyCurrentUser{}, err
	}
	return ontologyCurrentUserFromIdentity(result), nil
}

func resolveCurrentUserWithRuntime(ctx context.Context, rt *ontologyQueryRuntime) (identity.Resolution, error) {
	if rt == nil || rt.live == nil {
		return identity.Resolution{}, fmt.Errorf("ontology runtime is unavailable")
	}
	service := actions.CurrentUserService{Lookup: func(ctx context.Context, ref string) (actions.CurrentUserLookup, error) {
		return currentUserLookupWithRuntime(ctx, rt, ref)
	}}
	return service.Validate(ctx, rt.live.VaultDef.BasePath())
}

func currentUserLookupWithRuntime(ctx context.Context, rt *ontologyQueryRuntime, ref string) (actions.CurrentUserLookup, error) {
	prepared, errs := ontologyquery.PrepareWithVariables(rt.execSchema, actions.CurrentUserLookupQuery, map[string]any{"ref": ref})
	if len(errs) > 0 {
		return actions.CurrentUserLookup{}, ontologyQueryErrors(errs)
	}
	result := ontologyquery.ExecutePrepared(ctx, rt.deps(), rt.schema, rt.execSchema, prepared)
	return actions.CurrentUserLookupFromQueryResult(result)
}

func ontologyCurrentUserFromIdentity(result identity.Resolution) ontologyquery.OntologyCurrentUser {
	warnings := []ontologyquery.RuntimeWarning{}
	if result.ErrorCode != "" {
		warnings = append(warnings, ontologyquery.RuntimeWarning{Code: result.ErrorCode, Message: result.Error, Path: result.Path})
	}
	return ontologyquery.OntologyCurrentUser{
		Configured:  result.Configured,
		Ref:         result.Ref,
		Found:       result.Found,
		ResolvedRef: result.ResolvedRef,
		Path:        result.Path,
		Title:       result.Title,
		TypeName:    result.TypeName,
		ErrorCode:   result.ErrorCode,
		Error:       result.Error,
		Warnings:    warnings,
	}
}

func (p ontologyQueryRuntimeProviders) DocsForCode(ctx context.Context, req ontologyquery.CodeRuntimeRequest) (ontologyquery.CodeContextPack, error) {
	fileCtx, rel, err := p.fileContext(ctx, req.Path, req.First)
	if err != nil {
		return ontologyquery.CodeContextPack{}, err
	}
	return fileContextPack(req.Path, rel, fileCtx, req.First), nil
}

func (p ontologyQueryRuntimeProviders) CodeForNote(ctx context.Context, req ontologyquery.CodeRuntimeRequest) (ontologyquery.CodeContextPack, error) {
	pack := ontologyquery.CodeContextPack{
		InputPath: req.Path,
		Available: true,
	}
	if p.rt == nil || p.rt.live == nil {
		pack.Available = false
		pack.Warnings = []ontologyquery.RuntimeWarning{{Code: "code_runtime_unavailable", Message: "code runtime is unavailable", Path: req.Path}}
		return pack, nil
	}
	noteRel, noteAbs, err := p.runtimeNotePath(req.Path)
	if err != nil {
		pack.Available = false
		pack.Warnings = []ontologyquery.RuntimeWarning{{Code: "invalid_note_path", Message: err.Error(), Path: req.Path}}
		return pack, nil
	}
	pack.NormalizedPath = noteRel
	limit := req.First
	if limit <= 0 {
		limit = 20
	}

	seen := map[string]struct{}{}
	appendPath := func(item ontologyquery.RuntimePath) {
		if len(pack.Code) >= limit || item.Path == "" {
			return
		}
		key := item.Kind + "\x00" + item.Path
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		pack.Code = append(pack.Code, item)
	}

	if svc := p.rt.live.CodeAnchorService(); svc != nil {
		files, err := svc.CodeFilesForNote(ctx, pack.NormalizedPath)
		if err != nil {
			return ontologyquery.CodeContextPack{}, err
		}
		for _, file := range files {
			appendPath(ontologyquery.RuntimePath{Path: file, Kind: "file", Reason: "code-anchor-scope"})
		}

		anchors, err := svc.AnchorsByNotePaths(ctx, []string{pack.NormalizedPath})
		if err != nil {
			return ontologyquery.CodeContextPack{}, err
		}
		for _, anchor := range anchors[pack.NormalizedPath] {
			for _, item := range runtimePathsForAnchor(anchor) {
				appendPath(item)
			}
		}
	}

	if len(pack.Code) == 0 {
		content, err := os.ReadFile(noteAbs)
		if err != nil {
			pack.Warnings = append(pack.Warnings, ontologyquery.RuntimeWarning{Code: "note_read_failed", Message: err.Error(), Path: pack.NormalizedPath})
			return pack, nil
		}
		format, err := p.runtimeNoteFormat(pack.NormalizedPath)
		if err != nil {
			pack.Warnings = append(pack.Warnings, ontologyquery.RuntimeWarning{Code: "note_format_unavailable", Message: err.Error(), Path: pack.NormalizedPath})
			return pack, nil
		}
		note, markdown, err := markdownCodeAnchorParseCompat(pack.NormalizedPath, string(content), format)
		if !markdown {
			pack.Warnings = append(pack.Warnings, ontologyquery.RuntimeWarning{Code: "code_anchor_fallback_unsupported_format", Message: fmt.Sprintf("authored code-anchor fallback is not supported for note format %q", format), Path: pack.NormalizedPath})
			return pack, nil
		}
		if err != nil {
			pack.Warnings = append(pack.Warnings, ontologyquery.RuntimeWarning{Code: "note_anchor_parse_failed", Message: err.Error(), Path: pack.NormalizedPath})
			return pack, nil
		}
		for _, anchor := range note.DefinedAnchors {
			for _, item := range runtimePathsForAnchor(anchor) {
				appendPath(item)
			}
		}
		if len(pack.Code) > 0 {
			pack.Warnings = append(pack.Warnings, ontologyquery.RuntimeWarning{Code: "code_anchor_scope_unavailable", Message: "returned authored anchor targets; run code indexing to resolve exact files", Path: pack.NormalizedPath})
		} else {
			pack.Warnings = append(pack.Warnings, ontologyquery.RuntimeWarning{Code: "code_anchors_not_found", Message: "note has no code anchor targets; run fallback discovery if code evidence is required", Path: pack.NormalizedPath})
		}
	}
	return pack, nil
}

func (p ontologyQueryRuntimeProviders) runtimeNoteFormat(notePath string) (noteformat.FormatID, error) {
	if p.rt == nil || p.rt.live == nil {
		return "", fmt.Errorf("code runtime is unavailable")
	}
	var runtime noteformat.Runtime
	if p.rt.noteFormatRuntime != nil {
		runtime = *p.rt.noteFormatRuntime
	} else {
		var err error
		runtime, err = p.rt.live.NoteMetadataIndexer().FormatRuntime()
		if err != nil {
			// Format selection is provider authority. A missing runtime cannot
			// authorize Markdown parsing from a path or source bytes.
			return "", fmt.Errorf("note format runtime is unavailable: %w", err)
		}
	}
	vaultPaths, err := paths.NewVaultPaths(p.rt.live.VaultDef.BasePath())
	if err != nil {
		return "", err
	}
	selector, err := noteownership.CompileSelector(noteownership.SelectorInput{
		VaultDefinition: p.rt.live.VaultDef,
		Registry:        runtime.Registry(),
		CodeRoots:       []paths.AbsPath{paths.AbsPath(vaultPaths.Root())},
		CodeLanguage: func(ref paths.CodePathRef) codeanchor.Lang {
			return codeanchor.Lang(coderefs.DetectLanguage(ref.Rel.String()))
		},
	})
	if err != nil {
		return "", err
	}
	selection, err := selector.Select(paths.RelPath(notePath))
	if err != nil {
		return "", err
	}
	if selection.Owner != notediscovery.Note {
		return "", fmt.Errorf("path %q is not a configured note", notePath)
	}
	return selection.Provider, nil
}

func (p ontologyQueryRuntimeProviders) TestsForCode(ctx context.Context, req ontologyquery.CodeRuntimeRequest) (ontologyquery.CodeContextPack, error) {
	_ = ctx
	pack := ontologyquery.CodeContextPack{
		InputPath:      req.Path,
		NormalizedPath: strings.TrimSpace(req.Path),
		Available:      true,
	}
	if p.rt == nil || p.rt.live == nil {
		pack.Available = false
		pack.Warnings = []ontologyquery.RuntimeWarning{{Code: "code_runtime_unavailable", Message: "code runtime is unavailable", Path: req.Path}}
		return pack, nil
	}
	codeRel, _, err := p.runtimeCodePath(req.Path)
	if err != nil {
		pack.Available = false
		pack.Warnings = []ontologyquery.RuntimeWarning{{Code: "invalid_code_path", Message: err.Error(), Path: req.Path}}
		return pack, nil
	}
	pack.NormalizedPath = codeRel
	candidates := testPathCandidates(codeRel)
	limit := req.First
	if limit <= 0 {
		limit = 20
	}
	for _, candidate := range candidates {
		if len(pack.Tests) >= limit {
			break
		}
		_, abs, err := p.runtimeCodePath(candidate)
		if err != nil {
			continue
		}
		if _, err := os.Stat(abs); err == nil {
			pack.Tests = append(pack.Tests, ontologyquery.RuntimePath{Path: candidate, Kind: "test", Reason: "path-convention"})
		}
	}
	return pack, nil
}

func (p ontologyQueryRuntimeProviders) fileContext(ctx context.Context, rawPath string, limit int) (actions.FileContext, string, error) {
	if p.rt == nil || p.rt.live == nil {
		return actions.FileContext{}, "", fmt.Errorf("code runtime is unavailable")
	}
	vaultRoot := p.rt.live.VaultDef.BasePath()
	rel, abs, err := p.runtimeCodePath(rawPath)
	if err != nil {
		return actions.FileContext{}, "", err
	}
	budget := 12000
	if limit > 0 && limit < 20 {
		budget = 6000
	}
	fileCtx, err := actions.BuildFileContext(abs, actions.FileContextParams{
		Context:           ctx,
		IndexedReadOnly:   true,
		VaultDef:          p.rt.live.VaultDef,
		ProjectRoot:       vaultRoot,
		CodeRefRoot:       vaultRoot,
		ContextBudget:     budget,
		NoteReader:        p.rt.noteReader,
		CodeAnchor:        p.rt.live.CodeAnchorService(),
		SkipAnchorRefresh: true,
		SessionStore:      p.rt.live.IntelStore(),
		NodeReadScope:     nil,
		SubmoduleDepth:    1,
	})
	if err != nil {
		return actions.FileContext{}, "", err
	}
	return fileCtx, rel, nil
}

func (p ontologyQueryRuntimeProviders) runtimeCodePath(rawPath string) (string, string, error) {
	if p.rt == nil || p.rt.live == nil {
		return "", "", fmt.Errorf("code runtime is unavailable")
	}
	vaultRoot := p.rt.live.VaultDef.BasePath()
	vaultPaths, _ := paths.NewVaultPaths(vaultRoot)
	rel, abs, err := paths.ResolveCodeInputWithVaultPaths(vaultPaths, rawPath)
	if err != nil {
		return "", "", fmt.Errorf("code path must be inside the vault: %w", err)
	}
	return rel.String(), abs.String(), nil
}

func (p ontologyQueryRuntimeProviders) runtimeNotePath(rawPath string) (string, string, error) {
	if p.rt == nil || p.rt.live == nil {
		return "", "", fmt.Errorf("code runtime is unavailable")
	}
	vaultRoot := p.rt.live.VaultDef.BasePath()
	vaultPaths, _ := paths.NewVaultPaths(vaultRoot)
	rel, abs, err := paths.ResolveNotePathInputWithVaultPaths(vaultPaths, rawPath)
	if err != nil {
		return "", "", fmt.Errorf("note path must be inside the vault: %w", err)
	}
	return rel.String(), abs.String(), nil
}

func fileContextPack(inputPath, normalizedPath string, ctx actions.FileContext, limit int) ontologyquery.CodeContextPack {
	if limit <= 0 {
		limit = 20
	}
	pack := ontologyquery.CodeContextPack{
		InputPath:      inputPath,
		NormalizedPath: normalizedPath,
		Available:      true,
	}
	for _, doc := range ctx.AncestorDocs {
		if len(pack.Docs) >= limit {
			break
		}
		pack.Docs = append(pack.Docs, ontologyquery.RuntimePath{Path: doc.Path, Kind: "ancestor_doc", Content: doc.Content, Truncated: doc.Truncated})
	}
	for _, doc := range ctx.SubmoduleDocs {
		if len(pack.Docs) >= limit {
			break
		}
		pack.Docs = append(pack.Docs, ontologyquery.RuntimePath{Path: doc.Path, Kind: "submodule_doc", Content: doc.Content, Truncated: doc.Truncated})
	}
	for _, note := range ctx.LinkedNotes {
		if len(pack.Notes) >= limit {
			break
		}
		pack.Notes = append(pack.Notes, ontologyquery.RuntimePath{Path: note.Path, Title: note.Title, Kind: note.Kind, Snippet: note.Snippet, Line: note.Line})
	}
	if ctx.Truncated {
		pack.Warnings = append(pack.Warnings, ontologyquery.RuntimeWarning{Code: "truncated", Message: "file context was truncated", Path: normalizedPath})
	}
	return pack
}

func runtimePathsForAnchor(anchor codeanchor.Anchor) []ontologyquery.RuntimePath {
	var out []ontologyquery.RuntimePath
	reason := string(anchor.Kind)
	if anchor.Label != "" {
		reason = anchor.Label
	}
	if anchor.PathPrefix != "" {
		out = append(out, ontologyquery.RuntimePath{Path: anchor.PathPrefix, Kind: string(anchor.Kind), Reason: reason})
	}
	for _, glob := range anchor.Globs {
		out = append(out, ontologyquery.RuntimePath{Path: glob, Kind: string(anchor.Kind), Reason: reason})
	}
	if anchor.BaseSym != nil {
		path := strings.TrimPrefix(anchor.BaseSym.Pkg+"."+anchor.BaseSym.Name, ".")
		out = append(out, ontologyquery.RuntimePath{Path: path, Kind: string(anchor.Kind), Reason: reason})
	}
	if anchor.Ann != nil {
		path := strings.TrimPrefix(anchor.Ann.Symbol.Pkg+"."+anchor.Ann.Symbol.Name, ".")
		out = append(out, ontologyquery.RuntimePath{Path: path, Kind: string(anchor.Kind), Reason: reason})
	}
	return out
}

func testPathCandidates(rel string) []string {
	rel = strings.TrimSpace(filepath.ToSlash(rel))
	if rel == "" {
		return nil
	}
	ext := filepath.Ext(rel)
	stem := strings.TrimSuffix(rel, ext)
	dir := filepath.Dir(rel)
	base := filepath.Base(stem)
	candidates := []string{
		stem + "_test" + ext,
		filepath.ToSlash(filepath.Join(dir, base+".test"+ext)),
		filepath.ToSlash(filepath.Join(dir, base+".spec"+ext)),
	}
	if strings.HasPrefix(dir, "pkg/") {
		candidates = append(candidates, filepath.ToSlash(filepath.Join(dir, "tests", base+"_test"+ext)))
	}
	sort.Strings(candidates)
	return uniquePathCandidates(candidates)
}

func uniquePathCandidates(candidates []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(filepath.ToSlash(candidate))
		if candidate == "." || candidate == "" {
			continue
		}
		cleaned := filepath.ToSlash(filepath.Clean(candidate))
		if cleaned == "." || cleaned == "" || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
			continue
		}
		candidate = cleaned
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
	}
	return out
}
