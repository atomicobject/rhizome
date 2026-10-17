package runtimeproviders

import (
	"context"
	"fmt"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/codeintel"
	actions "github.com/atomicobject/rhizome/pkg/app/operations"
	"github.com/atomicobject/rhizome/pkg/diag"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/paths"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (p Providers) DocsForCode(ctx context.Context, req ontologyquery.CodeRuntimeRequest) (ontologyquery.CodeContextPack, error) {
	fileCtx, rel, err := p.fileContext(ctx, req.Path, req.First)
	if err != nil {
		return ontologyquery.CodeContextPack{}, err
	}
	return fileContextPack(req.Path, rel, fileCtx, req.First), nil
}
func (p Providers) CodeForNote(ctx context.Context, req ontologyquery.CodeRuntimeRequest) (ontologyquery.CodeContextPack, error) {
	pack := ontologyquery.CodeContextPack{InputPath: req.Path, Available: true}
	if p.deps.VaultDef.BasePath() == "" {
		pack.Available = false
		pack.Warnings = []diag.Warning{{Code: "code_runtime_unavailable", Message: "code runtime is unavailable", Path: req.Path}}
		return pack, nil
	}
	noteRel, _, err := p.runtimeNotePath(req.Path)
	if err != nil {
		pack.Available = false
		pack.Warnings = []diag.Warning{{Code: "invalid_note_path", Message: err.Error(), Path: req.Path}}
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
	if svc := p.anchors; svc != nil {
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
		anchors, warning := codeintel.ReadAuthoredMarkdownAnchors(p.deps.VaultDef, p.deps.NoteFormats, pack.NormalizedPath)
		if warning != nil {
			pack.Warnings = append(pack.Warnings, *warning)
			return pack, nil
		}
		for _, anchor := range anchors {
			for _, item := range runtimePathsForAnchor(anchor) {
				appendPath(item)
			}
		}
		if len(pack.Code) > 0 {
			pack.Warnings = append(pack.Warnings, diag.Warning{Code: "code_anchor_scope_unavailable", Message: "returned authored anchor targets; run code indexing to resolve exact files", Path: pack.NormalizedPath})
		} else {
			pack.Warnings = append(pack.Warnings, diag.Warning{Code: "code_anchors_not_found", Message: "note has no code anchor targets; run fallback discovery if code evidence is required", Path: pack.NormalizedPath})
		}
	}
	return pack, nil
}
func (p Providers) TestsForCode(ctx context.Context, req ontologyquery.CodeRuntimeRequest) (ontologyquery.CodeContextPack, error) {
	_ = ctx
	pack := ontologyquery.CodeContextPack{InputPath: req.Path, NormalizedPath: strings.TrimSpace(req.Path), Available: true}
	if p.deps.VaultDef.BasePath() == "" {
		pack.Available = false
		pack.Warnings = []diag.Warning{{Code: "code_runtime_unavailable", Message: "code runtime is unavailable", Path: req.Path}}
		return pack, nil
	}
	codeRel, _, err := p.runtimeCodePath(req.Path)
	if err != nil {
		pack.Available = false
		pack.Warnings = []diag.Warning{{Code: "invalid_code_path", Message: err.Error(), Path: req.Path}}
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
func (p Providers) fileContext(ctx context.Context, rawPath string, limit int) (actions.FileContext, string, error) {
	if p.deps.VaultDef.BasePath() == "" {
		return actions.FileContext{}, "", fmt.Errorf("code runtime is unavailable")
	}
	vaultRoot := p.deps.VaultDef.BasePath()
	rel, abs, err := p.runtimeCodePath(rawPath)
	if err != nil {
		return actions.FileContext{}, "", err
	}
	budget := 12000
	if limit > 0 && limit < 20 {
		budget = 6000
	}
	fileCtx, err := actions.BuildFileContext(abs, actions.FileContextParams{Context: ctx, IndexedReadOnly: true, VaultDef: p.deps.VaultDef, ProjectRoot: vaultRoot, CodeRefRoot: vaultRoot, ContextBudget: budget, NoteReader: p.deps.NoteReader, CodeAnchor: p.anchors, SkipAnchorRefresh: true, SessionStore: p.store, NodeReadScope: nil, SubmoduleDepth: 1})
	if err != nil {
		return actions.FileContext{}, "", err
	}
	return fileCtx, rel, nil
}
func (p Providers) runtimeCodePath(rawPath string) (string, string, error) {
	if p.deps.VaultDef.BasePath() == "" {
		return "", "", fmt.Errorf("code runtime is unavailable")
	}
	vaultRoot := p.deps.VaultDef.BasePath()
	vaultPaths, _ := paths.NewVaultPaths(vaultRoot)
	rel, abs, err := paths.ResolveCodeInputWithVaultPaths(vaultPaths, rawPath)
	if err != nil {
		return "", "", fmt.Errorf("code path must be inside the vault: %w", err)
	}
	return rel.String(), abs.String(), nil
}
func (p Providers) runtimeNotePath(rawPath string) (string, string, error) {
	if p.deps.VaultDef.BasePath() == "" {
		return "", "", fmt.Errorf("code runtime is unavailable")
	}
	vaultRoot := p.deps.VaultDef.BasePath()
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
	pack := ontologyquery.CodeContextPack{InputPath: inputPath, NormalizedPath: normalizedPath, Available: true}
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
		pack.Warnings = append(pack.Warnings, diag.Warning{Code: "truncated", Message: "file context was truncated", Path: normalizedPath})
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
	candidates := []string{stem + "_test" + ext, filepath.ToSlash(filepath.Join(dir, base+".test"+ext)), filepath.ToSlash(filepath.Join(dir, base+".spec"+ext))}
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
