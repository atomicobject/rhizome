package agentapi

func evaluateContract() *CodeOperationContract {
	entry := codeSchema{"oneOf": []any{str(), unknownObject(), codeSchema{"type": "array", "items": unknownValue()}, codeSchema{"type": "null"}}}
	state := codeSchema{"oneOf": []any{str(), unknownObject(), codeSchema{"type": "array", "items": unknownValue()}}}
	dictionary := func(value codeSchema) codeSchema { return codeSchema{"type": "object", "additionalProperties": value} }
	question := func(kind string, criteria codeSchema, required ...string) codeSchema {
		return object(append([]string{"type"}, required...), map[string]codeSchema{"type": enumeration(kind), "instructions": entry, "criteria": criteria})
	}
	questions := dictionary(codeSchema{"oneOf": []any{
		question("choice", dictionary(entry), "criteria"),
		question("score", codeSchema{"type": "array", "items": entry}, "criteria"),
		question("noul", codeSchema{"oneOf": []any{object(nil, map[string]codeSchema{"true": entry, "false": entry}), codeSchema{"type": "null"}}}),
	}})
	number := codeSchema{"type": "number"}
	probabilities := dictionary(number)
	answers := dictionary(codeSchema{"oneOf": []any{
		object([]string{"type", "choice", "probabilities", "confidence"}, map[string]codeSchema{"type": enumeration("choice"), "choice": str(), "probabilities": probabilities, "confidence": number}),
		object([]string{"type", "score", "probabilities", "legend", "confidence"}, map[string]codeSchema{"type": enumeration("score"), "score": number, "probabilities": probabilities, "legend": dictionary(entry), "confidence": number}),
		object([]string{"type", "noul"}, map[string]codeSchema{"type": enumeration("noul"), "noul": number}),
	}})
	return contract("Classify, score, or evaluate supplied content with TypeSafe's Jev model.",
		input(map[string]codeSchema{"state": state, "model": str(), "questions": questions}, "state", "questions"),
		object([]string{"model", "answers", "usage"}, map[string]codeSchema{
			"model": str(), "answers": answers, "requestId": str(),
			"usage": object([]string{"input_tokens", "output_tokens"}, map[string]codeSchema{"input_tokens": integer(), "output_tokens": integer()}),
		}),
		map[string]any{"state": map[string]any{"title": "Authentication spec", "content": "The spec describes the retired password flow."}, "questions": map[string]any{
			"action": map[string]any{"type": "choice", "instructions": "Choose how to maintain this spec.", "criteria": map[string]any{"keep": "Accurate and useful", "update": "Needs correction", "consolidate": "Duplicates another supplied spec"}},
			"stale":  map[string]any{"type": "noul", "instructions": "Does this spec describe obsolete behavior?"},
		}}, "sends supplied state and questions to TypeSafe and incurs API usage", "uses TYPESAFE_API_KEY from environment/config, then the unlocked Atomic key bundle", "does not mutate vault content")
}
