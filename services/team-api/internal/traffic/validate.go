package traffic

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
)

// gwError is a gateway-originated error before it is written.
type gwError struct {
	code  wire.ErrorCode
	msg   string
	retry time.Duration // Retry-After, when set
}

func fail(code wire.ErrorCode, msg string) *gwError { return &gwError{code: code, msg: msg} }

// validKey reports whether an idempotency key is 8 to 128 of [A-Za-z0-9._-].
func validKey(key string) bool {
	if len(key) < 8 || len(key) > 128 {
		return false
	}
	for _, c := range []byte(key) {
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-'
		if !ok {
			return false
		}
	}
	return true
}

// readBody reads at most limit bytes of an identity-encoded body.
func readBody(r *http.Request, limit int64) ([]byte, *gwError) {
	if enc := r.Header.Get("Content-Encoding"); enc != "" && !strings.EqualFold(enc, "identity") {
		return nil, fail(wire.CodeBadRequest, "request content encoding is not supported")
	}
	if r.ContentLength > limit {
		return nil, fail(wire.CodeTooLarge, "request body is too large")
	}
	var body []byte
	var err error
	if r.ContentLength >= 0 {
		body = make([]byte, r.ContentLength)
		_, err = io.ReadFull(r.Body, body)
	} else {
		body, err = io.ReadAll(io.LimitReader(r.Body, limit+1))
		if int64(len(body)) > limit {
			return nil, fail(wire.CodeTooLarge, "request body is too large")
		}
	}
	if err != nil {
		return nil, fail(wire.CodeBadRequest, "request body could not be read")
	}
	return body, nil
}

// object walks one top-level JSON object with exact, unique keys and no
// trailing data, calling field for each key with the decoder positioned at
// its value.
func object(body []byte, field func(key string, dec *json.Decoder) *gwError) *gwError {
	bad := fail(wire.CodeBadRequest, "request body must be one JSON object")
	dec := json.NewDecoder(bytes.NewReader(body))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return bad
	}
	seen := map[string]bool{}
	for dec.More() {
		tok, err := dec.Token()
		key, ok := tok.(string)
		if err != nil || !ok {
			return bad
		}
		if seen[key] {
			return fail(wire.CodeBadRequest, "duplicate field "+quote(key))
		}
		seen[key] = true
		if e := field(key, dec); e != nil {
			return e
		}
	}
	if tok, err := dec.Token(); err != nil || tok != json.Delim('}') {
		return bad
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return bad
	}
	return nil
}

// quote bounds a caller-supplied field name for an error message.
func quote(key string) string {
	if len(key) > 64 {
		key = key[:64]
	}
	b, _ := json.Marshal(key)
	return string(b)
}

func decodeField(dec *json.Decoder, key string, v any) *gwError {
	if err := dec.Decode(v); err != nil {
		return fail(wire.CodeBadRequest, "invalid "+key)
	}
	return nil
}

// validateVoyage returns the model and input count of an embeddings request.
func validateVoyage(body []byte, models map[string]Charge) (string, int, *gwError) {
	var model *string
	var inputs inputCount
	e := object(body, func(key string, dec *json.Decoder) *gwError {
		switch key {
		case "model":
			return decodeField(dec, key, &model)
		case "input":
			return decodeField(dec, key, &inputs)
		case "input_type":
			var v *string
			if e := decodeField(dec, key, &v); e != nil {
				return e
			}
			if v != nil && *v != "query" && *v != "document" {
				return fail(wire.CodeBadRequest, "input_type must be query, document, or null")
			}
		case "output_dimension":
			var v *int
			if e := decodeField(dec, key, &v); e != nil {
				return e
			}
			if v != nil && (*v < 1 || *v > wire.MaxVoyageOutputDimension) {
				return fail(wire.CodeBadRequest, "output_dimension is out of range")
			}
		case "truncation":
			var v *bool
			return decodeField(dec, key, &v)
		case "output_dtype":
			var v *string
			if e := decodeField(dec, key, &v); e != nil {
				return e
			}
			if v != nil && *v != "float" {
				return fail(wire.CodeBadRequest, "output_dtype must be float")
			}
		default:
			return fail(wire.CodeBadRequest, "unsupported field "+quote(key))
		}
		return nil
	})
	if e != nil {
		return "", 0, e
	}
	if model == nil {
		return "", 0, fail(wire.CodeBadRequest, "model is required")
	}
	if _, ok := models[*model]; !ok {
		return "", 0, fail(wire.CodeModelNotAllowed, "model is not allowed")
	}
	if inputs < 1 || inputs > wire.MaxVoyageInputs {
		return "", 0, fail(wire.CodeBadRequest, "input must be a string or 1 to 1000 strings")
	}
	return *model, int(inputs), nil
}

// inputCount decodes a string or an array of strings into its length without
// copying the strings.
type inputCount int

func (n *inputCount) UnmarshalJSON(data []byte) error {
	bad := errors.New("input must be a string or an array of strings")
	data = bytes.TrimSpace(data)
	if len(data) > 0 && data[0] == '"' {
		*n = 1
		return nil
	}
	if len(data) < 2 || data[0] != '[' {
		return bad
	}
	// The decoder already checked the syntax; only the element kinds and
	// count remain.
	count, i := 0, 1
	for {
		i = skipSpace(data, i)
		if data[i] == ']' && count == 0 {
			break
		}
		if data[i] != '"' {
			return bad
		}
		for i++; data[i] != '"'; i++ {
			if data[i] == '\\' {
				i++
			}
		}
		count++
		i = skipSpace(data, i+1)
		if data[i] == ']' {
			break
		}
		i++ // the comma
	}
	*n = inputCount(count)
	return nil
}

func skipSpace(data []byte, i int) int {
	for data[i] == ' ' || data[i] == '\t' || data[i] == '\n' || data[i] == '\r' {
		i++
	}
	return i
}

// validateTypeSafe returns the model (or the default) and question count of a
// System One request. Fields inside the documented top-level keys pass as is.
func validateTypeSafe(body []byte, models map[string]Charge) (string, int, *gwError) {
	model := defaultTypeSafeModel
	var questions map[string]json.RawMessage
	e := object(body, func(key string, dec *json.Decoder) *gwError {
		switch key {
		case "model":
			var v *string
			if e := decodeField(dec, key, &v); e != nil {
				return e
			}
			if v != nil && *v != "" {
				model = *v
			}
		case "state":
			var v json.RawMessage
			return decodeField(dec, key, &v)
		case "questions":
			return decodeField(dec, key, &questions)
		default:
			return fail(wire.CodeBadRequest, "unsupported field "+quote(key))
		}
		return nil
	})
	if e != nil {
		return "", 0, e
	}
	if _, ok := models[model]; !ok {
		return "", 0, fail(wire.CodeModelNotAllowed, "model is not allowed")
	}
	if len(questions) == 0 {
		return "", 0, fail(wire.CodeBadRequest, "questions are required")
	}
	return model, len(questions), nil
}
