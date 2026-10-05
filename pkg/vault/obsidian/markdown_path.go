package obsidian

import (
	"net/url"
	"strings"
)

// DecodeMarkdownPath decodes URL path escapes exactly once. A plus is literal;
// malformed escapes retain their authored bytes for compatibility.
func DecodeMarkdownPath(path string) string {
	decoded, err := url.PathUnescape(path)
	if err != nil {
		return path
	}
	return decoded
}

// EncodeMarkdownPath escapes destination syntax while preserving folder slashes.
func EncodeMarkdownPath(path string) string {
	segments := strings.Split(path, "/")
	for i := range segments {
		segments[i] = url.PathEscape(segments[i])
	}
	// A colon in the first segment can otherwise turn a relative path into a URI.
	segments[0] = strings.ReplaceAll(segments[0], ":", "%3A")
	return strings.Join(segments, "/")
}

// SplitMarkdownTarget separates the authored fragment before URL decoding, so
// an encoded hash in the filename cannot become a fragment delimiter.
func SplitMarkdownTarget(target string) (path, fragment string) {
	path, fragment, _ = strings.Cut(target, "#")
	return DecodeMarkdownPath(path), DecodeMarkdownPath(fragment)
}
