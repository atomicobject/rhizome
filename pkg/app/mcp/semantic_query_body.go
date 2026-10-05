package mcp

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/app/contextpack"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
)

func materializeSemanticBodies(ctx context.Context, cfg Config, intel *semdb.Store, groups []semanticQueryGroup, selected map[int]semanticSelection, optionsByIndex map[int][]semanticContentOption, anchors map[string]codeanchor.IntelAnchor, offset int) map[int]semanticBody {
	type task struct {
		idx      int
		plan     contentPlan
		maxChars int
	}
	type result struct {
		idx  int
		body semanticBody
	}

	tasks := make(chan task)
	results := make(chan result)

	workerCount := semanticBodyWorkerCount(len(selected))
	var wg sync.WaitGroup
	wg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go func() {
			defer wg.Done()
			for t := range tasks {
				group := groups[t.idx]
				body, fingerprint, truncated := renderSemanticBody(ctx, cfg, intel, group, anchors, t.plan, t.maxChars)
				results <- result{idx: t.idx, body: semanticBody{plan: t.plan, body: body, fingerprint: fingerprint, truncated: truncated}}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	go func() {
		for idx, sel := range selected {
			planCost := planCostForOption(optionsByIndex[idx], sel.plan)
			if planCost <= 0 {
				planCost = 900
			}
			header := semanticHeader(offset+idx, groups[idx].match, groups[idx].score)
			maxChars := max(400, planCost-len(header))
			tasks <- task{idx: idx, plan: sel.plan, maxChars: maxChars}
		}
		close(tasks)
	}()

	out := make(map[int]semanticBody, len(selected))
	for res := range results {
		out[res.idx] = res.body
	}
	return out
}

func semanticBodyWorkerCount(taskCount int) int {
	return min(8, max(1, taskCount))
}

func renderSemanticPiece(rank int, match SemanticMatchPayload, score float64, body string) string {
	header := semanticHeader(rank, match, score)
	body = strings.TrimSpace(body)
	if body == "" {
		return strings.TrimSpace(header)
	}
	return strings.TrimSpace(header + "\n" + indent(body, "    "))
}

func assignContentField(match *SemanticMatchPayload, plan contentPlan, body string, truncated bool) {
	if match == nil {
		return
	}
	body = strings.TrimSpace(body)
	if isLowSignalSpan(body) {
		return
	}
	if body == "" {
		return
	}
	match.ContentKind = string(plan)
	match.ContentTruncated = truncated
	switch plan {
	case planFull:
		match.FullFileContent = body
	case planExcerpt:
		match.ExcerptContent = body
	case planOutline:
		match.OutlineContent = body
	case planSignature:
		match.SignatureContent = body
	case planStub:
		match.StubContent = body
	}
}

func semanticHeader(rank int, match SemanticMatchPayload, score float64) string {
	scoreStr := fmt.Sprintf("%.1f%%", score*100)
	title := firstNonEmpty(match.Title, match.Symbol, match.FQN, filepath.Base(match.Path))
	path := strings.TrimSpace(match.Path)
	if match.Type == "code" && match.StartLine > 0 {
		path = fmt.Sprintf("%s:%d", path, match.StartLine)
	}
	header := fmt.Sprintf("%2d. %s [%s] %s\n    %s", rank+1, scoreStr, match.Type, title, path)
	where := strings.TrimSpace(firstNonEmpty(match.Breadcrumb, match.Heading))
	if where != "" {
		header = header + "\n    " + where
	}
	if hint := semanticReferenceHint(match); hint != "" {
		header = header + "\n    link: " + hint
	}
	return header
}

func semanticReferenceHint(match SemanticMatchPayload) string {
	if strings.TrimSpace(match.ReferenceHint) != "" {
		return strings.TrimSpace(match.ReferenceHint)
	}
	if match.LinkTarget != nil && strings.TrimSpace(match.LinkTarget.Wikilink) != "" {
		return strings.TrimSpace(match.LinkTarget.Wikilink)
	}
	return ""
}

func renderSemanticBody(ctx context.Context, cfg Config, intel *semdb.Store, group semanticQueryGroup, anchors map[string]codeanchor.IntelAnchor, plan contentPlan, maxChars int) (string, string, bool) {
	body := ""
	truncated := false
	switch plan {
	case planFull:
		if group.match.Type == "note" {
			body, truncated = readAndTrimMarkdownWithTruncation(ctx, cfg.VaultPath, group.match.Path, maxChars)
		} else if group.match.Type == "code" {
			body, truncated = readAndTrimCodeFullWithTruncation(ctx, cfg.VaultPath, group.match.Path, maxChars)
		}
	case planExcerpt:
		if group.match.Type == "note" {
			body, truncated = readNoteExcerptWithTruncation(ctx, cfg.VaultPath, group, maxChars)
		} else if group.match.Type == "code" {
			body, truncated = readCodeExcerptWithTruncation(ctx, cfg.VaultPath, group, anchors, maxChars)
		}
	case planOutline:
		if group.match.Type == "code" {
			body, truncated = renderCodeOutlineWithTruncation(group, anchors, maxChars)
		}
	case planSignature:
		if group.match.Type == "code" {
			body, truncated = renderAnchorSignatureWithTruncation(group, anchors, maxChars)
		}
	case planStub:
		body = ""
	}
	body = strings.TrimSpace(body)
	if !utf8.ValidString(body) {
		// JSON replaces each invalid source byte with U+FFFD. Bind fingerprints
		// and renderer ranges to that same delivered representation.
		body = strings.Map(func(r rune) rune { return r }, body)
	}
	fingerprint := ""
	if body != "" {
		fingerprint = fingerprintText(body)
	}
	return body, fingerprint, truncated
}

func readAndTrimCodeFullWithTruncation(ctx context.Context, vaultPath, relPath string, maxChars int) (string, bool) {
	full, ok := safeJoinVaultPath(vaultPath, relPath)
	if !ok {
		return "", false
	}
	b, err := readSemanticQueryFile(ctx, semanticReadCode, full)
	if err != nil {
		return "", false
	}
	return trimSemanticBody(strings.TrimSpace(string(b)), maxChars)
}

func readNoteExcerptWithTruncation(ctx context.Context, vaultPath string, group semanticQueryGroup, maxChars int) (string, bool) {
	for _, h := range group.hits {
		if h.ChunkIndex >= 0 {
			txt := embeddings.CoreChunkBody(semanticQueryChunkText(ctx, semanticReadNote, vaultPath, h.Path, h.ChunkIndex, nil))
			if strings.TrimSpace(txt) != "" {
				return trimSemanticBody(strings.TrimSpace(txt), maxChars)
			}
		}
	}
	return readAndTrimMarkdownWithTruncation(ctx, vaultPath, group.match.Path, maxChars)
}

func readCodeExcerptWithTruncation(ctx context.Context, vaultPath string, group semanticQueryGroup, anchors map[string]codeanchor.IntelAnchor, maxChars int) (string, bool) {
	full, ok := safeJoinVaultPath(vaultPath, group.match.Path)
	if !ok {
		return "", false
	}
	b, err := readSemanticQueryFile(ctx, semanticReadCode, full)
	if err != nil {
		return "", false
	}
	text := strings.TrimSpace(string(b))
	if len(anchors) == 0 {
		return trimSemanticBody(text, maxChars)
	}

	spans := make([]string, 0, 2)
	seen := map[string]struct{}{}
	for _, h := range group.hits {
		if h.AnchorID == "" {
			continue
		}
		a, ok := anchors[h.AnchorID]
		if !ok {
			continue
		}
		if _, ok := seen[a.AnchorID]; ok {
			continue
		}
		seen[a.AnchorID] = struct{}{}
		if span := strings.TrimSpace(extractSpan(b, a.StartByte, a.EndByte, a.StartLine, a.EndLine)); span != "" {
			if isLowSignalSpan(span) {
				continue
			}
			spans = append(spans, span)
		} else if sig := strings.TrimSpace(a.Signature); sig != "" {
			if isLowSignalSignature(sig) && strings.TrimSpace(a.DocComment) == "" {
				continue
			}
			s := sig
			if doc := strings.TrimSpace(a.DocComment); doc != "" {
				s = s + "\n" + doc
			}
			spans = append(spans, s)
		}
		if len(spans) >= 2 {
			break
		}
	}
	if len(spans) == 0 {
		return trimSemanticBody(text, maxChars)
	}
	joined := strings.Join(spans, "\n\n")
	return trimSemanticBody(strings.TrimSpace(joined), maxChars)
}

func renderAnchorSignatureWithTruncation(group semanticQueryGroup, anchors map[string]codeanchor.IntelAnchor, maxChars int) (string, bool) {
	for _, h := range group.hits {
		if h.AnchorID == "" {
			continue
		}
		a, ok := anchors[h.AnchorID]
		if !ok {
			continue
		}
		var parts []string
		if sig := strings.TrimSpace(a.Signature); sig != "" {
			if !isLowSignalSignature(sig) {
				parts = append(parts, sig)
			}
		}
		if doc := strings.TrimSpace(a.DocComment); doc != "" {
			parts = append(parts, doc)
		}
		if len(parts) == 0 {
			continue
		}
		return trimSemanticBody(strings.Join(parts, "\n"), maxChars)
	}
	return "", false
}

const maxCodeOutlineEntries = 6

type codeOutlineEntry struct {
	line      int64
	signature string
	doc       string
}

func renderCodeOutlineWithTruncation(group semanticQueryGroup, anchors map[string]codeanchor.IntelAnchor, maxChars int) (string, bool) {
	entries := codeOutlineEntries(group, anchors, maxCodeOutlineEntries)
	if len(entries) == 0 {
		return "", false
	}
	lines := make([]string, 0, len(entries)*2)
	for _, entry := range entries {
		sig := entry.signature
		if sig == "" {
			sig = entry.doc
		}
		if sig == "" {
			continue
		}
		if entry.line > 0 {
			lines = append(lines, fmt.Sprintf("L%d %s", entry.line, sig))
		} else {
			lines = append(lines, sig)
		}
		if entry.doc != "" && entry.doc != sig {
			lines = append(lines, "  "+entry.doc)
		}
	}
	return trimSemanticBody(strings.TrimSpace(strings.Join(lines, "\n")), maxChars)
}

func codeOutlineEntries(group semanticQueryGroup, anchors map[string]codeanchor.IntelAnchor, maxItems int) []codeOutlineEntry {
	if maxItems <= 0 || group.match.Type != "code" {
		return nil
	}
	entries := make([]codeOutlineEntry, 0, maxItems)
	seen := map[string]struct{}{}
	for _, h := range group.hits {
		id := strings.TrimSpace(h.AnchorID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		a, ok := anchors[id]
		if !ok {
			continue
		}
		entry := codeOutlineEntry{
			line:      a.StartLine,
			signature: codeOutlineSignature(a),
			doc:       codeOutlineDocLine(a.DocComment),
		}
		if entry.signature == "" && entry.doc == "" {
			continue
		}
		entries = append(entries, entry)
		if len(entries) >= maxItems {
			break
		}
	}
	return entries
}

func codeOutlineSignature(anchor codeanchor.IntelAnchor) string {
	sig := strings.TrimSpace(anchor.Signature)
	if isLowSignalSignature(sig) {
		sig = ""
	}
	if sig != "" {
		return trimPreview(sig, 180)
	}
	if strings.EqualFold(strings.TrimSpace(anchor.Kind), "module") {
		return ""
	}
	return trimPreview(strings.TrimSpace(firstNonEmpty(anchor.Symbol, anchor.FQN)), 180)
}

func codeOutlineDocLine(doc string) string {
	for _, line := range strings.Split(strings.TrimSpace(doc), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "///"))
		line = strings.TrimSpace(strings.TrimPrefix(line, "//"))
		line = strings.TrimSpace(strings.TrimPrefix(line, "#"))
		line = strings.TrimSpace(strings.TrimPrefix(line, "--"))
		line = strings.TrimSpace(strings.TrimPrefix(line, "/*"))
		line = strings.TrimSpace(strings.TrimPrefix(line, "*"))
		line = strings.TrimSpace(strings.TrimSuffix(line, "*/"))
		if line != "" {
			return trimPreview(line, 180)
		}
	}
	return ""
}

func readAndTrimMarkdownWithTruncation(ctx context.Context, vaultPath, relPath string, maxChars int) (string, bool) {
	full, ok := safeJoinVaultPath(vaultPath, relPath)
	if !ok {
		return "", false
	}
	b, err := readSemanticQueryFile(ctx, semanticReadNote, full)
	if err != nil {
		return "", false
	}
	out, truncated := contextpack.TrimMarkdown(string(b), maxChars)
	return out, truncated
}

func trimSemanticBody(text string, maxChars int) (string, bool) {
	text = strings.TrimSpace(text)
	trimmed := contextpack.TrimToBudget(text, maxChars)
	return trimmed, (maxChars <= 0 && text != "") || len(text) > maxChars
}

func extractSpan(content []byte, startByte, endByte, startLine, endLine int64) string {
	if startByte >= 0 && endByte > startByte && int(endByte) <= len(content) {
		return string(content[startByte:endByte])
	}
	if startLine > 0 && endLine >= startLine {
		return extractLines(content, int(startLine), int(endLine))
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

func semanticMatchKey(m SemanticMatchPayload) string {
	if m.NodeRef != nil && m.NodeRef.Kind != "" && m.NodeRef.Kind != "NOTE" {
		return strings.Join([]string{"node", m.NodeRef.NotePath, m.NodeRef.NodeID, m.NodeRef.Fragment, m.NodeRef.Structural}, "\x00")
	}
	switch strings.TrimSpace(m.Type) {
	case "note":
		if m.Path == "" {
			return ""
		}
		return "note:" + string(paths.NormalizeNotePath(m.Path))
	case "code":
		if m.Path == "" {
			return ""
		}
		return "code:" + string(paths.NormalizeCode(filepath.ToSlash(m.Path)))
	default:
		if m.Path == "" {
			return ""
		}
		return strings.TrimSpace(m.Type) + ":" + strings.TrimSpace(m.Path)
	}
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
