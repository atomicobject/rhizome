package schemadoc

import (
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"sort"
)

func enumValues(schema *ontoschema.Schema, typeName string) []string {
	enumType := ontoschema.EnumTypeForName(schema, typeName)
	if enumType == nil {
		return nil
	}
	out := make([]string, 0, len(enumType.Values))
	for _, value := range enumType.Values {
		if value == nil {
			continue
		}
		out = append(out, value.Name)
	}
	return out
}

func enumDoc(schema *ontoschema.Schema, typeName string) *EnumDoc {
	enumType := ontoschema.EnumTypeForName(schema, typeName)
	if enumType == nil {
		return nil
	}
	doc := &EnumDoc{
		Name:              enumType.Name,
		Summary:           guidanceSummary(enumType.Guidance, enumType.Description),
		Meaning:           guidanceMeaning(enumType.Guidance),
		Authoring:         guidanceAuthoring(enumType.Guidance),
		AgentImplications: guidanceAgentImplications(enumType.Guidance),
		Description:       enumType.Description,
		Values:            make([]EnumValueDoc, 0, len(enumType.Values)),
	}
	stages := enumType.Stages()
	for index, value := range enumType.Values {
		if value == nil {
			continue
		}
		valueDoc := EnumValueDoc{
			Name:              value.Name,
			Summary:           guidanceSummary(value.Guidance, value.Description),
			Meaning:           guidanceMeaning(value.Guidance),
			Authoring:         guidanceAuthoring(value.Guidance),
			AgentImplications: guidanceAgentImplications(value.Guidance),
			Description:       value.Description,
			Policy:            clonePolicy(value.Policy),
			Label:             value.View.Label,
			Order:             value.View.AuthoredOrder(),
			Tone:              value.View.Tone,
			Collapsed:         cloneBool(value.View.Collapsed),
		}
		if valueDoc.Tone == "" {
			valueDoc.Tone = value.View.Stage.DefaultTone()
		}
		if valueDoc.Collapsed == nil && value.View.Stage == ontoschema.StageDropped {
			collapsed := true
			valueDoc.Collapsed = &collapsed
		}
		if stages != nil {
			valueDoc.Stage, valueDoc.StageDeclared = stages[index].Stage, stages[index].Declared
		}
		doc.Values = append(doc.Values, valueDoc)
	}
	return doc
}

func cloneBool(in *bool) *bool {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func fieldEnumDocs(schema *ontoschema.Schema, fields []*ontoschema.Field) []EnumDoc {
	if schema == nil {
		return nil
	}
	seen := map[string]struct{}{}
	var out []EnumDoc
	for _, field := range fields {
		if field == nil {
			continue
		}
		if _, ok := seen[field.TypeName]; ok {
			continue
		}
		doc := enumDoc(schema, field.TypeName)
		if doc == nil {
			continue
		}
		seen[field.TypeName] = struct{}{}
		out = append(out, *doc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
