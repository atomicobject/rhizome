package schema

import (
	"fmt"
	"regexp"
	"strings"
)

type RecordExpansion string

const (
	RecordExpandSequence        RecordExpansion = "[]"
	RecordExpandMapping         RecordExpansion = "{}"
	RecordExpandMappingSequence RecordExpansion = "{}[]"
)

type RecordSelectorStep struct {
	Name      string
	Expansion RecordExpansion
}
type RecordSelector struct {
	Text  string
	Steps []RecordSelectorStep
}
type RecordKey struct {
	Field      string
	MappingKey bool
	Positional bool
	Template   []RecordTemplatePart
}
type RecordTemplatePart struct {
	Literal string
	Field   string
}
type RecordMembership struct {
	Path           RecordSelector
	Includes       string
	Wildcard       *string
	IncludeMissing bool
}
type RecordSourceBinding struct {
	Select                                  RecordSelector
	Key                                     RecordKey
	Title                                   []string
	TitleSelectors                          []RecordSelector
	ScalarAs, Container, Space, LogicalKind string
	Membership                              *RecordMembership
	SourceOrder                             int
}
type RecordEdge struct {
	Name      string
	Direction NeighborDirection
}
type RecordTarget struct {
	Tag, Type  string
	MemberPath RecordSelector
}
type RecordReferenceBinding struct {
	To                     []string
	Mode, Encoding, Values string
	Targets                []RecordTarget
	MatchName              string
	TargetKey              string
	MatchPath              RecordSelector
	AllowWildcard          bool
	Edge                   *RecordEdge
}
type RecordFieldBinding struct {
	Path               RecordSelector
	MappingKeyFallback bool
	References         *RecordReferenceBinding
}
type RecordContainmentBinding struct {
	Select RecordSelector
	Edge   *RecordEdge
}

var recordPropertyName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

// CompileRecordSelector accepts the bounded source-local collection grammar.
func CompileRecordSelector(text string) (RecordSelector, error) {
	out := RecordSelector{Text: text}
	if text == "" {
		return out, nil
	}
	for _, part := range strings.Split(text, ".") {
		step := RecordSelectorStep{}
		switch {
		case strings.HasSuffix(part, "{}[]"):
			step.Expansion = RecordExpandMappingSequence
			part = strings.TrimSuffix(part, "{}[]")
		case strings.HasSuffix(part, "[]"):
			step.Expansion = RecordExpandSequence
			part = strings.TrimSuffix(part, "[]")
		case strings.HasSuffix(part, "{}"):
			step.Expansion = RecordExpandMapping
			part = strings.TrimSuffix(part, "{}")
		}
		if !recordPropertyName.MatchString(part) {
			return out, fmt.Errorf("record_selector_unsupported: %q", text)
		}
		step.Name = part
		out.Steps = append(out.Steps, step)
	}
	return out, nil
}

func compileRecordTemplate(text string) ([]RecordTemplatePart, error) {
	var out []RecordTemplatePart
	var literal strings.Builder
	flush := func() {
		if literal.Len() > 0 {
			out = append(out, RecordTemplatePart{Literal: literal.String()})
			literal.Reset()
		}
	}
	for i := 0; i < len(text); {
		switch text[i] {
		case '{':
			if i+1 < len(text) && text[i+1] == '{' {
				literal.WriteByte('{')
				i += 2
				continue
			}
			end := strings.IndexByte(text[i+1:], '}')
			if end < 0 {
				return nil, fmt.Errorf("unclosed template field")
			}
			end += i + 1
			name := text[i+1 : end]
			if !recordPropertyName.MatchString(name) {
				return nil, fmt.Errorf("unsupported template field %q", name)
			}
			flush()
			out = append(out, RecordTemplatePart{Field: name})
			i = end + 1
		case '}':
			if i+1 >= len(text) || text[i+1] != '}' {
				return nil, fmt.Errorf("unmatched template brace")
			}
			literal.WriteByte('}')
			i += 2
		default:
			literal.WriteByte(text[i])
			i++
		}
	}
	flush()
	return out, nil
}
