package noteformat_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/noteformat"
)

func TestMetadataValueCanonicalizesAndExportsDetachedYAMLLikeValues(t *testing.T) {
	input := map[string]any{
		"z": []any{true, "value"},
		"a": time.Date(2026, 8, 3, 12, 34, 56, 0, time.FixedZone("EDT", -4*60*60)),
	}
	value, err := noteformat.NewMetadataValue(input)
	if err != nil {
		t.Fatalf("NewMetadataValue: %v", err)
	}
	if got := string(value.CanonicalJSON()); got != `{"a":"2026-08-03T12:34:56-04:00","z":[true,"value"]}` {
		t.Fatalf("CanonicalJSON = %s", got)
	}
	exported := value.Export().(map[string]any)
	exported["z"].([]any)[1] = "changed"
	if got := string(value.CanonicalJSON()); got != `{"a":"2026-08-03T12:34:56-04:00","z":[true,"value"]}` {
		t.Fatalf("MetadataValue changed through Export: %s", got)
	}
}

func TestMetadataValueRejectsCyclesUnsupportedValuesAndNonStringKeys(t *testing.T) {
	cyclic := map[string]any{}
	cyclic["self"] = cyclic
	cyclicSlice := make([]any, 1)
	cyclicSlice[0] = cyclicSlice
	for name, input := range map[string]any{
		"cycle":          cyclic,
		"slice cycle":    cyclicSlice,
		"function":       func() {},
		"non-string key": map[int]string{1: "nope"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := noteformat.NewMetadataValue(input); err == nil {
				t.Fatal("NewMetadataValue succeeded")
			}
		})
	}
}

func TestMetadataValueAcceptsAcyclicSharedSlices(t *testing.T) {
	for _, length := range []int{0, 1} {
		input := []any{"leaf", nil}
		input[1] = input[:length]
		want, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		value, err := noteformat.NewMetadataValue(input)
		if err != nil {
			t.Fatalf("prefix length %d: %v", length, err)
		}
		if got := string(value.CanonicalJSON()); got != string(want) {
			t.Fatalf("prefix length %d: CanonicalJSON = %s, want %s", length, got, want)
		}
	}
}

func TestMetadataValuePreservesDateOnlyCalendarValuesSeparatelyFromTimestamps(t *testing.T) {
	dateOnly := time.Date(2026, 8, 3, 0, 0, 0, 0, time.FixedZone("UTC+14", 14*60*60))
	date, err := noteformat.NewMetadataValue(dateOnly)
	if err != nil {
		t.Fatalf("NewMetadataValue date: %v", err)
	}
	if date.Kind() != noteformat.MetadataDate {
		t.Fatalf("date Kind = %q, want %q", date.Kind(), noteformat.MetadataDate)
	}
	if got := string(date.CanonicalJSON()); got != `"2026-08-03"` {
		t.Fatalf("date CanonicalJSON = %s", got)
	}
	if got := date.Export(); got != "2026-08-03" {
		t.Fatalf("date Export = %#v", got)
	}

	timestamp, err := noteformat.NewMetadataValue(time.Date(2026, 8, 3, 0, 0, 1, 123456789, time.FixedZone("EDT", -4*60*60)))
	if err != nil {
		t.Fatalf("NewMetadataValue timestamp: %v", err)
	}
	if timestamp.Kind() != noteformat.MetadataTimestamp {
		t.Fatalf("timestamp Kind = %q, want %q", timestamp.Kind(), noteformat.MetadataTimestamp)
	}
	if got := string(timestamp.CanonicalJSON()); got != `"2026-08-03T00:00:01.123456789-04:00"` {
		t.Fatalf("timestamp CanonicalJSON = %s", got)
	}

	container, err := noteformat.NewMetadataValue(map[string]any{"date": dateOnly})
	if err != nil {
		t.Fatalf("NewMetadataValue container: %v", err)
	}
	exported := container.Export().(map[string]any)
	exported["date"] = "changed"
	if got := string(container.CanonicalJSON()); got != `{"date":"2026-08-03"}` {
		t.Fatalf("date container changed through Export: %s", got)
	}
}

func TestMetadataValuePreservesIntegerFloatAndJSONNumberSemantics(t *testing.T) {
	tests := []struct {
		name      string
		input     any
		kind      noteformat.MetadataValueKind
		canonical string
	}{
		{"signed integer", int64(-42), noteformat.MetadataInteger, "-42"},
		{"unsigned integer", uint64(42), noteformat.MetadataInteger, "42"},
		{"whole float remains float", 1.0, noteformat.MetadataFloat, "1"},
		{"float uses legacy decimal", 1e20, noteformat.MetadataFloat, "100000000000000000000"},
		{"high precision JSON integer", json.Number("1234567890123456789012345678901234567890"), noteformat.MetadataInteger, "1234567890123456789012345678901234567890"},
		{"exponent JSON float", json.Number("1.234567890123456789e+123"), noteformat.MetadataFloat, "1.234567890123456789e+123"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value, err := noteformat.NewMetadataValue(test.input)
			if err != nil {
				t.Fatalf("NewMetadataValue: %v", err)
			}
			if value.Kind() != test.kind {
				t.Fatalf("Kind = %q, want %q", value.Kind(), test.kind)
			}
			if got := string(value.CanonicalJSON()); got != test.canonical {
				t.Fatalf("CanonicalJSON = %q, want %q", got, test.canonical)
			}
			if got := value.Export(); got != json.Number(test.canonical) {
				t.Fatalf("Export = %#v, want %q", got, test.canonical)
			}
		})
	}
	if _, err := noteformat.NewMetadataValue(json.Number("01")); err == nil {
		t.Fatal("NewMetadataValue accepted invalid JSON number")
	}
}

func TestMetadataValueNestedEmptyKeySurvivesCanonicalRoundTrip(t *testing.T) {
	value, err := noteformat.NewMetadataValue(map[string]any{"": []any{map[string]any{"": "kept"}}})
	if err != nil {
		t.Fatal(err)
	}
	copied, err := noteformat.NewMetadataValue(value)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(copied.CanonicalJSON()); got != `{"":[{"":"kept"}]}` {
		t.Fatalf("round trip: %s", got)
	}
}

func TestEmptyRootMetadataNameRemainsInvalid(t *testing.T) {
	value, err := noteformat.NewMetadataValue(map[string]any{"": "nested key is valid"})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"", "  "} {
		_, err := noteformat.NewProjectionWithFacts("provider-v1", "projection-v1", noteformat.ProjectionStatusCurrent, nil,
			noteformat.MustCapabilities(noteformat.CapabilityRootMetadataReading),
			noteformat.ProjectionFacts{RootMetadata: []noteformat.RootMetadataFact{{Key: key, Value: value}}})
		if err == nil {
			t.Fatalf("accepted empty root metadata name %q", key)
		}
	}
}
