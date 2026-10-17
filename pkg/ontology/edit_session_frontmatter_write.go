package ontology

import (
	"fmt"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"strings"

	"github.com/atomicobject/rhizome/pkg/markdownsyntax"
	"gopkg.in/yaml.v3"
)

// PatchRootFrontmatter changes one scalar or scalar-list property while keeping
// every other property's authored bytes. A nil list selects the scalar value.
func PatchRootFrontmatter(content, property, value string, values []string) (string, error) {
	property = strings.TrimSpace(property)
	if property == "" || strings.ContainsAny(property, "\r\n\x00") {
		return "", fmt.Errorf("frontmatter property must be a nonempty single line")
	}
	field := &ontoschema.Field{Name: property, Source: property, Kind: ontoschema.FieldKindScalar, TypeName: "String", List: values != nil}
	if values == nil {
		values = []string{value}
	}
	return setFrontmatterField(content, field, values, false)
}

func setFrontmatterField(content string, field *ontoschema.Field, values []string, asLinks bool) (string, error) {
	if updated, ok, err := patchFrontmatterFieldSource(content, field, values, asLinks); err != nil {
		return "", err
	} else if ok {
		return updated, nil
	}
	prefix, body := "", content
	if strings.HasPrefix(body, "\xef\xbb\xbf") {
		prefix, body = "\xef\xbb\xbf", body[3:]
	}
	if err := validateFrontmatterLocation(content); err != nil {
		return "", err
	}
	key := field.Source
	if key == "" {
		key = ontoschema.DefaultPropertyName(field.Name, ontoschema.PropertyCaseKebab)
	}
	encodedKey, err := yaml.Marshal(key)
	if err != nil {
		return "", err
	}
	eol := documentEOL(content)
	rendered := renderFrontmatterValue("", field, values, asLinks)
	return prefix + "---" + eol + strings.TrimSpace(string(encodedKey)) + ": " + rendered + eol + "---" + eol + body, nil
}

func validateFrontmatterLocation(content string) error {
	if _, _, ok := markdownsyntax.Frontmatter(content); ok {
		return nil
	}
	trimmed := strings.TrimLeft(strings.TrimPrefix(content, "\xef\xbb\xbf"), " \t\r\n")
	if strings.HasPrefix(trimmed, "---\n") || strings.HasPrefix(trimmed, "---\r\n") {
		return fmt.Errorf("%w: frontmatter must have exact delimiters starting on the first line", errUnsupportedTarget)
	}
	return nil
}

func documentEOL(content string) string {
	if strings.Contains(content, "\r\n") {
		return "\r\n"
	}
	return "\n"
}

func parseFrontmatterNode(content string) (*yaml.Node, *yaml.Node, error) {
	raw, _, ok := markdownsyntax.Frontmatter(content)
	if !ok {
		doc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
		return doc, doc.Content[0], nil
	}
	doc := &yaml.Node{}
	if err := yaml.Unmarshal([]byte(raw), doc); err != nil {
		return nil, nil, err
	}
	if doc.Kind == 0 {
		doc = &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	}
	if doc.Kind != yaml.DocumentNode {
		doc = &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{doc}}
	}
	if len(doc.Content) == 0 || doc.Content[0] == nil {
		doc.Content = []*yaml.Node{{Kind: yaml.MappingNode}}
	}
	if doc.Content[0].Kind != yaml.MappingNode {
		return nil, nil, fmt.Errorf("frontmatter is not a mapping")
	}
	return doc, doc.Content[0], nil
}

func insertedDocumentText(content, text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", documentEOL(content))
}
