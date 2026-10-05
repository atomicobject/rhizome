package ontology

import (
	"fmt"
	"github.com/vektah/gqlparser/v2/ast"
	"regexp"
	"sort"
	"strings"
)

// applyDerivableIdentifierArgs reads `derivable` + `derivedSuffix` off an
// @identifier directive and stamps them onto the field. The default for
// `derivable` depends on the containing type role: true for embedded
// preferred-identifier fields (their semantic id derives from parent + position),
// false for everything else (top-level note ids must be authored).
//
// WHY: derivation answers "where does the value come from"; population answers
// "when should markdown carry it." Some embedded identifiers, such as
// UserStory.id, derive from the parent but should still be authored on create.
// Other embedded nodes may have no semantic identifier at all and rely on
// ordinary block locators when they need external links.
func applyDerivableIdentifierArgs(field *Field, parent *ast.Definition, fieldDef *ast.FieldDefinition, parentRole TypeRole, args map[string]any) error {
	if !field.IsPreferredIdentifier {
		// `derivable`, `derivedSuffix`, and `populate` only apply to preferred identifiers.
		// Reject explicit use elsewhere so typos surface instead of silently
		// being ignored.
		if _, ok := args["derivable"]; ok {
			return compileError(fieldDef.Position, "@identifier on %s.%s: `derivable` requires `preferred: true`", parent.Name, fieldDef.Name)
		}
		if _, ok := args["derivedSuffix"]; ok {
			return compileError(fieldDef.Position, "@identifier on %s.%s: `derivedSuffix` requires `preferred: true`", parent.Name, fieldDef.Name)
		}
		if _, ok := args["populate"]; ok {
			return compileError(fieldDef.Position, "@identifier on %s.%s: `populate` requires `preferred: true`", parent.Name, fieldDef.Name)
		}
		return nil
	}
	defaultDerivable := parentRole == TypeRoleEmbeddedNode
	derivable := boolArg(args["derivable"], defaultDerivable)
	suffix := strings.TrimSpace(stringArg(args["derivedSuffix"], ""))
	if derivable {
		if parentRole != TypeRoleEmbeddedNode {
			return compileError(fieldDef.Position, "@identifier on %s.%s: `derivable: true` requires the containing type to be @node(locator: EMBEDDED)", parent.Name, fieldDef.Name)
		}
		if suffix == "" {
			return compileError(fieldDef.Position, "@identifier on %s.%s: `derivable: true` requires a non-empty `derivedSuffix` (e.g. \"US\" for UserStory, \"AC\" for AcceptanceCriterion)", parent.Name, fieldDef.Name)
		}
	}
	field.IsDerivableIdentifier = derivable
	field.DerivedSuffix = suffix
	field.IdentifierPopulate = identifierPopulateArg(args["populate"], defaultIdentifierPopulate(field, derivable))
	return nil
}

func defaultIdentifierPopulate(field *Field, derivable bool) IdentifierPopulate {
	if field == nil || !field.Required {
		return IdentifierPopulateManual
	}
	if derivable {
		return IdentifierPopulateOnLink
	}
	return IdentifierPopulateOnCreate
}

func identifierPopulateArg(v any, fallback IdentifierPopulate) IdentifierPopulate {
	raw := strings.ToUpper(strings.TrimSpace(stringArg(v, string(fallback))))
	switch raw {
	case string(IdentifierPopulateManual):
		return IdentifierPopulateManual
	case string(IdentifierPopulateOnCreate):
		return IdentifierPopulateOnCreate
	case string(IdentifierPopulateOnLink):
		return IdentifierPopulateOnLink
	default:
		return fallback
	}
}

func applyIdentifierDirective(field *Field, parent *ast.Definition, fieldDef *ast.FieldDefinition, parentRole TypeRole, dir *ast.Directive) error {
	if field.Kind != FieldKindScalar {
		return compileError(fieldDef.Position, "@identifier on %s.%s requires a scalar String field", parent.Name, fieldDef.Name)
	}
	if field.TypeName != "String" && field.TypeName != "ID" {
		return compileError(fieldDef.Position, "@identifier on %s.%s requires a String or ID field (got %s)", parent.Name, fieldDef.Name, field.TypeName)
	}
	if field.List {
		return compileError(fieldDef.Position, "@identifier on %s.%s cannot be a list field", parent.Name, fieldDef.Name)
	}
	if parentRole != TypeRoleSection && parentRole != TypeRoleEmbeddedNode && field.SourceKind != FieldSourceFrontmatter {
		// Note identifiers are mirrored into aliases and require frontmatter.
		return compileError(fieldDef.Position, "@identifier on %s.%s requires sourceKind: FRONTMATTER", parent.Name, fieldDef.Name)
	}
	if field.SourceKind != FieldSourceFrontmatter && field.SourceKind != FieldSourceInline {
		return compileError(fieldDef.Position, "@identifier on %s.%s requires a frontmatter or inline field", parent.Name, fieldDef.Name)
	}
	args := dir.ArgumentMap(nil)
	field.IsIdentifier = true
	field.IsPreferredIdentifier = boolArg(args["preferred"], false)
	format, formatErr := parseIdentifierFormat(args, dir.Arguments.ForName("pad") != nil)
	if formatErr != nil {
		return compileError(fieldDef.Position, "@identifier on %s.%s: %v", parent.Name, fieldDef.Name, formatErr)
	}
	field.IdentifierFormat = format
	return applyDerivableIdentifierArgs(field, parent, fieldDef, parentRole, args)
}

// identifierPrefixPattern enforces the SPEC-0006 convention that id prefixes
// are uppercase letters/digits starting with a letter. It is intentionally
// strict so the directive cannot be used to encode lowercase, dotted, or
// punctuation-bearing prefixes that would defeat ordinary `<prefix>-NNNN`
// parsing.
var identifierPrefixPattern = regexp.MustCompile(`^[A-Z][A-Z0-9]{0,15}$`)

// parseIdentifierFormat reads optional strategy/prefix/pad/separator args from
// an @identifier directive and returns populated allocation metadata when
// prefix is set. Without prefix the directive remains valid (backwards-
// compatible with free-form identifiers) but agents and tooling treat the
// type as unsupported by automated id allocation. Defaults are compiled into
// runtime metadata only; loading a schema never backfills source SDL.
func parseIdentifierFormat(args map[string]any, padExplicit bool) (*IdentifierFormat, error) {
	prefix := stringArg(args["prefix"], "")
	if prefix == "" {
		return nil, nil
	}
	strategy := IdentifierStrategy(stringArg(args["strategy"], string(IdentifierStrategySequential)))
	if strategy != IdentifierStrategySequential && strategy != IdentifierStrategyDateTime {
		return nil, fmt.Errorf("unsupported IdentifierStrategy %q", strategy)
	}
	if !identifierPrefixPattern.MatchString(prefix) {
		return nil, fmt.Errorf("prefix %q must match %s", prefix, identifierPrefixPattern.String())
	}
	separator := stringArg(args["separator"], "-")
	if separator == "" {
		return nil, fmt.Errorf("separator cannot be empty")
	}
	pad := 0
	if strategy == IdentifierStrategySequential {
		pad = intArg(args["pad"], 4)
		if pad < 1 || pad > 8 {
			return nil, fmt.Errorf("pad %d out of range [1,8]", pad)
		}
	} else if padExplicit {
		return nil, fmt.Errorf("pad is not valid for DATETIME identifiers")
	}
	return &IdentifierFormat{
		Strategy:  strategy,
		Prefix:    prefix,
		Separator: separator,
		Pad:       pad,
	}, nil
}

// validateIdentifierNamespaces prevents different codecs from claiming the
// same rendered prefix/separator namespace. Pool discovery can then group on
// namespace without hiding collisions behind strategy-specific parsing.
func validateIdentifierNamespaces(schema *Schema) error {
	type declaration struct {
		typeName string
		field    *Field
	}
	namespaces := make(map[string]declaration)
	typeNames := make([]string, 0, len(schema.Types))
	for typeName := range schema.Types {
		typeNames = append(typeNames, typeName)
	}
	sort.Strings(typeNames)
	for _, typeName := range typeNames {
		noteType := schema.Types[typeName]
		for _, field := range noteType.Fields {
			if field == nil || !field.IsPreferredIdentifier || field.IdentifierFormat == nil {
				continue
			}
			format := field.IdentifierFormat
			namespace := strings.ToLower(format.Prefix + format.Separator)
			prior, exists := namespaces[namespace]
			if !exists {
				namespaces[namespace] = declaration{typeName: typeName, field: field}
				continue
			}
			priorFormat := prior.field.IdentifierFormat
			compatible := priorFormat.Strategy == format.Strategy
			if compatible && format.Strategy == IdentifierStrategySequential {
				compatible = priorFormat.Pad == format.Pad
			}
			if !compatible {
				return fmt.Errorf("identifier namespace %q has incompatible declarations: %s.%s uses strategy %s with pad %d, while %s.%s uses strategy %s with pad %d",
					format.Prefix+format.Separator,
					prior.typeName, prior.field.Name, priorFormat.Strategy, priorFormat.Pad,
					typeName, field.Name, format.Strategy, format.Pad)
			}
		}
	}
	return nil
}
