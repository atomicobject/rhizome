package ontology

import (
	"sort"
	"strings"
)

func effectiveTypeRole(noteType *NoteType) TypeRole {
	if noteType == nil {
		return ""
	}
	if noteType.Role == "" {
		return TypeRoleNote
	}
	return noteType.Role
}

func typeMatchesOrImplements(schema *Schema, actual, expected string) bool {
	actual = strings.TrimSpace(actual)
	expected = strings.TrimSpace(expected)
	if actual == "" || expected == "" {
		return false
	}
	if actual == expected {
		return true
	}
	if expected == "Note" {
		return true
	}
	if actual == "Section" {
		return expected == "Section"
	}
	if schema == nil {
		return false
	}
	if noteType := schema.Types[actual]; noteType != nil {
		for _, iface := range noteType.Implements {
			if iface == expected || interfaceImplements(schema, iface, expected) {
				return true
			}
		}
	}
	return interfaceImplements(schema, actual, expected)
}

// TypeMatchesOrImplements reports whether actual is expected or implements it
// under the compiled ontology contract. Consumers resolving typed references
// must use this helper so interface semantics do not drift across subsystems.
func TypeMatchesOrImplements(schema *Schema, actual, expected string) bool {
	return typeMatchesOrImplements(schema, actual, expected)
}

// InterfaceImplementors maps each schema interface to the sorted concrete
// types that implement it, directly or through another interface.
func InterfaceImplementors(schema *Schema) map[string][]string {
	out := map[string][]string{}
	if schema == nil {
		return out
	}
	for ifaceName := range schema.Interfaces {
		for typeName := range schema.Types {
			if typeMatchesOrImplements(schema, typeName, ifaceName) {
				out[ifaceName] = append(out[ifaceName], typeName)
			}
		}
		sort.Strings(out[ifaceName])
	}
	return out
}

func interfaceImplements(schema *Schema, actual, expected string) bool {
	actual = strings.TrimSpace(actual)
	expected = strings.TrimSpace(expected)
	if actual == "" || expected == "" {
		return false
	}
	if actual == expected {
		return true
	}
	if actual == "Section" {
		return expected == "Section"
	}
	if schema == nil {
		return false
	}
	iface := schema.Interfaces[actual]
	if iface == nil {
		return false
	}
	for _, name := range iface.Implements {
		if name == expected || interfaceImplements(schema, name, expected) {
			return true
		}
	}
	return false
}

func interfaceImplementersOrConcrete(schema *Schema, typeName string) []*NoteType {
	if schema == nil {
		return nil
	}
	if noteType := schema.Types[typeName]; noteType != nil {
		return []*NoteType{noteType}
	}
	if typeName == "Section" {
		out := make([]*NoteType, 0)
		for _, noteType := range schema.Types {
			if noteType != nil && (effectiveTypeRole(noteType) == TypeRoleSection || effectiveTypeRole(noteType) == TypeRoleEmbeddedNode) {
				out = append(out, noteType)
			}
		}
		return out
	}
	if _, ok := schema.Interfaces[typeName]; !ok {
		return nil
	}
	out := make([]*NoteType, 0)
	for _, noteType := range schema.Types {
		if noteType != nil && typeMatchesOrImplements(schema, noteType.Name, typeName) {
			out = append(out, noteType)
		}
	}
	return out
}
