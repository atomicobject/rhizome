package notediscovery

import (
	"path/filepath"
	"strings"
)

// explicitTerminalExtensions returns the literal extension set selected by a
// pattern's filename component. It intentionally rejects character classes and
// extension suffix wildcards: an explicitly included provider must be
// authorized by a pattern that can be shown to select only its claimed
// extensions, without inferring intent from a broad glob. Callers reject brace
// alternatives not wholly claimed by the selected provider.
//
// The normalized pattern folds only the terminal literal extension. Callers
// preserve existing case-sensitive semantics for directories and literal
// basenames while treating an owned format's extension case-insensitively.
func explicitTerminalExtensions(pattern string) (map[string]struct{}, string, bool) {
	pattern = filepath.ToSlash(strings.TrimSpace(pattern))
	if pattern == "" {
		return nil, "", false
	}

	segmentStart := strings.LastIndex(pattern, "/") + 1
	segment := pattern[segmentStart:]

	if extensions, normalizedSegment, ok := braceExtensions(segment); ok {
		return extensions, pattern[:segmentStart] + normalizedSegment, true
	}

	dot := strings.LastIndex(segment, ".")
	if dot < 0 || dot == len(segment)-1 {
		return nil, "", false
	}
	extension := segment[dot:]
	if strings.ContainsAny(extension, "*?[]{}\\") {
		return nil, "", false
	}
	extension = strings.ToLower(extension)
	return map[string]struct{}{extension: {}}, pattern[:segmentStart] + segment[:dot] + extension, true
}

func braceExtensions(segment string) (map[string]struct{}, string, bool) {
	if !strings.HasSuffix(segment, "}") {
		return nil, "", false
	}
	open := strings.LastIndex(segment, "{")
	if open < 1 || segment[open-1] != '.' || strings.ContainsAny(segment[open:], "*?[]\\") {
		return nil, "", false
	}

	parts := strings.Split(segment[open+1:len(segment)-1], ",")
	if len(parts) < 2 {
		return nil, "", false
	}

	extensions := make(map[string]struct{}, len(parts))
	normalizedParts := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" || strings.ContainsAny(part, ".{}") {
			return nil, "", false
		}
		extension := "." + strings.ToLower(part)
		extensions[extension] = struct{}{}
		normalizedParts = append(normalizedParts, strings.TrimPrefix(extension, "."))
	}
	return extensions, segment[:open] + "{" + strings.Join(normalizedParts, ",") + "}", true
}
