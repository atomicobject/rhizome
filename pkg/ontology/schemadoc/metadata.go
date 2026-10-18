package schemadoc

import (
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"

	"strings"
)

// requiredWhen lists the @requiresWhen conditions that make fieldName
// required. A rule that also requires a specific value still makes the field
// required, so it is listed too.
func requiredWhen(rules []ontoschema.ConditionalRequirement, fieldName string) []FieldCondition {
	var out []FieldCondition
	for _, rule := range rules {
		for _, required := range rule.Require {
			if required.Field == fieldName {
				out = append(out, FieldCondition{Field: rule.Field, Equals: rule.Equals})
				break
			}
		}
	}
	return out
}

func guidanceSummary(guidance *ontoschema.Guidance, fallback string) string {
	if guidance != nil && strings.TrimSpace(guidance.Summary) != "" {
		return strings.TrimSpace(guidance.Summary)
	}
	return strings.TrimSpace(fallback)
}

func guidanceMeaning(guidance *ontoschema.Guidance) string {
	if guidance == nil {
		return ""
	}
	return strings.TrimSpace(guidance.Meaning)
}

func guidanceAuthoring(guidance *ontoschema.Guidance) string {
	if guidance == nil {
		return ""
	}
	return strings.TrimSpace(guidance.Authoring)
}

func guidanceAgentImplications(guidance *ontoschema.Guidance) string {
	if guidance == nil {
		return ""
	}
	return strings.TrimSpace(guidance.AgentImplications)
}

func clonePolicy(in *ontoschema.PolicyHint) *ontoschema.PolicyHint {
	if in == nil {
		return nil
	}
	cloned := *in
	return &cloned
}

func cloneAnnotations(in map[string]map[string]any) map[string]map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]map[string]any, len(in))
	for key, values := range in {
		if len(values) == 0 {
			out[key] = map[string]any{}
			continue
		}
		cloned := make(map[string]any, len(values))
		for valueKey, value := range values {
			cloned[valueKey] = value
		}
		out[key] = cloned
	}
	return out
}
