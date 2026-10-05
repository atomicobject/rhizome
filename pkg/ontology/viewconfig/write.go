package viewconfig

import (
	"bytes"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// SavedLayout is the view state Save view writes (SPEC-0112): the defaults
// and the table and kanban settings it owns. Empty search, filters, and sort
// remove their key, so the file says only what the reader chose. A nil
// group, density, column field, or lane field, or empty columns, leaves the
// file's key as it is; an empty density or lane field removes it.
type SavedLayout struct {
	Variant     string
	Search      string
	Filters     []FilterSpec
	Sort        []SortSpec
	Group       *GroupSpec
	Columns     []ViewColumn
	Density     *string
	ColumnField *string
	LaneField   *string
}

// Marshal renders a new view file in the repository's two-space style.
func Marshal(def ViewDefinition) ([]byte, error) {
	var node yaml.Node
	if err := node.Encode(def); err != nil {
		return nil, err
	}
	return encodeNode(&node)
}

// ApplySavedLayout rewrites one view file's YAML with layout's defaults and
// variants keys, keeping every other key and comment in place.
func ApplySavedLayout(data []byte, layout SavedLayout) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("view file is not a YAML mapping")
	}
	root := doc.Content[0]
	defaults := ensureMapping(root, "defaults")
	if layout.Variant != "" {
		if err := setKey(defaults, "variant", layout.Variant); err != nil {
			return nil, err
		}
	}
	for _, item := range []struct {
		key   string
		value any
		set   bool
	}{
		{"search", layout.Search, layout.Search != ""},
		{"filters", layout.Filters, len(layout.Filters) > 0},
		{"sort", layout.Sort, len(layout.Sort) > 0},
	} {
		if err := setOrDelete(defaults, item.key, item.value, item.set); err != nil {
			return nil, err
		}
	}
	if layout.Group != nil {
		if err := setKey(defaults, "group", layout.Group); err != nil {
			return nil, err
		}
	}
	variants := ensureMapping(root, "variants")
	table := mappingValue(variants, "table")
	if len(layout.Columns) > 0 {
		table = ensureMapping(variants, "table")
		if err := setKey(table, "columns", layout.Columns); err != nil {
			return nil, err
		}
	}
	if table != nil && layout.Density != nil {
		if err := setOrDelete(table, "density", *layout.Density, *layout.Density != ""); err != nil {
			return nil, err
		}
	}
	if kanban := mappingValue(variants, "kanban"); kanban != nil {
		for _, item := range []struct {
			key   string
			value *string
		}{{"columnField", layout.ColumnField}, {"laneField", layout.LaneField}} {
			if item.value == nil {
				continue
			}
			if err := setOrDelete(kanban, item.key, *item.value, *item.value != ""); err != nil {
				return nil, err
			}
		}
	}
	return encodeNodeIndented(&doc, indentWidth(data))
}

// indentWidth is the smallest indentation the file uses, so a rewrite keeps
// its style; files without nesting get the repository's two spaces.
func indentWidth(data []byte) int {
	width := 0
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimLeft(line, " ")
		if indent := len(line) - len(trimmed); indent > 0 && trimmed != "" && !strings.HasPrefix(trimmed, "#") && (width == 0 || indent < width) {
			width = indent
		}
	}
	return max(width, 2)
}

func encodeNode(node *yaml.Node) ([]byte, error) {
	return encodeNodeIndented(node, 2)
}

func encodeNodeIndented(node *yaml.Node, indent int) ([]byte, error) {
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(indent)
	if err := encoder.Encode(node); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

// ensureMapping returns key's mapping value, replacing a non-mapping value and
// appending the key when absent.
func ensureMapping(mapping *yaml.Node, key string) *yaml.Node {
	if value := mappingValue(mapping, key); value != nil && value.Kind == yaml.MappingNode {
		return value
	}
	value := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	replaceValue(mapping, key, value)
	return value
}

func setOrDelete(mapping *yaml.Node, key string, value any, set bool) error {
	if set {
		return setKey(mapping, key, value)
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content = append(mapping.Content[:i], mapping.Content[i+2:]...)
			return nil
		}
	}
	return nil
}

func setKey(mapping *yaml.Node, key string, value any) error {
	var node yaml.Node
	if err := node.Encode(value); err != nil {
		return err
	}
	replaceValue(mapping, key, &node)
	return nil
}

// replaceValue swaps key's value node, keeping comments written on the old
// value, or appends the key.
func replaceValue(mapping *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			old := mapping.Content[i+1]
			value.HeadComment, value.LineComment, value.FootComment = old.HeadComment, old.LineComment, old.FootComment
			mapping.Content[i+1] = value
			return
		}
	}
	mapping.Content = append(mapping.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
}
