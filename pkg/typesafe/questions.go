// Package typesafe provides a typed client for TypeSafe's System One API (Jev).
// It has no dependency on Rhizome configuration, credentials, or runtime state.
package typesafe

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Question is a Choice, Score, or Noul. Each concrete type supplies its wire
// discriminator, so callers cannot accidentally send a mismatched type.
type Question interface {
	questionType() string
}

// Choice selects one named option. Descriptions and Instructions accept text,
// JSON objects, arrays, or nil. Nested values may be any JSON value.
type Choice struct {
	Instructions any            `json:"instructions,omitempty"`
	Criteria     map[string]any `json:"criteria"`
}

func (Choice) questionType() string { return "choice" }

func (q Choice) MarshalJSON() ([]byte, error) {
	type fields Choice
	return json.Marshal(struct {
		Type string `json:"type"`
		fields
	}{"choice", fields(q)})
}

// Score evaluates an ordered rubric, numbered from zero. Criteria must contain
// at least two levels. Instructions and levels accept the same shapes as Choice.
type Score struct {
	Instructions any   `json:"instructions,omitempty"`
	Criteria     []any `json:"criteria"`
}

func (Score) questionType() string { return "score" }

func (q Score) MarshalJSON() ([]byte, error) {
	type fields Score
	return json.Marshal(struct {
		Type string `json:"type"`
		fields
	}{"score", fields(q)})
}

// Noul evaluates a yes/no question and returns the probability of yes.
type Noul struct {
	Instructions any           `json:"instructions,omitempty"`
	Criteria     *NoulCriteria `json:"criteria,omitempty"`
}

// NoulCriteria optionally describes either outcome, using text, objects,
// arrays, or nil.
type NoulCriteria struct {
	True  any `json:"true,omitempty"`
	False any `json:"false,omitempty"`
}

func (Noul) questionType() string { return "noul" }

func (q Noul) MarshalJSON() ([]byte, error) {
	type fields Noul
	return json.Marshal(struct {
		Type string `json:"type"`
		fields
	}{"noul", fields(q)})
}

// Request evaluates one state against independent named questions. State is
// text, a JSON object, or an array, never nil. An empty Model uses the client's
// default. Question IDs identify answers but are not instructions to the model.
type Request struct {
	State     any                 `json:"state"`
	Model     string              `json:"model"`
	Questions map[string]Question `json:"questions"`
}

// wireQuestion also captures the submitted contract for response validation.
// Validating the serialized snapshot handles typed nils and custom JSON values
// without modifying or rereading caller-owned maps during retries.
type wireQuestion struct {
	Type         string          `json:"type"`
	Instructions json.RawMessage `json:"instructions"`
	Criteria     json.RawMessage `json:"criteria"`
}

func encodeRequest(request Request) ([]byte, map[string]wireQuestion, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return nil, nil, fmt.Errorf("typesafe: request is not JSON encodable (%T)", err)
	}
	var wire struct {
		State     json.RawMessage         `json:"state"`
		Questions map[string]wireQuestion `json:"questions"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, nil, fmt.Errorf("typesafe: invalid request shape")
	}
	if !entry(wire.State, false) {
		return nil, nil, fmt.Errorf("typesafe: state must be a string, object, or array")
	}
	if len(wire.Questions) == 0 {
		return nil, nil, fmt.Errorf("typesafe: at least one question is required")
	}
	for id, q := range wire.Questions {
		if err := q.validate(); err != nil {
			return nil, nil, fmt.Errorf("typesafe: question %q: %w", id, err)
		}
	}
	return body, wire.Questions, nil
}

func (q wireQuestion) validate() error {
	if !entry(q.Instructions, true) {
		return fmt.Errorf("instructions must be a string, object, array, or null")
	}
	var descriptions []json.RawMessage
	switch q.Type {
	case "choice":
		var criteria map[string]json.RawMessage
		if json.Unmarshal(q.Criteria, &criteria) != nil || len(criteria) == 0 || len(criteria) > 255 {
			return fmt.Errorf("choice criteria must have 1 to 255 options")
		}
		for _, value := range criteria {
			descriptions = append(descriptions, value)
		}
	case "score":
		if json.Unmarshal(q.Criteria, &descriptions) != nil || len(descriptions) < 2 {
			return fmt.Errorf("score criteria must have at least two levels")
		}
	case "noul":
		if len(q.Criteria) == 0 || bytes.Equal(q.Criteria, []byte("null")) {
			return nil
		}
		var criteria map[string]json.RawMessage
		if json.Unmarshal(q.Criteria, &criteria) != nil {
			return fmt.Errorf("noul criteria must be an object")
		}
		for _, value := range criteria {
			descriptions = append(descriptions, value)
		}
	default:
		return fmt.Errorf("question must be a non-nil Choice, Score, or Noul")
	}
	for _, value := range descriptions {
		if !entry(value, true) {
			return fmt.Errorf("criteria descriptions must be strings, objects, arrays, or null")
		}
	}
	return nil
}

func entry(raw json.RawMessage, allowNull bool) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return allowNull
	}
	return raw[0] == '"' || raw[0] == '{' || raw[0] == '['
}
