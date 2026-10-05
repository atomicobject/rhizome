package ontology

import (
	"encoding/json"
	"sort"
	"strings"
)

// TraversalOverride captures @traversal arguments for a specific set of
// intents. An empty Intents slice means the override applies to every intent.
type TraversalOverride struct {
	Intents           []string `json:"intents,omitempty"`
	IncludeAmbient    *bool    `json:"includeAmbient,omitempty"`
	MinStructuralHits *int     `json:"minStructuralHits,omitempty"`
	MaxDepth          *int     `json:"maxDepth,omitempty"`
}

// TypePolicy is the compiled per-type traversal contract extracted from
// @traversal directives on ontology note types.
type TypePolicy struct {
	TypeName  string              `json:"typeName"`
	Overrides []TraversalOverride `json:"overrides,omitempty"`
}

func (p TypePolicy) Empty() bool {
	if len(p.Overrides) == 0 {
		return true
	}
	for _, ov := range p.Overrides {
		if ov.IncludeAmbient != nil || ov.MinStructuralHits != nil || ov.MaxDepth != nil {
			return false
		}
	}
	return true
}

func (p TypePolicy) OverrideForIntent(intent string) (TraversalOverride, bool) {
	intent = strings.TrimSpace(intent)
	var fallback TraversalOverride
	haveFallback := false
	for _, ov := range p.Overrides {
		if len(ov.Intents) == 0 {
			if !haveFallback {
				fallback = ov
				haveFallback = true
			}
			continue
		}
		if intent == "" {
			continue
		}
		for _, want := range ov.Intents {
			if strings.EqualFold(strings.TrimSpace(want), intent) {
				return ov, true
			}
		}
	}
	if haveFallback {
		return fallback, true
	}
	return TraversalOverride{}, false
}

func CompileTypePolicies(schema *Schema) []TypePolicy {
	if schema == nil {
		return nil
	}
	out := make([]TypePolicy, 0, len(schema.Types))
	for name, noteType := range schema.Types {
		if noteType == nil || effectiveTypeRole(noteType) != TypeRoleNote {
			continue
		}
		override, ok := extractTraversalOverride(noteType.Annotations["traversal"])
		if !ok {
			continue
		}
		out = append(out, TypePolicy{
			TypeName:  name,
			Overrides: []TraversalOverride{override},
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TypeName < out[j].TypeName })
	return out
}

func MarshalTypePolicy(policy TypePolicy) string {
	if policy.Empty() {
		return ""
	}
	buf, err := json.Marshal(policy)
	if err != nil {
		return ""
	}
	return string(buf)
}

func UnmarshalTypePolicy(raw string) (TypePolicy, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return TypePolicy{}, nil
	}
	var policy TypePolicy
	if err := json.Unmarshal([]byte(raw), &policy); err != nil {
		return TypePolicy{}, err
	}
	return policy, nil
}

func extractTraversalOverride(args map[string]any) (TraversalOverride, bool) {
	if len(args) == 0 {
		return TraversalOverride{}, false
	}
	override := TraversalOverride{}
	touched := false

	if intents, ok := args["intents"].([]any); ok {
		for _, item := range intents {
			s, ok := item.(string)
			if !ok {
				continue
			}
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			override.Intents = append(override.Intents, s)
		}
	}
	if v, ok := args["includeAmbient"].(bool); ok {
		b := v
		override.IncludeAmbient = &b
		touched = true
	}
	if v, ok := coerceInt(args["minStructuralHits"]); ok {
		n := v
		override.MinStructuralHits = &n
		touched = true
	}
	if v, ok := coerceInt(args["maxDepth"]); ok {
		n := v
		override.MaxDepth = &n
		touched = true
	}
	if !touched {
		return TraversalOverride{}, false
	}
	return override, true
}

func coerceInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}
