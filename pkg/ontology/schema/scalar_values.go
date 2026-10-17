package schema

import (
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type ScalarKind string

const (
	ScalarKindString   ScalarKind = "string"
	ScalarKindBoolean  ScalarKind = "bool"
	ScalarKindInt      ScalarKind = "int"
	ScalarKindReal     ScalarKind = "real"
	ScalarKindDate     ScalarKind = "date"
	ScalarKindDateTime ScalarKind = "datetime"
	ScalarKindURL      ScalarKind = "url"
	ScalarKindID       ScalarKind = "id"
)

// ScalarKindForType supplies the common scalar classification for reads and writes.
func ScalarKindForType(typeName string) ScalarKind {
	switch strings.ToLower(strings.TrimSpace(typeName)) {
	case "boolean", "bool":
		return ScalarKindBoolean
	case "int", "integer":
		return ScalarKindInt
	case "float", "number":
		return ScalarKindReal
	case "date":
		return ScalarKindDate
	case "datetime":
		return ScalarKindDateTime
	case "url":
		return ScalarKindURL
	case "id":
		return ScalarKindID
	default:
		return ScalarKindString
	}
}

type ScalarValue struct {
	Kind      ScalarKind
	Canonical string
	Bool      bool
	Int       int64
	Real      float64
}

// ParseScalar validates an authored scalar and supplies its indexed representation.
func ParseScalar(field *Field, raw string) (ScalarValue, bool) {
	if field == nil || field.Kind != FieldKindScalar {
		return ScalarValue{}, false
	}
	raw = strings.TrimSpace(raw)
	value := ScalarValue{Kind: ScalarKindForType(field.TypeName), Canonical: raw}
	var err error
	switch value.Kind {
	case ScalarKindBoolean:
		value.Bool, err = strconv.ParseBool(strings.ToLower(raw))
		value.Canonical = strconv.FormatBool(value.Bool)
	case ScalarKindInt:
		value.Int, err = strconv.ParseInt(raw, 10, 64)
		value.Canonical = strconv.FormatInt(value.Int, 10)
	case ScalarKindReal:
		value.Real, err = strconv.ParseFloat(raw, 64)
		if math.IsNaN(value.Real) || math.IsInf(value.Real, 0) {
			return ScalarValue{}, false
		}
		value.Canonical = strconv.FormatFloat(value.Real, 'g', -1, 64)
	case ScalarKindDate:
		parsed, ok := parseScalarTime(raw, "2006-01-02", "2006/01/02")
		if !ok {
			return ScalarValue{}, false
		}
		value.Canonical = parsed.Format("2006-01-02")
	case ScalarKindDateTime:
		// Authored timestamps without a zone use UTC, matching the date parser.
		parsed, ok := parseScalarTime(raw, time.RFC3339Nano,
			"2006-01-02 15:04", "2006-01-02 15:04:05",
			"2006/01/02 15:04", "2006/01/02 15:04:05",
			"2006-01-02T15:04", "2006-01-02T15:04:05")
		if !ok {
			return ScalarValue{}, false
		}
		// Fixed precision and UTC make lexical ordering equal instant ordering.
		value.Canonical = parsed.UTC().Format("2006-01-02T15:04:05.000000000Z")
	case ScalarKindURL:
		var parsed *url.URL
		parsed, err = url.ParseRequestURI(raw)
		if err == nil && ((!strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https")) || parsed.Host == "") {
			return ScalarValue{}, false
		}
	case ScalarKindID:
		return value, raw != ""
	}
	return value, err == nil
}

func parseScalarTime(raw string, layouts ...string) (time.Time, bool) {
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}
