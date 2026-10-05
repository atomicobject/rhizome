package typesafe

import (
	"encoding/json"
	"fmt"
)

// UnmarshalJSON decodes the question discriminators into native question types
// and validates the request before replacing the receiver.
func (r *Request) UnmarshalJSON(data []byte) error {
	var wire struct {
		State     any                        `json:"state"`
		Model     string                     `json:"model"`
		Questions map[string]json.RawMessage `json:"questions"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	request := Request{State: wire.State, Model: wire.Model, Questions: make(map[string]Question, len(wire.Questions))}
	for id, data := range wire.Questions {
		var tag struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &tag); err != nil {
			return fmt.Errorf("typesafe: question %q: %w", id, err)
		}
		var question Question
		switch tag.Type {
		case "choice":
			question = &Choice{}
		case "score":
			question = &Score{}
		case "noul":
			question = &Noul{}
		default:
			return fmt.Errorf("typesafe: question %q: unknown type %q", id, tag.Type)
		}
		if err := json.Unmarshal(data, question); err != nil {
			return fmt.Errorf("typesafe: question %q: %w", id, err)
		}
		request.Questions[id] = question
	}
	if _, _, err := encodeRequest(request); err != nil {
		return err
	}
	*r = request
	return nil
}
