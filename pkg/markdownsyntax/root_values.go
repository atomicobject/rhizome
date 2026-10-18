package markdownsyntax

import (
	"path"
	"strings"
)

// TitleValue extracts the authored Markdown note title.
func TitleValue(notePath, content string, frontmatter map[string]any) string {
	for _, key := range []string{"title", "name"} {
		if value, ok := frontmatter[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	if title := SingleMarkdownH1Title(content); title != "" {
		return title
	}
	base := path.Base(notePath)
	return strings.TrimSuffix(base, path.Ext(base))
}

// TagValues extracts normalized authored frontmatter and visible inline tags.
func TagValues(content string, frontmatter map[string]any) []string {
	values := make([]string, 0)
	if raw, ok := frontmatter["tags"]; ok {
		switch tags := raw.(type) {
		case []string:
			values = append(values, tags...)
		case []any:
			for _, tag := range tags {
				if text, ok := tag.(string); ok {
					values = append(values, text)
				}
			}
		}
	}
	for _, tag := range ExtractHashtags(content) {
		values = append(values, strings.TrimPrefix(tag, "#"))
	}
	seen := make(map[string]struct{}, len(values))
	facts := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimPrefix(strings.TrimSpace(value), "#")
		if value == "" {
			continue
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		facts = append(facts, value)
	}
	return facts
}
