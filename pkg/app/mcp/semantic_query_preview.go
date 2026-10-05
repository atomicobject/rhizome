package mcp

import (
	"context"
	"fmt"
	"os"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/vault/frontmatter"
)

func previewFromBody(body string, match SemanticMatchPayload, config Config, noteCache, codeCache map[string]string, codeAnchorCache map[string][]codeanchor.IntelAnchor, intel *semdb.Store, group semanticQueryGroup, anchors map[string]codeanchor.IntelAnchor) string {
	body = strings.TrimSpace(body)
	if isLowSignalSpan(body) {
		body = ""
	}
	preview := ""
	if body != "" {
		switch match.Type {
		case "note":
			preview = notePreviewFromContent(body, match.Path, config)
		case "code":
			preview = codePreviewFromContent(body)
		default:
			preview = trimPreview(body, 240)
		}
	}
	if preview == "" {
		preview = trimPreview(firstNonEmpty(match.Heading, match.Title), 240)
	}
	if needsPreviewFromHit(preview, match) {
		if hitPreview := previewFromHit(group, config.VaultPath, anchors); hitPreview != "" {
			return hitPreview
		}
	}
	if match.Type == "note" && needsNotePreviewFallback(preview, match) {
		if notePreview := notePreviewText(config, match.Path, noteCache); notePreview != "" {
			preview = notePreview
		}
		return preview
	}
	if match.Type == "code" && needsCodePreviewFallback(preview, match) {
		if anchorPreview := previewFromPathAnchors(group, codeAnchorCache, intel); anchorPreview != "" {
			return anchorPreview
		}
		if fallback := codePreviewFromFile(config.VaultPath, match.Path, codeCache); fallback != "" {
			return fallback
		}
	}
	if match.Type == "code" && isBoilerplateLine(strings.ToLower(preview)) {
		if anchorPreview := previewFromPathAnchors(group, codeAnchorCache, intel); anchorPreview != "" {
			return anchorPreview
		}
		if fallback := codePreviewFromFile(config.VaultPath, match.Path, codeCache); fallback != "" {
			return fallback
		}
	}
	return preview
}

func trimPreview(text string, maxChars int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if maxChars <= 0 {
		maxChars = 200
	}
	if len(text) <= maxChars {
		return text
	}
	cut := text[:maxChars]
	for _, sep := range []string{". ", "\n", "; "} {
		if idx := strings.LastIndex(cut, sep); idx > 40 {
			cut = strings.TrimSpace(cut[:idx+1])
			return cut
		}
	}
	return strings.TrimSpace(cut)
}

func needsNotePreviewFallback(preview string, match SemanticMatchPayload) bool {
	preview = strings.TrimSpace(preview)
	if preview == "" {
		return true
	}
	heading := strings.TrimSpace(match.Heading)
	title := strings.TrimSpace(match.Title)
	if heading != "" && strings.EqualFold(preview, heading) {
		return true
	}
	if title != "" && strings.EqualFold(preview, title) {
		return true
	}
	return len(preview) < 40
}

func needsPreviewFromHit(preview string, match SemanticMatchPayload) bool {
	preview = strings.TrimSpace(preview)
	if preview == "" {
		return true
	}
	heading := strings.TrimSpace(match.Heading)
	title := strings.TrimSpace(match.Title)
	if heading != "" && strings.EqualFold(preview, heading) {
		return true
	}
	if title != "" && strings.EqualFold(preview, title) {
		return true
	}
	return len(preview) < 60
}

func needsCodePreviewFallback(preview string, match SemanticMatchPayload) bool {
	preview = strings.TrimSpace(preview)
	if preview == "" {
		return true
	}
	if strings.HasPrefix(strings.ToLower(preview), "package ") {
		return true
	}
	return len(preview) < 60
}

func previewFromHit(group semanticQueryGroup, vaultPath string, anchors map[string]codeanchor.IntelAnchor) string {
	if group.match.Type == "note" {
		for _, h := range group.hits {
			if h.ChunkIndex < 0 {
				continue
			}
			txt := embeddings.CoreChunkBody(embeddings.ChunkTextForPath(vaultPath, h.Path, h.ChunkIndex, nil))
			if strings.TrimSpace(txt) != "" {
				return trimPreview(strings.TrimSpace(txt), 240)
			}
		}
		return ""
	}
	if group.match.Type != "code" {
		return ""
	}
	if preview := previewFromAnchors(group, anchors, 3); preview != "" {
		return preview
	}
	for _, h := range group.hits {
		if h.AnchorID == "" {
			continue
		}
		a, ok := anchors[h.AnchorID]
		if !ok {
			continue
		}
		full, ok := safeJoinVaultPath(vaultPath, group.match.Path)
		if ok {
			if b, err := os.ReadFile(full); err == nil {
				if span := strings.TrimSpace(extractSpan(b, a.StartByte, a.EndByte, a.StartLine, a.EndLine)); span != "" {
					if isLowSignalSpan(span) {
						continue
					}
					return trimPreview(span, 240)
				}
			}
		}
	}
	return ""
}

func previewFromPathAnchors(group semanticQueryGroup, cache map[string][]codeanchor.IntelAnchor, intel *semdb.Store) string {
	if group.match.Type != "code" || intel == nil {
		return ""
	}
	path := strings.TrimSpace(group.match.Path)
	if path == "" {
		return ""
	}
	anchors := cache[path]
	if anchors == nil {
		list, err := intel.IntelAnchorsByPath(context.Background(), path)
		if err != nil || len(list) == 0 {
			cache[path] = nil
			return ""
		}
		cache[path] = list
		anchors = list
	}
	if len(anchors) == 0 {
		return ""
	}
	lines := make([]string, 0, 3)
	for _, a := range anchors {
		if line := anchorPreviewLine(a); line != "" {
			lines = append(lines, line)
		}
		if len(lines) >= 3 {
			break
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return trimPreview(strings.Join(lines, "\n"), 240)
}

func codePreviewFromFile(vaultPath, relPath string, cache map[string]string) string {
	if vaultPath == "" || relPath == "" {
		return ""
	}
	if cached, ok := cache[relPath]; ok {
		return cached
	}
	full, ok := safeJoinVaultPath(vaultPath, relPath)
	if !ok {
		return ""
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return ""
	}
	preview := codePreviewFromContent(string(b))
	cache[relPath] = preview
	return preview
}

func codePreviewFromContent(content string) string {
	if strings.TrimSpace(content) == "" {
		return ""
	}
	lines := strings.Split(content, "\n")
	var out []string
	var commentBuf []string
	inBlockComment := false
	inImport := false
	maxScan := min(len(lines), 200)

	flushComment := func() string {
		if len(commentBuf) == 0 {
			return ""
		}
		text := strings.TrimSpace(strings.Join(commentBuf, " "))
		commentBuf = commentBuf[:0]
		return text
	}

	for i := 0; i < maxScan; i++ {
		line := lines[i]
		trim := strings.TrimSpace(line)
		if trim == "" {
			if !inBlockComment {
				commentBuf = commentBuf[:0]
			}
			continue
		}
		if strings.HasPrefix(trim, "#!") {
			continue
		}
		if inBlockComment {
			if end := strings.Index(trim, "*/"); end >= 0 {
				commentBuf = append(commentBuf, strings.TrimSpace(trim[:end]))
				inBlockComment = false
			} else {
				commentBuf = append(commentBuf, strings.TrimSpace(trim))
			}
			continue
		}
		if strings.HasPrefix(trim, "/*") {
			if end := strings.Index(trim, "*/"); end >= 0 {
				commentBuf = append(commentBuf, strings.TrimSpace(strings.TrimPrefix(trim[:end], "/*")))
			} else {
				inBlockComment = true
				commentBuf = append(commentBuf, strings.TrimSpace(strings.TrimPrefix(trim, "/*")))
			}
			continue
		}
		if strings.HasPrefix(trim, "//") || strings.HasPrefix(trim, "#") || strings.HasPrefix(trim, "--") {
			commentBuf = append(commentBuf, strings.TrimSpace(strings.TrimPrefix(trim, "//")))
			continue
		}
		if isBoilerplateLine(trim) {
			continue
		}
		if inImport {
			if trim == ")" {
				inImport = false
			}
			continue
		}
		if strings.HasPrefix(trim, "import ") || strings.HasPrefix(trim, "from ") {
			inImport = strings.HasSuffix(trim, "(") || strings.HasSuffix(trim, "import(")
			continue
		}
		if decl := codeSignatureLine(trim); decl != "" {
			doc := flushComment()
			if doc != "" {
				out = append(out, doc+" — "+decl)
			} else {
				out = append(out, decl)
			}
			if len(out) >= 3 {
				break
			}
			continue
		}
		commentBuf = commentBuf[:0]
		if len(out) == 0 {
			out = append(out, trim)
		}
		if len(out) >= 3 {
			break
		}
	}
	if len(out) == 0 {
		return ""
	}
	return trimPreview(strings.Join(out, "\n"), 240)
}

func isBoilerplateLine(trim string) bool {
	lower := strings.ToLower(trim)
	switch {
	case strings.HasPrefix(lower, "package "):
		return true
	case strings.HasPrefix(lower, "import "):
		return true
	case strings.HasPrefix(lower, "from "):
		return true
	case strings.HasPrefix(lower, "using "):
		return true
	case strings.HasPrefix(lower, "namespace "):
		return true
	case strings.HasPrefix(lower, "module "):
		return true
	}
	return false
}

func isLowSignalSpan(span string) bool {
	span = strings.TrimSpace(span)
	if span == "" {
		return true
	}
	lines := strings.Split(span, "\n")
	nonEmpty := 0
	var first string
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		if strings.HasPrefix(trim, "//") || strings.HasPrefix(trim, "#") || strings.HasPrefix(trim, "--") {
			continue
		}
		if first == "" {
			first = trim
		}
		nonEmpty++
		if nonEmpty > 1 {
			break
		}
	}
	if nonEmpty == 0 {
		return true
	}
	if nonEmpty == 1 && isBoilerplateLine(strings.ToLower(first)) {
		return true
	}
	return false
}

func codeSignatureLine(trim string) string {
	lower := strings.ToLower(trim)
	switch {
	case strings.HasPrefix(lower, "func "):
		return trim
	case strings.HasPrefix(lower, "def "):
		return trim
	case strings.HasPrefix(lower, "class "):
		return trim
	case strings.HasPrefix(lower, "interface "):
		return trim
	case strings.HasPrefix(lower, "struct "):
		return trim
	case strings.HasPrefix(lower, "enum "):
		return trim
	case strings.HasPrefix(lower, "type "):
		return trim
	case strings.HasPrefix(lower, "public ") || strings.HasPrefix(lower, "private ") || strings.HasPrefix(lower, "protected "):
		return trim
	}
	if strings.Contains(trim, "(") && (strings.Contains(trim, "{") || strings.HasSuffix(trim, ")") || strings.Contains(trim, "=>")) {
		return trim
	}
	if strings.Contains(trim, ":") && (strings.HasPrefix(lower, "def ") || strings.HasPrefix(lower, "class ")) {
		return trim
	}
	return ""
}

func previewFromAnchors(group semanticQueryGroup, anchors map[string]codeanchor.IntelAnchor, maxItems int) string {
	if maxItems <= 0 {
		return ""
	}
	lines := make([]string, 0, maxItems)
	seen := map[string]struct{}{}
	for _, h := range group.hits {
		if h.AnchorID == "" {
			continue
		}
		if _, ok := seen[h.AnchorID]; ok {
			continue
		}
		seen[h.AnchorID] = struct{}{}
		a, ok := anchors[h.AnchorID]
		if !ok {
			continue
		}
		line := anchorPreviewLine(a)
		if line == "" {
			continue
		}
		lines = append(lines, line)
		if len(lines) >= maxItems {
			break
		}
	}
	if len(lines) == 0 {
		return ""
	}
	remaining := max(0, group.spread-len(lines))
	if remaining > 0 {
		lines = append(lines, fmt.Sprintf("…and %d more matches", remaining))
	}
	return trimPreview(strings.Join(lines, "\n"), 240)
}

func anchorPreviewLine(anchor codeanchor.IntelAnchor) string {
	sig := strings.TrimSpace(anchor.Signature)
	doc := strings.TrimSpace(anchor.DocComment)
	if isLowSignalSignature(sig) {
		sig = ""
	}
	if sig == "" && doc == "" {
		return ""
	}
	if sig == "" {
		return trimPreview(doc, 120)
	}
	if doc == "" {
		return trimPreview(sig, 120)
	}
	doc = trimPreview(doc, 120)
	return trimPreview(sig+" — "+doc, 160)
}

func isLowSignalSignature(sig string) bool {
	sig = strings.TrimSpace(sig)
	if sig == "" {
		return true
	}
	lower := strings.ToLower(sig)
	if isBoilerplateLine(lower) {
		return true
	}
	if strings.HasPrefix(lower, "package ") || strings.HasPrefix(lower, "module ") || strings.HasPrefix(lower, "namespace ") {
		return true
	}
	return false
}

func notePreviewText(config Config, relPath string, cache map[string]string) string {
	if relPath == "" || config.VaultPath == "" {
		return ""
	}
	if cache != nil {
		if cached, ok := cache[relPath]; ok {
			return cached
		}
	}
	full, ok := safeJoinVaultPath(config.VaultPath, relPath)
	if !ok {
		return ""
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return ""
	}
	content := string(b)
	preview := notePreviewFromContent(content, relPath, config)
	if cache != nil {
		cache[relPath] = preview
	}
	return preview
}

// notePreviewFromContent consumes only facts from the configured note-format
// runtime. A descriptor-only source is intentionally omitted before any
// preview parsing is attempted.
func notePreviewFromContent(content, path string, config Config) string {
	if strings.TrimSpace(content) == "" {
		return ""
	}
	projection, ok := previewProjectionForContent(content, path, config)
	if !ok {
		return ""
	}
	if summary := summarizeBlessedFrontmatter(frontmatter.FilterBlessed(projectionRootMetadata(projection))); summary != "" {
		return trimPreview(summary, 240)
	}
	content = withoutProjectionRootMetadataEnvelope(content, projection)
	paragraph := firstParagraph(content)
	if paragraph == "" {
		return ""
	}
	return trimPreview(paragraph, 240)
}

func previewProjectionForContent(content, path string, config Config) (noteformat.Projection, bool) {
	runtime, err := config.NoteMetadata.FormatRuntime()
	if err != nil {
		return noteformat.Projection{}, false
	}
	notePath, err := paths.CleanNotePath(path)
	if err != nil {
		return noteformat.Projection{}, false
	}
	provider, ok := runtime.ProviderForPath(paths.RelPath(notePath))
	if !ok {
		return noteformat.Projection{}, false
	}
	descriptor := provider.Descriptor()
	if !runtime.CanProject(descriptor.ID) || !descriptor.Capabilities.Has(noteformat.CapabilitySourceReading) {
		return noteformat.Projection{}, false
	}
	source, err := noteformat.NewAuthoredSource(notePath, descriptor, []byte(content), 0)
	if err != nil {
		return noteformat.Projection{}, false
	}
	projection, err := runtime.Project(source)
	if err != nil || projection.Status != noteformat.ProjectionStatusCurrent {
		return noteformat.Projection{}, false
	}
	return projection, true
}

func projectionRootMetadata(projection noteformat.Projection) map[string]any {
	if len(projection.Facts.RootMetadata) == 0 {
		return nil
	}
	metadata := make(map[string]any, len(projection.Facts.RootMetadata))
	for _, fact := range projection.Facts.RootMetadata {
		metadata[strings.ToLower(fact.Key)] = fact.Value.Export()
	}
	return metadata
}

// withoutProjectionRootMetadataEnvelope keeps the existing Markdown preview
// output without selecting Markdown syntax. The provider's root-metadata fact
// authorizes removal only when the sealed authored source has an envelope.
func withoutProjectionRootMetadataEnvelope(content string, projection noteformat.Projection) string {
	if len(projection.Facts.RootMetadata) == 0 {
		return content
	}
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return content
	}
	for index := 1; index < len(lines); index++ {
		marker := strings.TrimSpace(lines[index])
		if marker == "---" || marker == "..." {
			return strings.Join(lines[index+1:], "\n")
		}
	}
	return content
}

func summarizeBlessedFrontmatter(fm map[string]interface{}) string {
	if len(fm) == 0 {
		return ""
	}
	for _, k := range frontmatter.BlessedKeys {
		val, ok := fm[strings.ToLower(k)]
		if !ok {
			continue
		}
		if strings.EqualFold(k, "tags") {
			continue
		}
		if s := formatFrontmatterPreviewValue(val); s != "" {
			return s
		}
	}
	return ""
}

func formatFrontmatterPreviewValue(val interface{}) string {
	switch v := val.(type) {
	case string:
		return strings.TrimSpace(v)
	case []string:
		return strings.TrimSpace(strings.Join(v, ", "))
	case []interface{}:
		var parts []string
		for _, item := range v {
			if s, ok := item.(string); ok {
				if trimmed := strings.TrimSpace(s); trimmed != "" {
					parts = append(parts, trimmed)
				}
			}
		}
		return strings.TrimSpace(strings.Join(parts, ", "))
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func firstParagraph(content string) string {
	lines := strings.Split(content, "\n")
	var buf []string
	started := false
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == "" {
			if started {
				break
			}
			continue
		}
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			if started {
				break
			}
			continue
		}
		if strings.HasPrefix(t, "#") && !started {
			continue
		}
		started = true
		buf = append(buf, t)
		if len(strings.Join(buf, " ")) > 360 {
			break
		}
	}
	return strings.TrimSpace(strings.Join(buf, " "))
}
