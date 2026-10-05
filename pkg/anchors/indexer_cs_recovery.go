//go:build cgo

package codeanchor

import (
	"strconv"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

type transformedSourceMap struct {
	lineOffsets []int64
}

func newIdentitySourceMap() transformedSourceMap {
	return transformedSourceMap{}
}

func (m transformedSourceMap) OriginalLine(line int64) int64 {
	if len(m.lineOffsets) == 0 || line <= 0 {
		return line
	}
	idx := int(line - 1)
	if idx < 0 || idx >= len(m.lineOffsets) {
		return line
	}
	return line + m.lineOffsets[idx]
}

type byteSpan struct {
	start int
	end   int
}

const collectionExprErrorPadding = 64

func collectErrorSpans(root *sitter.Node) []byteSpan {
	if root == nil {
		return nil
	}
	seen := make(map[byteSpan]struct{})
	var spans []byteSpan
	var walk func(*sitter.Node)
	walk = func(node *sitter.Node) {
		if node == nil {
			return
		}
		childHasError := false
		for i := uint(0); i < node.ChildCount(); i++ {
			child := node.Child(i)
			if child != nil && child.HasError() {
				childHasError = true
			}
			walk(child)
		}
		if node.Kind() == "ERROR" || (node.HasError() && !childHasError) {
			span := byteSpan{start: int(node.StartByte()), end: int(node.EndByte())}
			if _, ok := seen[span]; !ok {
				seen[span] = struct{}{}
				spans = append(spans, span)
			}
		}
	}
	walk(root)
	return spans
}

func nearErrorSpan(start, end int, spans []byteSpan, padding int) bool {
	for _, span := range spans {
		spanStart := span.start - padding
		if spanStart < 0 {
			spanStart = 0
		}
		spanEnd := span.end + padding
		if start < spanEnd && end > spanStart {
			return true
		}
	}
	return false
}

func findMatchingBracket(src []byte, start int) int {
	if start < 0 || start >= len(src) || src[start] != '[' {
		return -1
	}
	depth := 0
	for i := start; i < len(src); i++ {
		switch src[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func prevNonSpaceIndex(src []byte, idx int) int {
	for idx >= 0 {
		switch src[idx] {
		case ' ', '\t', '\n', '\r':
			idx--
			continue
		}
		return idx
	}
	return -1
}

func isIdentByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

func hasPrevKeyword(src []byte, idx int, word string) bool {
	if idx < len(word)-1 {
		return false
	}
	start := idx - len(word) + 1
	if string(src[start:idx+1]) != word {
		return false
	}
	if start > 0 && isIdentByte(src[start-1]) {
		return false
	}
	return true
}

func hasPrevToken(src []byte, idx int, token string) bool {
	if idx < len(token)-1 {
		return false
	}
	start := idx - len(token) + 1
	return string(src[start:idx+1]) == token
}

func isCollectionExpressionContext(src []byte, start int) bool {
	prev := prevNonSpaceIndex(src, start-1)
	if prev < 0 {
		return false
	}
	if hasPrevKeyword(src, prev, "return") {
		return true
	}
	if hasPrevToken(src, prev, "=>") || hasPrevToken(src, prev, "??") {
		return true
	}
	if isIdentByte(src[prev]) {
		return false
	}
	switch src[prev] {
	case '=', '(', ',', ':', '?', '{':
		return true
	case ')':
		return true
	}
	return false
}

type rewritePatch struct {
	start       int
	end         int
	replacement string
}

func rewriteCollectionExpressions(content []byte, errorSpans []byteSpan) ([]byte, transformedSourceMap, bool) {
	if len(errorSpans) == 0 {
		return content, newIdentitySourceMap(), false
	}
	src := string(content)
	var patches []rewritePatch
	changed := false
	for i := 0; i < len(src); i++ {
		if src[i] != '[' {
			continue
		}
		end := findMatchingBracket([]byte(src), i)
		if end < 0 {
			continue
		}
		// Stay narrow: only rewrite bracket forms that are both near parser errors and
		// in positions where C# collection expressions appear, so indexers/attributes/types
		// keep their original text unless the parser is already failing around them.
		if !nearErrorSpan(i, end+1, errorSpans, collectionExprErrorPadding) {
			continue
		}
		if !isCollectionExpressionContext([]byte(src), i) {
			continue
		}
		span := src[i : end+1]
		patches = append(patches, rewritePatch{
			start:       i,
			end:         end + 1,
			replacement: "new object()" + strings.Repeat("\n", strings.Count(span, "\n")),
		})
		changed = true
		i = end
	}
	if !changed {
		return content, newIdentitySourceMap(), false
	}
	var out strings.Builder
	last := 0
	for _, patch := range patches {
		out.WriteString(src[last:patch.start])
		out.WriteString(patch.replacement)
		last = patch.end
	}
	out.WriteString(src[last:])
	return []byte(out.String()), newIdentitySourceMap(), true
}

func mergeCSharpSummaries(primary, recovered FileSummary, mapper transformedSourceMap) FileSummary {
	out := primary
	importSeen := make(map[string]struct{}, len(primary.Imports))
	for _, imp := range out.Imports {
		importSeen[strings.TrimSpace(imp.Module)] = struct{}{}
	}
	for _, imp := range recovered.Imports {
		key := strings.TrimSpace(imp.Module)
		if key == "" {
			continue
		}
		if _, ok := importSeen[key]; ok {
			continue
		}
		importSeen[key] = struct{}{}
		out.Imports = append(out.Imports, imp)
	}

	symSeen := make(map[string]struct{}, len(primary.Symbols))
	for _, sym := range out.Symbols {
		symSeen[csharpSymbolKey(sym)] = struct{}{}
	}
	for _, sym := range recovered.Symbols {
		sym.StartLine = mapper.OriginalLine(sym.StartLine)
		sym.EndLine = mapper.OriginalLine(sym.EndLine)
		key := csharpSymbolKey(sym)
		if _, ok := symSeen[key]; ok {
			continue
		}
		symSeen[key] = struct{}{}
		out.Symbols = append(out.Symbols, sym)
	}

	typeRefSeen := make(map[string]struct{}, len(primary.TypeRefs))
	for _, ref := range out.TypeRefs {
		typeRefSeen[csharpTypeRefKey(ref)] = struct{}{}
	}
	for _, ref := range recovered.TypeRefs {
		key := csharpTypeRefKey(ref)
		if _, ok := typeRefSeen[key]; ok {
			continue
		}
		typeRefSeen[key] = struct{}{}
		out.TypeRefs = append(out.TypeRefs, ref)
	}

	memberSeen := make(map[string]struct{}, len(primary.MemberRefs))
	for _, ref := range out.MemberRefs {
		memberSeen[strings.Join([]string{ref.File, ref.OwnerFQN, string(ref.Sym.Lang), ref.Sym.Pkg, ref.Sym.Name}, "\x00")] = struct{}{}
	}
	for _, ref := range recovered.MemberRefs {
		ref.File = primary.FilePath
		key := strings.Join([]string{ref.File, ref.OwnerFQN, string(ref.Sym.Lang), ref.Sym.Pkg, ref.Sym.Name}, "\x00")
		if _, ok := memberSeen[key]; ok {
			continue
		}
		memberSeen[key] = struct{}{}
		out.MemberRefs = append(out.MemberRefs, ref)
	}

	callSeen := make(map[string]struct{}, len(primary.Calls))
	for _, call := range out.Calls {
		callSeen[csharpCallKey(call)] = struct{}{}
	}
	for _, call := range recovered.Calls {
		key := csharpCallKey(call)
		if _, ok := callSeen[key]; ok {
			continue
		}
		callSeen[key] = struct{}{}
		out.Calls = append(out.Calls, call)
	}

	annSeen := make(map[string]struct{}, len(primary.Annotations))
	for _, ann := range out.Annotations {
		annSeen[csharpAnnotationKey(ann)] = struct{}{}
	}
	for _, ann := range recovered.Annotations {
		key := csharpAnnotationKey(ann)
		if _, ok := annSeen[key]; ok {
			continue
		}
		annSeen[key] = struct{}{}
		out.Annotations = append(out.Annotations, ann)
	}

	superSeen := make(map[string]struct{}, len(primary.Supers))
	for _, edge := range out.Supers {
		superSeen[csharpSuperKey(edge)] = struct{}{}
	}
	for _, edge := range recovered.Supers {
		key := csharpSuperKey(edge)
		if _, ok := superSeen[key]; ok {
			continue
		}
		superSeen[key] = struct{}{}
		out.Supers = append(out.Supers, edge)
	}

	return out
}

func csharpSymbolKey(sym Symbol) string {
	return strings.Join([]string{
		string(sym.Kind),
		sym.Pkg,
		sym.Name,
		sym.File,
		int64String(sym.StartLine),
		int64String(sym.EndLine),
	}, "|")
}

func csharpTypeRefKey(ref TypeRef) string {
	return strings.Join([]string{
		ref.File,
		ref.OwnerFQN,
		string(ref.TypeSym.Lang),
		ref.TypeSym.Pkg,
		ref.TypeSym.Name,
	}, "|")
}

func csharpCallKey(call CallSite) string {
	return strings.Join([]string{
		call.File,
		call.OwnerFQN,
		string(call.CalleeSymbol.Lang),
		call.CalleeSymbol.Pkg,
		call.CalleeSymbol.Name,
	}, "|")
}

func csharpAnnotationKey(ann AnnotationUse) string {
	return strings.Join([]string{ann.OwnerFQN, ann.AnnSymbol.Pkg, ann.AnnSymbol.Name}, "|")
}

func csharpSuperKey(edge SuperEdge) string {
	return edge.ChildFQN + "|" + edge.ParentFQN
}

func int64String(v int64) string {
	return strconv.FormatInt(v, 10)
}
