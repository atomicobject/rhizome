package markdown

import (
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"gopkg.in/yaml.v3"
)

// frontmatterWithTemporalProvenance retains YAML timestamp spelling where the
// generic frontmatter decoder has reduced both dates and timestamps to
// time.Time. It leaves all non-temporal values on the existing parser path.
func frontmatterWithTemporalProvenance(content string, frontmatter map[string]any) map[string]any {
	if len(frontmatter) == 0 {
		return frontmatter
	}
	raw, ok := rawFrontmatterYAML(content)
	if !ok {
		return frontmatter
	}
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(raw), &document); err != nil {
		return frontmatter
	}
	root := yamlRoot(&document)
	if root == nil || root.Kind != yaml.MappingNode {
		return frontmatter
	}
	withProvenance := make(map[string]any, len(frontmatter))
	for key, value := range frontmatter {
		withProvenance[key] = annotateYAMLTemporalValue(value, yamlMappingValue(root, key))
	}
	return withProvenance
}

// rawFrontmatterYAML deliberately mirrors the fence boundary used by
// obsidian.ExtractFrontmatter. Parsing and ordinary frontmatter behavior stay
// owned by that existing primitive; this raw slice is only temporal evidence.
func rawFrontmatterYAML(content string) (string, bool) {
	if !strings.HasPrefix(content, "---") {
		return "", false
	}
	end := strings.Index(content[3:], "\n---")
	if end == -1 {
		return "", false
	}
	return content[3 : 3+end], true
}

func yamlRoot(document *yaml.Node) *yaml.Node {
	if document == nil {
		return nil
	}
	if document.Kind == yaml.DocumentNode && len(document.Content) == 1 {
		return document.Content[0]
	}
	return document
}

func yamlMappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			return mapping.Content[index+1]
		}
	}
	// ExtractFrontmatter canonicalizes the tags key case-insensitively.
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if strings.EqualFold(mapping.Content[index].Value, key) {
			return mapping.Content[index+1]
		}
	}
	return nil
}

func annotateYAMLTemporalValue(value any, node *yaml.Node) any {
	if node == nil {
		return value
	}
	if node.Kind == yaml.AliasNode {
		node = node.Alias
		if node == nil {
			return value
		}
	}
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Tag != "!!timestamp" {
			return value
		}
		at, ok := value.(time.Time)
		if !ok {
			return value
		}
		if isDateOnlyYAMLTimestamp(node.Value) {
			return noteformat.NewDateMetadataValue(at)
		}
		return noteformat.NewTimestampMetadataValue(at)
	case yaml.SequenceNode:
		items, ok := value.([]any)
		if !ok || len(items) != len(node.Content) {
			return value
		}
		out := make([]any, len(items))
		for index, item := range items {
			out[index] = annotateYAMLTemporalValue(item, node.Content[index])
		}
		return out
	case yaml.MappingNode:
		entries, ok := value.(map[string]any)
		if !ok {
			return value
		}
		out := make(map[string]any, len(entries))
		for key, item := range entries {
			out[key] = annotateYAMLTemporalValue(item, yamlMappingValue(node, key))
		}
		return out
	default:
		return value
	}
}

func isDateOnlyYAMLTimestamp(value string) bool {
	date, err := time.Parse(time.DateOnly, value)
	return err == nil && date.Format(time.DateOnly) == value
}
