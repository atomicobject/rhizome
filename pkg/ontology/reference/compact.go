package reference

import (
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
)

// CompactResponse is the authored Markdown contract for one type. It retains
// schema-owned bindings and policies; executable query fields are a separate
// contract exposed by ontology-query-schema.
type CompactResponse struct {
	Format     string             `json:"format"`
	SchemaHash string             `json:"schemaHash"`
	Types      []ontology.TypeDoc `json:"types"`
	Expand     []Expansion        `json:"expand"`
}

type Expansion struct {
	Operation string            `json:"operation"`
	Input     map[string]string `json:"input"`
	Purpose   string            `json:"purpose"`
}

func Compact(schema *ontology.Schema, typeName string) (CompactResponse, error) {
	typeName = strings.TrimSpace(typeName)
	if typeName == "" {
		return CompactResponse{}, fmt.Errorf("compact ontology reference requires a type")
	}
	if ontology.OntologyTypeVisibility(typeName) == ontology.TypeVisibilityInternal {
		return CompactResponse{}, fmt.Errorf("ontology type %q is internal", typeName)
	}
	docs, err := ontology.SchemaDocs(schema, typeName)
	if err != nil {
		return CompactResponse{}, err
	}
	for i := range docs {
		doc := &docs[i]
		doc.Summary, doc.Meaning, doc.Authoring, doc.AgentImplications, doc.Description = "", "", "", "", ""
		doc.Label, doc.PluralLabel, doc.DisplayGroup, doc.DisplayParent, doc.Color = "", "", "", "", ""
		doc.Annotations = compactAnnotations(doc.Annotations)
		for j := range doc.Fields {
			field := &doc.Fields[j]
			field.Summary, field.Meaning, field.Authoring, field.AgentImplications, field.Description = "", "", "", "", ""
			field.SectionDisplay = ""
			field.Enum, field.EnumValues = nil, nil // each referenced enum is defined once below
			field.Annotations = compactAnnotations(field.Annotations)
		}
		for j := range doc.Enums {
			enum := &doc.Enums[j]
			enum.Summary, enum.Meaning, enum.Authoring, enum.AgentImplications, enum.Description = "", "", "", "", ""
			for k := range enum.Values {
				value := &enum.Values[k]
				value.Summary, value.Meaning, value.Authoring, value.AgentImplications, value.Description = "", "", "", "", ""
				// Presentation and stage come from @view, which compaction drops.
				value.Label, value.Order, value.Tone, value.Collapsed = "", nil, "", nil
				value.Stage, value.StageDeclared = "", false
			}
		}
	}
	return CompactResponse{
		Format: "compact-authoring", SchemaHash: schema.Hash, Types: docs,
		Expand: []Expansion{
			{Operation: "ontology_authoring_guide", Input: map[string]string{"type": typeName}, Purpose: "authoring guidance and governing companion documents"},
			{Operation: "ontology_reference", Input: map[string]string{"type": typeName}, Purpose: "expanded descriptions and annotations; select a related type explicitly to inspect its contract"},
			{Operation: "ontology_query_schema", Input: map[string]string{"type": typeName}, Purpose: "executable query fields and root arguments, distinct from authored requirements"},
		},
	}, nil
}

func compactAnnotations(annotations map[string]map[string]any) map[string]map[string]any {
	out := make(map[string]map[string]any)
	for name, values := range annotations {
		switch name {
		case "display", "guidance", "view", "preview":
			continue
		}
		// Retain unknown and behavioral annotations. Compactness must not erase
		// schema constraints merely because a newer directive is unfamiliar.
		copyValues := make(map[string]any, len(values))
		for key, value := range values {
			if name == "contains" && key == "display" {
				continue
			}
			copyValues[key] = value
		}
		out[name] = copyValues
	}
	return out
}
