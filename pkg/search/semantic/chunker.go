// Package semantic provides unified semantic chunking + vector retrieval for search.
//
// The primary user-facing retrieval entry point is Searcher.Search (see search.go), which:
//  1. Embeds the query text (code/note providers may differ)
//  2. Runs vector similarity search against the unified intel_embeddings store
//  3. Returns code anchors and note doc sections as unified Results
//
// This file focuses on *chunk synthesis* for code embeddings: ChunkBuilder turns intel
// anchors into deterministic chunks (module + symbol) bounded by ChunkBudget.
//
// Docs: [Search (Hub)](docs/hubs/Search (Hub).md), [Indexing pipeline - Live updating (watcher runtime)](docs/reference/analysis/Indexing pipeline - Live updating (watcher runtime).md)
package semantic

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/embeddings/codeindex"
)

// ChunkBudget constrains semantic chunk synthesis.
type ChunkBudget struct {
	TargetChars int
	MaxChars    int
	Overlap     int
}

// DefaultChunkBudget returns the default budgets aligned with note chunking.
func DefaultChunkBudget() ChunkBudget {
	return ChunkBudget{TargetChars: 1500, MaxChars: 2800, Overlap: 300}
}

// SemanticChunk captures synthesized chunk text alongside the persisted chunk metadata.
type SemanticChunk struct {
	Input codeindex.ChunkInput
	Text  string
}

// ChunkBuilder synthesizes semantic chunks for embedding from code anchors.
// It creates deterministic chunk text that includes:
//   - Structured metadata headers (kind, lang, path, symbol, FQN)
//   - Exported symbols and signatures (for module chunks)
//   - Doc comments and related doc titles (for symbol chunks)
//   - Body content split with overlap (for large symbols)
//
// The default budget (1500 target, 2800 max, 300 overlap) aligns with note chunking.
type ChunkBuilder struct {
	Budget ChunkBudget
}

// BuildModuleChunk constructs a single module chunk that summarizes a file and its exported symbols.
func (b ChunkBuilder) BuildModuleChunk(path string, lang codeanchor.Lang, anchors []codeanchor.IntelAnchor, contextLines []string) SemanticChunk {
	return b.buildModuleChunk(path, lang, anchors, contextLines, "")
}

func (b ChunkBuilder) buildModuleChunk(path string, lang codeanchor.Lang, anchors []codeanchor.IntelAnchor, contextLines []string, enrichment string) SemanticChunk {
	if b.Budget.TargetChars == 0 {
		b.Budget = DefaultChunkBudget()
	}
	type entry struct {
		kind      string
		symbol    string
		fqn       string
		signature string
		doc       string
	}
	// Extract package-level doc comment from module anchor if present.
	var packageDoc string
	for _, a := range anchors {
		if strings.EqualFold(a.Kind, "module") && strings.TrimSpace(a.DocComment) != "" {
			packageDoc = strings.TrimSpace(a.DocComment)
			break
		}
	}

	var entries []entry
	for _, a := range anchors {
		if strings.EqualFold(a.Kind, "module") {
			continue
		}
		if strings.TrimSpace(a.Symbol) == "" && strings.TrimSpace(a.FQN) == "" {
			continue
		}
		e := entry{
			kind:      strings.TrimSpace(a.Kind),
			symbol:    strings.TrimSpace(a.Symbol),
			fqn:       strings.TrimSpace(a.FQN),
			signature: strings.TrimSpace(a.Signature),
			doc:       strings.TrimSpace(a.DocComment),
		}
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].kind != entries[j].kind {
			return entries[i].kind < entries[j].kind
		}
		if entries[i].symbol != entries[j].symbol {
			return entries[i].symbol < entries[j].symbol
		}
		return entries[i].fqn < entries[j].fqn
	})

	exports := make([]string, 0, len(entries))
	fieldNames := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.symbol == "" {
			continue
		}
		if strings.EqualFold(e.kind, "field") {
			fieldNames = append(fieldNames, e.symbol)
			continue
		}
		exports = append(exports, e.symbol)
	}
	sort.Strings(exports)
	fieldNames = compactFieldNames(fieldNames, 40)

	signatures := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.EqualFold(e.kind, "field") {
			continue
		}
		line := strings.TrimSpace(e.signature)
		if line == "" {
			label := strings.TrimSpace(e.symbol)
			if label == "" {
				label = strings.TrimSpace(e.fqn)
			}
			line = strings.TrimSpace(strings.Join([]string{e.kind, label}, " "))
		}
		if line != "" {
			signatures = append(signatures, line)
		}
	}

	docSummaries := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.TrimSpace(e.doc) == "" {
			continue
		}
		label := strings.TrimSpace(e.symbol)
		if label == "" {
			label = strings.TrimSpace(e.fqn)
		}
		summary := docSummaryLine(e.doc)
		if label != "" && summary != "" {
			docSummaries = append(docSummaries, label+": "+summary)
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Kind: module\nLang: %s\nPath: %s\n", lang, path))
	if packageDoc != "" {
		sb.WriteString("\nPackage:\n")
		sb.WriteString(strings.TrimSpace(packageDoc))
		sb.WriteString("\n")
	}
	if len(exports) > 0 {
		sb.WriteString(fmt.Sprintf("Exports: %s\n", strings.Join(exports, ", ")))
	}
	if len(fieldNames) > 0 {
		sb.WriteString(fmt.Sprintf("Fields: %s\n", strings.Join(fieldNames, ", ")))
	}
	if len(signatures) > 0 {
		sb.WriteString("Signatures:\n")
		for _, sig := range signatures {
			if strings.TrimSpace(sig) == "" {
				continue
			}
			sb.WriteString("- " + strings.TrimSpace(sig) + "\n")
			if RuneLen(sb.String()) >= b.Budget.MaxChars {
				break
			}
		}
	}
	if len(docSummaries) > 0 {
		sb.WriteString("Docs:\n")
		for _, d := range docSummaries {
			d = strings.TrimSpace(d)
			if d == "" {
				continue
			}
			sb.WriteString("- " + d + "\n")
			if RuneLen(sb.String()) >= b.Budget.MaxChars {
				break
			}
		}
	}
	if len(contextLines) > 0 {
		sb.WriteString("Context:\n")
		for _, line := range contextLines {
			sb.WriteString("- " + strings.TrimSpace(line) + "\n")
		}
	}
	if enrichment = strings.TrimSpace(enrichment); enrichment != "" {
		sb.WriteString(enrichment)
		sb.WriteString("\n")
	}

	finalText := TrimToBudget(sb.String(), b.Budget.MaxChars)
	return SemanticChunk{
		Input: codeindex.ChunkInput{
			Index:       0,
			Granularity: "module",
			Breadcrumb:  filepath.ToSlash(path),
			Heading:     filepath.Base(path),
			Hash:        hashText(finalText),
		},
		Text: finalText,
	}
}

func compactFieldNames(names []string, limit int) []string {
	if len(names) == 0 || limit == 0 {
		return nil
	}
	if limit < 0 {
		limit = len(names)
	}
	seen := make(map[string]struct{}, len(names))
	unique := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		unique = append(unique, name)
	}
	sort.Slice(unique, func(i, j int) bool {
		iExported := startsExported(unique[i])
		jExported := startsExported(unique[j])
		if iExported != jExported {
			return iExported
		}
		return unique[i] < unique[j]
	})
	if len(unique) <= limit {
		return unique
	}
	truncated := append([]string(nil), unique[:limit]...)
	truncated = append(truncated, fmt.Sprintf("+%d more fields", len(unique)-limit))
	return truncated
}

func startsExported(name string) bool {
	for _, r := range name {
		return unicode.IsUpper(r)
	}
	return false
}

func docSummaryLine(doc string) string {
	doc = strings.TrimSpace(doc)
	if doc == "" {
		return ""
	}
	lines := strings.Split(doc, "\n")
	var first string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Basic cleanup for common comment syntaxes (best-effort; keep deterministic).
		line = strings.TrimPrefix(line, "//")
		line = strings.TrimPrefix(line, "#")
		line = strings.TrimPrefix(line, "*")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		first = line
		break
	}
	if first == "" {
		return ""
	}
	first = strings.ReplaceAll(first, "\t", " ")
	first = strings.Join(strings.Fields(first), " ")
	// Keep module docs compact/deterministic.
	return strings.TrimSpace(TrimToBudget(first, 160))
}

// BuildPrimaryAnchorChunk builds a deterministic primary chunk (Index=0) for a code anchor.
// granularity is a semantic label describing the representation (e.g. "signature_doc", "decl_full").
func (b ChunkBuilder) BuildPrimaryAnchorChunk(anchor codeanchor.IntelAnchor, granularity string, text string) SemanticChunk {
	if b.Budget.TargetChars == 0 {
		b.Budget = DefaultChunkBudget()
	}
	granularity = strings.TrimSpace(granularity)
	if granularity == "" {
		granularity = "symbol"
	}
	text = strings.TrimSpace(text)

	header := fmt.Sprintf("Kind: %s\nLang: %s\nSymbol: %s\nFQN: %s\nPath: %s\nRep: %s\n\n", anchor.Kind, anchor.Lang, anchor.Symbol, anchor.FQN, anchor.Path, granularity)
	full := TrimToBudget(header+text, b.Budget.MaxChars)

	return SemanticChunk{
		Input: codeindex.ChunkInput{
			Index:       0,
			Granularity: granularity,
			Breadcrumb:  anchor.Path,
			Heading:     anchor.Symbol,
			StartByte:   int(anchor.StartByte),
			EndByte:     int(anchor.EndByte),
			StartLine:   int(anchor.StartLine),
			EndLine:     int(anchor.EndLine),
			Hash:        hashText(full),
		},
		Text: full,
	}
}

// SymbolChunkContext is the prose that explains a symbol beyond its own doc
// comment: the owning module's summary and the inline rationale comments
// inside the symbol's span. Embedding it lets paraphrased questions reach code.
type SymbolChunkContext struct {
	ModuleDoc string                 // one-line summary of the file's package/module doc comment
	Rationale []codeanchor.Rationale // rationale comments whose lines fall inside the anchor span
}

// ModuleDocSummary returns the one-line summary of the module anchor's doc comment
// for a file's anchors, or "" when there is no module anchor or no doc comment.
func ModuleDocSummary(anchors []codeanchor.IntelAnchor) string {
	for _, a := range anchors {
		if strings.EqualFold(a.Kind, "module") {
			return docSummaryLine(a.DocComment)
		}
	}
	return ""
}

// RationaleInSpan selects the rationale comments that explain a single anchor:
// those whose lines fall inside the anchor's span, or — when the anchor has no
// line span — those attributed to its FQN. Results are returned in line order.
func RationaleInSpan(anchor codeanchor.IntelAnchor, rationale []codeanchor.Rationale) []codeanchor.Rationale {
	if len(rationale) == 0 {
		return nil
	}
	hasSpan := anchor.StartLine > 0 && anchor.EndLine >= anchor.StartLine
	fqn := strings.TrimSpace(anchor.FQN)
	var out []codeanchor.Rationale
	for _, r := range rationale {
		if hasSpan {
			if r.StartLine >= anchor.StartLine && r.EndLine <= anchor.EndLine {
				out = append(out, r)
			}
			continue
		}
		if fqn != "" && strings.TrimSpace(r.SymbolFQN) == fqn {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartLine < out[j].StartLine })
	return out
}

// Section renders the module summary and rationale lines as one chunk section,
// bounded by budgetChars, or "" when the context is empty.
func (c SymbolChunkContext) Section(budgetChars int) string {
	var sb strings.Builder
	if moduleDoc := strings.TrimSpace(c.ModuleDoc); moduleDoc != "" {
		sb.WriteString("Module: " + moduleDoc + "\n")
	}
	if len(c.Rationale) > 0 {
		sb.WriteString("Rationale:\n")
		for _, r := range c.Rationale {
			content := strings.Join(strings.Fields(r.Content), " ")
			if content == "" {
				continue
			}
			sb.WriteString("- " + string(r.Kind) + ": " + content + "\n")
			if budgetChars > 0 && RuneLen(sb.String()) >= budgetChars {
				break
			}
		}
	}
	return strings.TrimSpace(sb.String())
}

// BuildSymbolChunk builds a semantic header chunk for a single code anchor.
func (b ChunkBuilder) BuildSymbolChunk(anchor codeanchor.IntelAnchor, relatedTitles []string, context SymbolChunkContext) SemanticChunk {
	if b.Budget.TargetChars == 0 {
		b.Budget = DefaultChunkBudget()
	}
	var sb strings.Builder
	if anchor.Signature != "" {
		sb.WriteString(fmt.Sprintf("Signature: %s\n", strings.TrimSpace(anchor.Signature)))
	}
	if anchor.DocComment != "" {
		sb.WriteString("\nDoc:\n")
		sb.WriteString(strings.TrimSpace(anchor.DocComment))
		sb.WriteString("\n")
	}
	if section := context.Section(b.Budget.TargetChars); section != "" {
		sb.WriteString(section + "\n")
	}
	if len(relatedTitles) > 0 {
		sb.WriteString("Related docs:\n")
		for _, title := range relatedTitles {
			sb.WriteString("- " + strings.TrimSpace(title) + "\n")
		}
	}
	return b.BuildPrimaryAnchorChunk(anchor, "symbol", sb.String())
}

// BuildBodyChunks slices a symbol body into overlapping chunks if spans are provided.
func (b ChunkBuilder) BuildBodyChunks(anchor codeanchor.IntelAnchor, body string) []SemanticChunk {
	if b.Budget.TargetChars == 0 {
		b.Budget = DefaultChunkBudget()
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}
	parts := splitWithOverlap(body, b.Budget.MaxChars, b.Budget.Overlap)
	chunks := make([]SemanticChunk, 0, len(parts))
	for i, part := range parts {
		// Index 0 is reserved for the non-body "symbol" chunk for this anchor; body chunks start at 1
		// so we can track content hashes with a simple index map and avoid re-embedding unchanged chunks.
		header := fmt.Sprintf("Kind: %s_body\nLang: %s\nFQN: %s\nPath: %s\n", anchor.Kind, anchor.Lang, anchor.FQN, anchor.Path)
		// Body slices are raw code; the symbol's own prose is what makes a
		// paraphrased question match them.
		if sig := strings.TrimSpace(anchor.Signature); sig != "" {
			header += "Signature: " + sig + "\n"
		}
		if doc := docSummaryLine(anchor.DocComment); doc != "" {
			header += "Doc: " + doc + "\n"
		}
		header += fmt.Sprintf("Chunk: %d/%d\n\n", i+1, len(parts))
		text := header + strings.TrimSpace(part)
		chunks = append(chunks, SemanticChunk{
			Input: codeindex.ChunkInput{
				Index:       i + 1,
				Granularity: "body",
				Breadcrumb:  anchor.Path,
				Heading:     anchor.Symbol,
				StartByte:   int(anchor.StartByte),
				EndByte:     int(anchor.EndByte),
				StartLine:   int(anchor.StartLine),
				EndLine:     int(anchor.EndLine),
				Hash:        hashText(text),
			},
			Text: text,
		})
	}
	return chunks
}

func splitWithOverlap(body string, maxChars, overlap int) []string {
	if maxChars <= 0 {
		return []string{body}
	}
	if overlap < 0 {
		overlap = 0
	}
	if overlap >= maxChars {
		overlap = maxChars / 5
	}
	r := []rune(body)
	if len(r) <= maxChars {
		return []string{body}
	}
	var parts []string
	start := 0
	for start < len(r) {
		end := start + maxChars
		if end > len(r) {
			end = len(r)
		}
		parts = append(parts, string(r[start:end]))
		if end == len(r) {
			break
		}
		start = end - overlap
		if start < 0 {
			start = 0
		}
	}
	return parts
}

// hashText delegates to the shared embeddings.HashText for consistent chunk hashing.
func hashText(text string) string {
	return embeddings.HashText(text)
}

// ExtractSpan returns the body slice for a symbol based on span metadata.
func ExtractSpan(content []byte, anchor codeanchor.IntelAnchor) string {
	if anchor.StartByte >= 0 && anchor.EndByte > anchor.StartByte && int(anchor.EndByte) <= len(content) {
		return string(content[anchor.StartByte:anchor.EndByte])
	}
	// fallback to line-based extraction if byte spans are missing
	if anchor.StartLine > 0 && anchor.EndLine >= anchor.StartLine {
		return extractLines(content, int(anchor.StartLine), int(anchor.EndLine))
	}
	return ""
}

func extractLines(content []byte, startLine, endLine int) string {
	if startLine <= 0 || endLine < startLine {
		return ""
	}
	lines := strings.SplitAfter(string(content), "\n")
	start := startLine - 1
	if start >= len(lines) {
		return ""
	}
	end := endLine
	if end > len(lines) {
		end = len(lines)
	}
	return strings.TrimSpace(strings.Join(lines[start:end], ""))
}

// TrimToBudget shortens a string to fit within a character budget while staying UTF-8 safe.
func TrimToBudget(s string, maxChars int) string {
	if maxChars <= 0 || len(s) <= maxChars {
		return s
	}
	r := []rune(s)
	if len(r) <= maxChars {
		return s
	}
	return string(r[:maxChars])
}

// StableStrings returns a sorted copy of the input slice.
func StableStrings(items []string) []string {
	out := append([]string(nil), items...)
	sort.Strings(out)
	return out
}

// RuneLen returns the rune length of a string; exposed for tests.
func RuneLen(s string) int { return utf8.RuneCountInString(s) }
