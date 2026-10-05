package semantic

import (
	"regexp"
	"strings"

	"github.com/atomicobject/rhizome/pkg/anchors"
)

var quotedLiteralPattern = regexp.MustCompile(`"([^"\\]*(?:\\.[^"\\]*)*)"|'([^'\\]*(?:\\.[^'\\]*)*)'|` + "`([^`]*)`")

type defaultSynthesisPolicy struct {
	opts    PolicyOptions
	builder ChunkBuilder
}

func DefaultSynthesisPolicy(opts PolicyOptions) SynthesisPolicy {
	if opts.Budget.TargetChars == 0 {
		opts.Budget = DefaultChunkBudget()
	}
	if opts.MaxPrimaryChars <= 0 {
		opts.MaxPrimaryChars = opts.Budget.MaxChars
	}
	if opts.ShortBodyLines <= 0 {
		opts.ShortBodyLines = 20
	}
	return &defaultSynthesisPolicy{
		opts:    opts,
		builder: ChunkBuilder{Budget: opts.Budget},
	}
}

func (p *defaultSynthesisPolicy) BuildModuleChunks(path string, lang codeanchor.Lang, anchors []codeanchor.IntelAnchor, fileContent []byte) []SemanticChunk {
	return []SemanticChunk{p.builder.buildModuleChunk(
		path,
		lang,
		anchors,
		relatedDocsForModule(anchors, nil),
		factualEnrichmentSection(path, "module", string(fileContent)),
	)}
}

func (*defaultSynthesisPolicy) ModuleChunkUsesCalls(string, codeanchor.Lang, []codeanchor.IntelAnchor, []byte) bool {
	return false
}

func (p *defaultSynthesisPolicy) ShouldIndexAnchor(anchor codeanchor.IntelAnchor) bool {
	return !strings.EqualFold(strings.TrimSpace(anchor.Kind), "field")
}

func (p *defaultSynthesisPolicy) BuildAnchorChunks(anchor codeanchor.IntelAnchor, fileContent []byte, relatedTitles []string, context SymbolChunkContext) []SemanticChunk {
	if len(relatedTitles) == 0 {
		relatedTitles = anchor.RelatedDocs
	}
	// Module summary and in-span rationale are the prose that lets a paraphrased
	// question reach code; every primary chunk carries them.
	prose := context.Section(p.opts.Budget.TargetChars)
	// v1: exactly one primary chunk per anchor (Index=0). Granularity varies based on representation.
	kind := strings.ToLower(strings.TrimSpace(anchor.Kind))

	span := ""
	if len(fileContent) > 0 {
		span = strings.TrimSpace(ExtractSpan(fileContent, anchor))
	}
	spanChars := RuneLen(span)
	enrichment := factualEnrichmentSection(anchor.Path, anchor.Kind, span)

	granularity := "signature_doc"
	excerpt := ""
	switch kind {
	case "class", "interface", "type", "enum", "struct":
		// Type-like declarations: prefer the full decl when it's small.
		if span != "" && spanChars <= p.opts.MaxPrimaryChars {
			granularity, excerpt = "decl_full", span
		} else {
			granularity, excerpt = "decl_trunc", truncateDeterministic(span, 800)
		}
	case "function", "method", "func":
		// Undocumented functions carry their short body or a bounded excerpt.
		if strings.TrimSpace(anchor.DocComment) == "" {
			spanLines := 1 + strings.Count(span, "\n")
			if span != "" && spanLines <= p.opts.ShortBodyLines && spanChars <= p.opts.MaxPrimaryChars {
				granularity, excerpt = "signature_body", span
			} else {
				excerpt = truncateDeterministic(span, 600)
			}
		}
	}

	text := joinSections(anchor.Signature, anchor.DocComment, excerpt, prose,
		relatedTitlesSection(relatedTitles), enrichment)
	return []SemanticChunk{p.builder.BuildPrimaryAnchorChunk(anchor, granularity, text)}
}

func (*defaultSynthesisPolicy) AnchorChunkUsesCalls(codeanchor.IntelAnchor, []byte, []string) bool {
	return false
}

func joinSections(parts ...string) string {
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return strings.Join(out, "\n\n")
}

func relatedTitlesSection(titles []string) string {
	if len(titles) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Related docs:\n")
	for _, t := range StableStrings(titles) {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		b.WriteString("- " + t + "\n")
	}
	return strings.TrimSpace(b.String())
}

func truncateDeterministic(s string, maxChars int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if maxChars <= 0 {
		return s
	}
	if RuneLen(s) <= maxChars {
		return s
	}
	return strings.TrimSpace(TrimToBudget(s, maxChars))
}

const (
	primaryEnrichmentMaxChars  = 320
	namedSignalsMaxChars       = 240
	namedSignalsMaxCount       = 6
	namedSignalsScanMaxMatches = namedSignalsMaxCount
)

func factualEnrichmentSection(path, kind, span string) string {
	var lines []string
	if surfaces := codeSurfaceLabels(path, kind); len(surfaces) > 0 {
		lines = append(lines, "Surface: "+strings.Join(surfaces, ", "))
	}
	if literals := meaningfulStringLiterals(span); len(literals) > 0 {
		line := "Named signals: " + strings.Join(literals, ", ")
		lines = append(lines, truncateDeterministic(line, namedSignalsMaxChars))
	}
	return truncateDeterministic(strings.Join(lines, "\n"), primaryEnrichmentMaxChars)
}

func codeSurfaceLabels(path, kind string) []string {
	normalized := "/" + strings.ToLower(strings.Trim(strings.ReplaceAll(path, "\\", "/"), "/")) + "/"
	kind = strings.ToLower(strings.TrimSpace(kind))
	base := strings.TrimSuffix(normalized[strings.LastIndex(strings.TrimSuffix(normalized, "/"), "/")+1:], "/")
	var labels []string
	add := func(label string, matched bool) {
		if matched {
			labels = append(labels, label)
		}
	}
	add("test", strings.Contains(normalized, "/test/") || strings.Contains(normalized, "/tests/") || strings.HasSuffix(base, "_test.go") || strings.Contains(base, ".test."))
	add("command", strings.Contains(normalized, "/cmd/"))
	add("agent-tool", strings.Contains(normalized, "/mcp/") || strings.Contains(normalized, "/agentapi/"))
	add("configuration", strings.Contains(normalized, "/config/") || strings.HasPrefix(base, "config"))
	add("persistence", strings.Contains(normalized, "/sqlite/") || strings.HasPrefix(strings.ToLower(base), "store"))
	add("indexer", strings.Contains(normalized, "/indexing/") || strings.Contains(normalized, "/indexer/") || strings.HasPrefix(strings.ToLower(base), "indexer"))
	add("validator", strings.Contains(normalized, "/validate/") || strings.HasPrefix(strings.ToLower(base), "validator"))
	add("boundary-handler", kind == "route" || kind == "handler")
	labels = stableUniqueInOrder(labels)
	if len(labels) > 3 {
		labels = labels[:3]
	}
	return labels
}

func stableUniqueInOrder(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.Join(strings.Fields(strings.TrimSpace(item)), " ")
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func meaningfulStringLiterals(span string) []string {
	matches := quotedLiteralPattern.FindAllStringSubmatch(span, namedSignalsScanMaxMatches)
	if len(matches) == 0 {
		return nil
	}
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		lit := ""
		for i := 1; i < len(match); i++ {
			if strings.TrimSpace(match[i]) != "" {
				lit = strings.TrimSpace(match[i])
				break
			}
		}
		lit = strings.Join(strings.Fields(lit), " ")
		if RuneLen(lit) < 4 || RuneLen(lit) > 90 {
			continue
		}
		if !literalLooksSemantic(lit) {
			continue
		}
		out = append(out, lit)
	}
	out = stableNonEmpty(out)
	if len(out) > namedSignalsMaxCount {
		out = out[:namedSignalsMaxCount]
	}
	return out
}

func literalLooksSemantic(lit string) bool {
	for _, r := range lit {
		if r == '_' || r == '-' || r == '/' || r == '.' || r == ':' {
			return true
		}
	}
	for _, r := range lit {
		if r >= 'A' && r <= 'Z' {
			return true
		}
	}
	return false
}
