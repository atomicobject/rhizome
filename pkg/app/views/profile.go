package views

import (
	"slices"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	"github.com/atomicobject/rhizome/pkg/vault/identity"
)

// sourceSubject names the type or interface an ontology-sourced view reads.
func sourceSubject(def viewconfig.ViewDefinition) string {
	switch def.SourceSpec.Kind {
	case viewconfig.SourceKindOntologyType:
		return def.SourceSpec.Type
	case viewconfig.SourceKindOntologyInterface:
		return def.SourceSpec.Interface
	default:
		return ""
	}
}

// subjectProfile derives the SPEC-0112 profile of a type or interface.
func subjectProfile(schema *ontology.Schema, name string) (ontology.TypeProfile, bool) {
	if schema == nil || strings.TrimSpace(name) == "" {
		return ontology.TypeProfile{}, false
	}
	return ontology.DeriveTypeProfile(schema, name, identity.CurrentUserType)
}

// sourceProfile is the profile of a view's ontology source; ok is false for
// query recipe and custom sources.
func sourceProfile(schema *ontology.Schema, def viewconfig.ViewDefinition) (ontology.TypeProfile, bool) {
	return subjectProfile(schema, sourceSubject(def))
}

// schemaFieldRole names a schema field's capability role from schema metadata
// and the profile, never from the field's name. The web reads "identifier",
// "status" (the lifecycle field), "summary", and "date".
func schemaFieldRole(profile *ontology.TypeProfile, field *ontology.Field) string {
	switch {
	case field == nil:
		return ""
	case field.IsIdentifier || field.IsPreferredIdentifier || field.IsDerivableIdentifier:
		return "identifier"
	case profile != nil && field.Name == profile.LifecycleField:
		return "status"
	case profile != nil && field.Name == profile.SummaryField:
		return "summary"
	case isDateField(field):
		return "date"
	default:
		return ""
	}
}

func isDateField(field *ontology.Field) bool {
	if field == nil || field.Kind != ontology.FieldKindScalar {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(field.TypeName)) {
	case "date", "datetime":
		return true
	default:
		return false
	}
}

// subjectFields lists a type's or interface's schema fields.
func subjectFields(schema *ontology.Schema, name string) []*ontology.Field {
	if schema == nil {
		return nil
	}
	if noteType := schema.Types[name]; noteType != nil {
		return noteType.Fields
	}
	if iface := schema.Interfaces[name]; iface != nil {
		return iface.Fields
	}
	return nil
}

func schemaFieldByName(fields []*ontology.Field, name string) *ontology.Field {
	for _, field := range fields {
		if field != nil && field.Name == name {
			return field
		}
	}
	return nil
}

// lifecycleEnum returns the enum type behind a profile's lifecycle field.
func lifecycleEnum(schema *ontology.Schema, fields []*ontology.Field, profile ontology.TypeProfile) *ontology.EnumType {
	field := schemaFieldByName(fields, profile.LifecycleField)
	if field == nil || schema == nil {
		return nil
	}
	return schema.EnumTypes[field.TypeName]
}

// stageRank orders lifecycle stages for grouping: in motion first, then
// waiting, then finished, then dropped; values without a stage come last.
func stageRank(stage ontology.LifecycleStage) int {
	if index := slices.Index([]ontology.LifecycleStage{ontology.StageActive, ontology.StageOpen, ontology.StageDone, ontology.StageDropped}, stage); index >= 0 {
		return index
	}
	return 4
}

// enumValueStages maps an enum's value names to their effective stages.
func enumValueStages(enumType *ontology.EnumType) map[string]ontology.LifecycleStage {
	if enumType == nil {
		return nil
	}
	stages := enumType.Stages()
	if len(stages) != len(enumType.Values) {
		return nil
	}
	out := make(map[string]ontology.LifecycleStage, len(stages))
	for index, value := range enumType.Values {
		if value != nil {
			out[value.Name] = stages[index].Stage
		}
	}
	return out
}

// builtinRole names the roles of row fields every source has.
func builtinRole(key string) string {
	switch key {
	case "path", "notePath":
		return "path"
	case "title":
		return "title"
	case "updatedAt":
		return "system"
	default:
		return ""
	}
}

// semanticRoleForKey guesses a role from a field's name. Only sources without
// schema fields, such as query recipes over non-ontology rows, use it.
func semanticRoleForKey(key string) string {
	lower := strings.ToLower(canonicalCapabilityField(key))
	switch {
	case builtinRole(key) != "":
		return builtinRole(key)
	case strings.Contains(lower, "updated") || strings.Contains(lower, "created"):
		return "system"
	case lower == "id" || strings.HasSuffix(lower, "id"):
		return "identifier"
	case lower == "status" || strings.HasSuffix(lower, "status") || lower == "done" ||
		lower == "state" || strings.HasSuffix(lower, "state") || lower == "phase":
		return "status"
	case lower == "summary":
		return "summary"
	case strings.Contains(lower, "date") || strings.Contains(lower, "due"):
		return "date"
	default:
		return ""
	}
}
