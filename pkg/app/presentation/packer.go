// Package presentation renders search results into budgeted text output.
//
// Docs: [CONTEXT.md](pkg/app/presentation/CONTEXT.md)
//
// This package implements the Packer interface used by pkg/search.Service.
// DefaultPacker renders ranked results as numbered, indented text using
// pkg/app/contextpack for budget management.
package presentation

import (
	"context"
	"fmt"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/contextpack"
	"github.com/atomicobject/rhizome/pkg/indexingperf"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/noderead"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	codeindex "github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type Packer interface {
	Pack(ctx context.Context, spec search.QuerySpec, results []search.RankedResult) (search.PackedContext, error)
}

// DefaultPacker renders a compact, budgeted text output using pkg/contextpack.
// It stays intentionally simple so multiple retrieval surfaces share one packer.
type DefaultPacker struct {
	VaultPath  string
	CodeRoot   string
	Intel      IntelReader
	CodeIndex  ChunkBodyReader // Optional: if provided, uses FTS chunk body instead of reading files
	UseFTSBody bool            // If true, prefer FTS body over file reads

	VaultDef       obsidian.VaultDefinition
	NoteReader     obsidian.NoteReader
	OntologySchema *ontology.Schema
}

type IntelReader interface {
	IntelAnchorByID(ctx context.Context, anchorID string) (codeanchor.IntelAnchor, bool, error)
}

// ChunkBodyReader retrieves chunk body text from FTS (includes doc comments).
// Accepts anchorID as string for flexibility; implementations should convert as needed.
type ChunkBodyReader interface {
	GetChunkBody(ctx context.Context, anchorID string, chunkIndex int) (string, error)
}

// codeIndexAdapter adapts codeindex.Index to ChunkBodyReader interface.
type codeIndexAdapter struct {
	idx codeindex.Index
}

func (a *codeIndexAdapter) GetChunkBody(ctx context.Context, anchorID string, chunkIndex int) (string, error) {
	return a.idx.GetChunkBody(ctx, codeindex.AnchorID(anchorID), chunkIndex)
}

// AdaptCodeIndex wraps a codeindex.Index to satisfy ChunkBodyReader.
func AdaptCodeIndex(idx codeindex.Index) ChunkBodyReader {
	if idx == nil {
		return nil
	}
	return &codeIndexAdapter{idx: idx}
}

func (p *DefaultPacker) Pack(ctx context.Context, spec search.QuerySpec, results []search.RankedResult) (search.PackedContext, error) {
	// Docs: [[search-answer-workflow#^spec-0034-us4-ac1]] and [[unified-search-answer-architecture#^spec-0035-us3-ac2]].
	// WHY: presentation may hydrate already-ranked snippets, but it must not broaden retrieval; ranking and answer role selection stay upstream/downstream.
	if ctx.Err() != nil {
		return search.PackedContext{}, ctx.Err()
	}

	budget := spec.Budget.Chars
	if budget <= 0 {
		budget = contextpack.DefaultBudgetChars
	}

	headerText := fmt.Sprintf("Query: %s\nResults: %d", strings.TrimSpace(spec.Text), len(results))
	pieces := make([]contextpack.Piece, 0, 1+len(results))
	pieces = append(pieces, contextpack.Piece{
		Key:      "header",
		Priority: 1000,
		Score:    1,
		Text:     headerText,
	})

	chunkCache := make(map[string][]embeddings.ChunkInput)
	var ontologyScope *noderead.Scope
	if p != nil && p.OntologySchema != nil && p.NoteReader != nil {
		var store noderead.Store
		if s, ok := p.Intel.(noderead.Store); ok {
			store = s
		}
		ontologyScope = noderead.NewService(p.VaultDef, p.NoteReader, store, p.OntologySchema).NewScope(ctx, noderead.ScopeOptions{})
	}
	perResultBudget := budget
	if len(results) > 0 {
		remaining := budget - len(headerText) - 8
		if remaining < 120 {
			remaining = budget - len(headerText)
		}
		if remaining > 0 {
			perResultBudget = remaining / len(results)
			if perResultBudget < 120 {
				perResultBudget = remaining
			}
		}
	}
	for i, r := range results {
		if ctx.Err() != nil {
			return search.PackedContext{}, ctx.Err()
		}
		pieces = append(pieces, renderResultPiece(ctx, p, ontologyScope, i, r, chunkCache, perResultBudget))
	}

	if err := ctx.Err(); err != nil {
		return search.PackedContext{}, err
	}

	text, meta := contextpack.Pack(pieces, budget)
	return search.PackedContext{
		Text: text,
		Meta: map[string]any{
			"budgetRequested": meta.BudgetRequested,
			"budgetUsed":      meta.BudgetUsed,
			"trimmed":         meta.Trimmed,
			"includedPieces":  meta.IncludedPieces,
			"omittedPieces":   meta.OmittedPieces,
		},
	}, nil
}

func renderResultPiece(ctx context.Context, p *DefaultPacker, ontologyScope *noderead.Scope, rank int, r search.RankedResult, chunkCache map[string][]embeddings.ChunkInput, resultBudget int) contextpack.Piece {
	scoreStr := fmt.Sprintf("%.1f%%", r.FinalScore*100)
	key := fmt.Sprintf("result:%d:%s", rank, r.Handle.String())

	var body string
	switch r.Type {
	case "note":
		title := firstNonEmpty(r.Title, r.NoteID, r.Path, r.Handle.String())
		matchLabel := reverseBreadcrumb(firstNonEmpty(r.Breadcrumb, r.Heading))
		notePath := r.NoteID
		if notePath == "" && r.NodeRef == nil && r.NodeRefJSON == "" && r.NodeID == "" {
			notePath = r.Path
		}
		snippet := noteChunkText(ctx, p.VaultPath, notePath, r.ChunkIndex, chunkCache)
		ontologyBudget := resultBudget - 160
		if ontologyBudget < 120 {
			ontologyBudget = 120
		}
		if ontologyContext := renderOntologyNodeContext(ctx, p, ontologyScope, r, ontologyBudget); ontologyContext != "" {
			snippet = strings.TrimSpace(strings.Join(nonEmptyStrings(snippet, ontologyContext), "\n\n"))
		}
		body = fmt.Sprintf("%2d. %s [note] %s (%s)\n%s", rank+1, scoreStr, title, matchLabel, indent(snippet, "    "))
	case "code":
		label := firstNonEmpty(r.Symbol, r.FQN, r.Path, r.Handle.String())
		matchLabel := reverseBreadcrumb(firstNonEmpty(r.Breadcrumb, r.Heading))
		snippet := ""
		// Prefer FTS chunk body if available and requested (includes doc comments, avoids file I/O)
		if p.UseFTSBody && p.CodeIndex != nil && strings.TrimSpace(r.AnchorID) != "" && r.ChunkIndex >= 0 {
			bodyText, err := p.CodeIndex.GetChunkBody(ctx, r.AnchorID, r.ChunkIndex)
			if err == nil && strings.TrimSpace(bodyText) != "" {
				snippet = renderChunkBody(bodyText, strings.EqualFold(r.Kind, "module") || strings.EqualFold(r.Granularity, "module"))
			}
		}
		// Fallback to file-based rendering
		if snippet == "" && p.Intel != nil && strings.TrimSpace(r.AnchorID) != "" && strings.TrimSpace(p.CodeRoot) != "" {
			if a, ok, err := p.Intel.IntelAnchorByID(ctx, r.AnchorID); err == nil && ok {
				snippet = renderCodeSpan(ctx, p.CodeRoot, a, strings.EqualFold(r.Kind, "module") || strings.EqualFold(r.Granularity, "module"))
			}
		}
		if snippet != "" {
			body = fmt.Sprintf("%2d. %s [code] %s (%s)\n    %s\n%s", rank+1, scoreStr, label, matchLabel, r.Path, indent(snippet, "    "))
		} else {
			body = fmt.Sprintf("%2d. %s [code] %s (%s)\n    %s", rank+1, scoreStr, label, matchLabel, r.Path)
		}
	default:
		label := firstNonEmpty(r.Title, r.Symbol, r.FQN, r.Path, r.Handle.String())
		snippet := bestSnippet(r.Evidence)
		if snippet != "" {
			body = fmt.Sprintf("%2d. %s [%s] %s\n    %s\n    %s", rank+1, scoreStr, r.Type, label, r.Path, snippet)
		} else {
			body = fmt.Sprintf("%2d. %s [%s] %s\n    %s", rank+1, scoreStr, r.Type, label, r.Path)
		}
	}

	return contextpack.Piece{
		Key:      key,
		Priority: 900 - rank,
		Score:    r.FinalScore,
		Text:     strings.TrimSpace(body),
	}
}

func bestSnippet(evidence []search.Evidence) string {
	for _, ev := range evidence {
		if ev.Details == nil {
			continue
		}
		if s := strings.TrimSpace(ev.Details["snippet"]); s != "" {
			return s
		}
	}
	return ""
}

func noteChunkText(ctx context.Context, vaultPath, noteID string, chunkIndex int, cache map[string][]embeddings.ChunkInput) string {
	if vaultPath == "" || noteID == "" || chunkIndex < 0 {
		return ""
	}
	cleaned, err := paths.CleanNotePath(noteID)
	if err != nil || cleaned == "" {
		return ""
	}
	noteID = cleaned.String()
	txt, bytesRead, didRead := embeddings.ChunkTextForPathObserved(vaultPath, noteID, chunkIndex, cache)
	if didRead {
		indexingperf.AddCount(ctx, indexingperf.SemanticQueryOpBodyReads, 1)
		indexingperf.AddCount(ctx, indexingperf.SemanticQueryOpBodyReadBytes, bytesRead)
	}
	return strings.TrimSpace(embeddings.CoreChunkBody(txt))
}

func indent(s, prefix string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func reverseBreadcrumb(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	parts := strings.Split(s, " > ")
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, " > ")
}
