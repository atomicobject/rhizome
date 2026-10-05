package ontology

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

type setRawFrontmatterListOp struct {
	Ref    NodeRef
	Field  string
	Values []string
}

// SetRawFrontmatterList stages an explicit note-root frontmatter list edit.
// Unlike SetScalarListField, the field need not be declared in the ontology.
func (s *EditSession) SetRawFrontmatterList(ref NodeRef, field string, values []string) error {
	return s.stage(setRawFrontmatterListOp{
		Ref: ref, Field: strings.TrimSpace(field), Values: append([]string(nil), values...),
	})
}

func (op setRawFrontmatterListOp) notePath() string { return op.Ref.NotePath }

func (op setRawFrontmatterListOp) apply(state *documentState) error {
	_, projection, err := projectState(state, op.Ref)
	if err != nil {
		return err
	}
	if projection.Ref.Kind != NodeKindNote {
		return fmt.Errorf("%w: raw frontmatter list requires a note root", errUnsupportedTarget)
	}
	if op.Field == "" {
		return fmt.Errorf("%w: raw frontmatter list field is required", errUnsupportedTarget)
	}
	updated, err := setRawFrontmatterList(state.content, op.Field, op.Values)
	if err != nil {
		return err
	}
	state.setContent(updated)
	return nil
}

func setRawFrontmatterList(content, field string, values []string) (string, error) {
	if updated, ok, err := patchFrontmatterFieldSource(content, &Field{
		Name: field, Source: field, Kind: FieldKindScalar, TypeName: "String", List: true,
	}, values, false); err != nil {
		return "", err
	} else if ok {
		return updated, nil
	}
	doc, mapping, err := parseFrontmatterNode(content)
	if err != nil {
		return "", err
	}
	setYAMLSequenceValuePreservingStyle(mapping, field, values)
	yamlText, err := encodeFrontmatterNode(doc)
	if err != nil {
		return "", err
	}
	fmRange := frontmatterRange(content)
	if fmRange.Len() == 0 {
		return "---\n" + yamlText + "---\n" + content, nil
	}
	return replaceRange(content, fmRange, "---\n"+yamlText+"---\n"), nil
}

func setYAMLSequenceValuePreservingStyle(mapping *yaml.Node, key string, values []string) {
	next := yamlSequenceNode(values)
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value != key {
			continue
		}
		current := mapping.Content[i+1]
		if current.Kind == yaml.SequenceNode {
			next.Style = current.Style
			next.HeadComment = current.HeadComment
			next.LineComment = current.LineComment
			next.FootComment = current.FootComment
		}
		mapping.Content[i+1] = next
		return
	}
	mapping.Content = append(mapping.Content, scalarYAMLNode(key), next)
}
