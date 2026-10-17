package schema

import (
	"strings"
)

// FallbackNoteTypeName is the reserved internal ontology type used to give
// genuinely untyped note roots the same semantic surface as typed notes.
const FallbackNoteTypeName = "_FallbackNote"

// FallbackSectionTypeName is the reserved internal ontology type used to give
// authored sections inside untyped notes stable node identity.
const FallbackSectionTypeName = "_FallbackSection"

// IsFallbackNoteTypeName reports whether typeName is the internal fallback
// note type.
func IsFallbackNoteTypeName(typeName string) bool {
	return strings.TrimSpace(typeName) == FallbackNoteTypeName
}

// IsFallbackSectionTypeName reports whether typeName is the internal fallback
// section type.
func IsFallbackSectionTypeName(typeName string) bool {
	return strings.TrimSpace(typeName) == FallbackSectionTypeName
}

// IsFallbackTypeName reports whether typeName is one of Rhizome's internal
// fallback semantic types.
func IsFallbackTypeName(typeName string) bool {
	return IsFallbackNoteTypeName(typeName) || IsFallbackSectionTypeName(typeName)
}

type TypeVisibility string

// OntologyTypeVisibility classifies type names at public read boundaries.
// Fallback notes are first-class internal evidence, not authored schema types.
func OntologyTypeVisibility(typeName string) TypeVisibility {
	if IsFallbackTypeName(typeName) {
		return TypeVisibilityInternal
	}
	return TypeVisibilityPublic
}

const (
	TypeVisibilityPublic   TypeVisibility = "public"
	TypeVisibilityInternal TypeVisibility = "internal"
)
