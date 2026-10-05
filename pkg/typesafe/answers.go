package typesafe

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

// Answer is a ChoiceAnswer, ScoreAnswer, or NoulAnswer. Use Response's typed
// accessors for named answers, or a type switch when iterating a mixed batch.
type Answer interface {
	answerType() string
}

type ChoiceAnswer struct {
	Choice        string             `json:"choice"`
	Probabilities map[string]float64 `json:"probabilities"`
	Confidence    float64            `json:"confidence"`
}

func (ChoiceAnswer) answerType() string { return "choice" }

func (a ChoiceAnswer) MarshalJSON() ([]byte, error) {
	type fields ChoiceAnswer
	return json.Marshal(struct {
		Type string `json:"type"`
		fields
	}{"choice", fields(a)})
}

type ScoreAnswer struct {
	Score         float64            `json:"score"`
	Probabilities map[string]float64 `json:"probabilities"`
	// Legend preserves structured level descriptions as native JSON values.
	Legend     map[string]any `json:"legend"`
	Confidence float64        `json:"confidence"`
}

func (ScoreAnswer) answerType() string { return "score" }

func (a ScoreAnswer) MarshalJSON() ([]byte, error) {
	type fields ScoreAnswer
	return json.Marshal(struct {
		Type string `json:"type"`
		fields
	}{"score", fields(a)})
}

type NoulAnswer struct {
	Noul float64 `json:"noul"`
}

func (NoulAnswer) answerType() string { return "noul" }

func (a NoulAnswer) MarshalJSON() ([]byte, error) {
	type fields NoulAnswer
	return json.Marshal(struct {
		Type string `json:"type"`
		fields
	}{"noul", fields(a)})
}

type Usage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

type Response struct {
	Model     string            `json:"model"`
	Answers   map[string]Answer `json:"answers"`
	Usage     Usage             `json:"usage"`
	RequestID string            `json:"-"`
}

// Choice retrieves a named choice, failing on a missing ID or a different type.
func (r Response) Choice(id string) (ChoiceAnswer, error) {
	return answerAs[ChoiceAnswer](r, id, "choice")
}

// Score retrieves a named score, failing on a missing ID or a different type.
func (r Response) Score(id string) (ScoreAnswer, error) {
	return answerAs[ScoreAnswer](r, id, "score")
}

// Noul retrieves a named probability, failing on a missing ID or a different type.
func (r Response) Noul(id string) (NoulAnswer, error) {
	return answerAs[NoulAnswer](r, id, "noul")
}

func answerAs[T Answer](r Response, id, kind string) (T, error) {
	a, ok := r.Answers[id].(T)
	if !ok {
		return a, fmt.Errorf("typesafe: answer %q is missing or is not %s", id, kind)
	}
	return a, nil
}

// ResponseError means the server returned JSON that violates the answer
// contract. It never includes the supplied state or raw response body.
type ResponseError struct {
	Problem string
}

func (e *ResponseError) Error() string { return "typesafe: invalid response: " + e.Problem }

// UnmarshalJSON decodes the answer union and rejects absent required values.
// In particular, a missing noul must never become a valid zero probability.
func (r *Response) UnmarshalJSON(data []byte) error {
	var wire struct {
		Model   string                     `json:"model"`
		Answers map[string]json.RawMessage `json:"answers"`
		Usage   *struct {
			Input  *int64 `json:"input_tokens"`
			Output *int64 `json:"output_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(data, &wire) != nil {
		return &ResponseError{"expected a JSON response object"}
	}
	if wire.Model == "" || wire.Answers == nil || wire.Usage == nil || wire.Usage.Input == nil || wire.Usage.Output == nil {
		return &ResponseError{"model, answers, and token usage are required"}
	}
	if *wire.Usage.Input < 0 || *wire.Usage.Output < 0 {
		return &ResponseError{"token usage cannot be negative"}
	}
	result := Response{Model: wire.Model, Usage: Usage{*wire.Usage.Input, *wire.Usage.Output}, Answers: make(map[string]Answer, len(wire.Answers))}
	for id, raw := range wire.Answers {
		answer, err := decodeAnswer(raw)
		if err != nil {
			return &ResponseError{fmt.Sprintf("answer %q: %s", id, err.Error())}
		}
		result.Answers[id] = answer
	}
	*r = result
	return nil
}

func decodeAnswer(raw json.RawMessage) (Answer, error) {
	var wire struct {
		Type          string              `json:"type"`
		Choice        *string             `json:"choice"`
		Score         *float64            `json:"score"`
		Noul          *float64            `json:"noul"`
		Confidence    *float64            `json:"confidence"`
		Probabilities map[string]*float64 `json:"probabilities"`
		Legend        map[string]any      `json:"legend"`
	}
	if json.Unmarshal(raw, &wire) != nil {
		return nil, fmt.Errorf("invalid answer fields")
	}
	if wire.Type == "noul" {
		if wire.Noul == nil || !probability(*wire.Noul) {
			return nil, fmt.Errorf("noul must be a number from 0 to 1")
		}
		return NoulAnswer{Noul: *wire.Noul}, nil
	}
	if wire.Type != "choice" && wire.Type != "score" {
		return nil, fmt.Errorf("unknown or missing answer type")
	}
	if wire.Confidence == nil || !probability(*wire.Confidence) || len(wire.Probabilities) == 0 {
		return nil, fmt.Errorf("confidence and probabilities are required")
	}
	probabilities := make(map[string]float64, len(wire.Probabilities))
	probabilitySum := 0.0
	for key, value := range wire.Probabilities {
		if value == nil || !probability(*value) {
			return nil, fmt.Errorf("probabilities must be numbers from 0 to 1")
		}
		probabilities[key] = *value
		probabilitySum += *value
	}
	// Permit small service-side rounding error while rejecting malformed
	// distributions that cannot represent mutually exclusive outcomes.
	if math.Abs(probabilitySum-1) > 0.01 {
		return nil, fmt.Errorf("probabilities must sum to 1")
	}
	if wire.Type == "choice" {
		if wire.Choice == nil {
			return nil, fmt.Errorf("choice is required")
		}
		if _, ok := probabilities[*wire.Choice]; !ok {
			return nil, fmt.Errorf("choice is absent from probabilities")
		}
		return ChoiceAnswer{*wire.Choice, probabilities, *wire.Confidence}, nil
	}
	if wire.Score == nil || math.IsNaN(*wire.Score) || math.IsInf(*wire.Score, 0) || wire.Legend == nil {
		return nil, fmt.Errorf("score and legend are required")
	}
	return ScoreAnswer{*wire.Score, probabilities, wire.Legend, *wire.Confidence}, nil
}

func probability(value float64) bool {
	return !math.IsNaN(value) && value >= 0 && value <= 1
}

func (r Response) validateQuestions(questions map[string]wireQuestion) error {
	if len(r.Answers) != len(questions) {
		return &ResponseError{"answer IDs do not match question IDs"}
	}
	for id, q := range questions {
		a, ok := r.Answers[id]
		if !ok || a.answerType() != q.Type {
			return &ResponseError{fmt.Sprintf("answer %q is missing or has the wrong type", id)}
		}
		var expected map[string]json.RawMessage
		var actual map[string]float64
		switch a := a.(type) {
		case ChoiceAnswer:
			_ = json.Unmarshal(q.Criteria, &expected)
			actual = a.Probabilities
		case ScoreAnswer:
			var levels []json.RawMessage
			_ = json.Unmarshal(q.Criteria, &levels)
			expected = make(map[string]json.RawMessage, len(levels))
			for i, level := range levels {
				expected[strconv.Itoa(i)] = level
			}
			if a.Score < 0 || a.Score > float64(len(levels)-1) || len(a.Legend) != len(levels) {
				return &ResponseError{fmt.Sprintf("answer %q does not match score levels", id)}
			}
			for key := range expected {
				if _, ok := a.Legend[key]; !ok {
					return &ResponseError{fmt.Sprintf("answer %q has an incomplete legend", id)}
				}
			}
			actual = a.Probabilities
		case NoulAnswer:
			continue
		}
		if len(expected) != len(actual) {
			return &ResponseError{fmt.Sprintf("answer %q has different options than its question", id)}
		}
		for key := range expected {
			if _, ok := actual[key]; !ok {
				return &ResponseError{fmt.Sprintf("answer %q is missing an option probability", id)}
			}
		}
	}
	return nil
}
