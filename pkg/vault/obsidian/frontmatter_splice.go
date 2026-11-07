package obsidian

import (
	"bytes"
	"reflect"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"gopkg.in/yaml.v3"
)

// spliceFrontmatter rewrites only the top-level frontmatter entries whose
// decoded value differs between original and updated, so every other
// frontmatter byte survives: comments, key order and spelling, quoting and
// line endings. original must come from the decoder the edit started from.
// A changed entry is re-encoded under its authored key; a new one is
// appended. Frontmatter whose entries cannot be cut apart by line (a flow
// mapping, or aliases that tie entries together) is re-encoded whole.
func spliceFrontmatter(content string, original, updated map[string]interface{}) (string, bool) {
	start, end, bodyStart, ok := markdownsyntax.FrontmatterSpan(content)
	if !ok {
		return content, false
	}
	if len(updated) == 0 {
		return content[bodyStart:], true
	}
	raw := content[start:end]
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(raw), &doc); err != nil {
		return content, false
	}
	var keys []*yaml.Node
	if len(doc.Content) > 0 {
		mapping := doc.Content[0]
		if mapping.Kind != yaml.MappingNode {
			return content, false
		}
		if mapping.Style&yaml.FlowStyle != 0 || hasYAMLAlias(mapping) {
			return remarshalFrontmatter(content, updated)
		}
		for i := 0; i < len(mapping.Content); i += 2 {
			// Re-encoded entries start at column 1, so an indented root
			// mapping would mix indentations; rewrite it whole instead.
			if mapping.Content[i].Column != 1 {
				return remarshalFrontmatter(content, updated)
			}
			keys = append(keys, mapping.Content[i])
		}
	}

	lines := strings.SplitAfter(raw, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	// Entry i owns its key line through the line before the next key, minus
	// trailing blank lines and column-0 comments, which head the next entry.
	spanEnd := make([]int, len(keys))
	for i, key := range keys {
		from, to := key.Line-1, len(lines)
		if i+1 < len(keys) {
			to = keys[i+1].Line - 1
		}
		if from < 0 || to <= from || to > len(lines) {
			return remarshalFrontmatter(content, updated)
		}
		for to > from+1 && (strings.TrimSpace(lines[to-1]) == "" || strings.HasPrefix(lines[to-1], "#")) {
			to--
		}
		spanEnd[i] = to
	}

	eol := "\n"
	if strings.Contains(content[:bodyStart], "\r\n") {
		eol = "\r\n"
	}
	names := make([]string, 0, len(original)+len(updated))
	for name := range original {
		names = append(names, name)
	}
	for name := range updated {
		if _, seen := original[name]; !seen {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	edits := map[int]string{} // key index -> replacement; "" deletes the entry
	var additions []string
	for _, name := range names {
		before, wasSet := original[name]
		after, isSet := updated[name]
		if wasSet && isSet && reflect.DeepEqual(before, after) {
			continue
		}
		src := authoredKeyIndex(keys, name)
		if _, taken := edits[src]; src >= 0 && taken {
			return remarshalFrontmatter(content, updated)
		}
		if !isSet {
			if src >= 0 {
				edits[src] = ""
			}
			continue
		}
		key := name
		if src >= 0 {
			key = keys[src].Value
		}
		entry, err := encodeFrontmatterEntry(key, after, eol)
		if err != nil {
			return content, false
		}
		if src >= 0 {
			edits[src] = entry
		} else {
			additions = append(additions, entry)
		}
	}

	var out strings.Builder
	out.WriteString(content[:start])
	next := 0
	for i, key := range keys {
		replacement, edited := edits[i]
		if !edited {
			continue
		}
		out.WriteString(strings.Join(lines[next:key.Line-1], ""))
		out.WriteString(replacement)
		next = spanEnd[i]
	}
	out.WriteString(strings.Join(lines[next:], ""))
	out.WriteString(strings.Join(additions, ""))
	out.WriteString(content[end:])
	return out.String(), true
}

// authoredKeyIndex finds the authored key for a decoded name, preferring an
// exact match and falling back to a unique case-insensitive one (the decoder
// may have folded "Tags" to "tags").
func authoredKeyIndex(keys []*yaml.Node, name string) int {
	folded := -1
	for i, key := range keys {
		if key.Value == name {
			return i
		}
		if strings.EqualFold(key.Value, name) {
			if folded >= 0 {
				return -1
			}
			folded = i
		}
	}
	return folded
}

func encodeFrontmatterEntry(key string, value interface{}, eol string) (string, error) {
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(map[string]interface{}{key: value}); err != nil {
		return "", err
	}
	if err := encoder.Close(); err != nil {
		return "", err
	}
	return strings.ReplaceAll(buf.String(), "\n", eol), nil
}

func hasYAMLAlias(node *yaml.Node) bool {
	if node.Kind == yaml.AliasNode {
		return true
	}
	for _, child := range node.Content {
		if hasYAMLAlias(child) {
			return true
		}
	}
	return false
}

// remarshalFrontmatter replaces the whole frontmatter block with updated.
func remarshalFrontmatter(content string, updated map[string]interface{}) (string, bool) {
	_, bodyStart, ok := markdownsyntax.Frontmatter(content)
	if !ok {
		return content, false
	}
	yamlBytes, err := yaml.Marshal(updated)
	if err != nil {
		return content, false
	}
	return "---\n" + string(yamlBytes) + "---\n" + content[bodyStart:], true
}
