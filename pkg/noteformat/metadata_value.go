package noteformat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// MetadataValueKind is the closed value domain accepted from YAML/JSON-like
// root metadata. It excludes arbitrary Go objects and mutable references.
type MetadataValueKind string

const (
	MetadataNull      MetadataValueKind = "null"
	MetadataBoolean   MetadataValueKind = "boolean"
	MetadataInteger   MetadataValueKind = "integer"
	MetadataFloat     MetadataValueKind = "float"
	MetadataString    MetadataValueKind = "string"
	MetadataDate      MetadataValueKind = "date"
	MetadataTimestamp MetadataValueKind = "timestamp"
	MetadataArray     MetadataValueKind = "array"
	MetadataObject    MetadataValueKind = "object"
)

// MetadataObjectEntry is one deterministic string-key object member.
type MetadataObjectEntry struct {
	Key   string
	Value MetadataValue
}

// MetadataValue is an immutable canonical metadata value. Constructors and
// accessors copy collections, so callers cannot mutate projection state.
type MetadataValue struct {
	kind      MetadataValueKind
	boolean   bool
	number    string
	stringVal string
	array     []MetadataValue
	object    []MetadataObjectEntry
}

// NewMetadataValue canonicalizes supported YAML/JSON-like values. It accepts
// null, booleans, numeric primitives, strings, time.Time, arrays, and objects
// with string keys. Cycles and unsupported values return errors without panic.
func NewMetadataValue(value any) (MetadataValue, error) {
	return metadataValueFromReflect(reflect.ValueOf(value), make(map[metadataVisit]bool))
}

// NewDateMetadataValue creates a date-only metadata value from an authored
// calendar value. Callers that know a temporal scalar's source syntax should
// use this instead of asking NewMetadataValue to infer date-only intent from a
// midnight time.Time.
func NewDateMetadataValue(at time.Time) MetadataValue {
	return MetadataValue{kind: MetadataDate, stringVal: at.Format(time.DateOnly)}
}

// NewTimestampMetadataValue creates a timestamp metadata value from an
// authored timestamp. It preserves the timestamp's offset in RFC3339Nano
// form, including when the time of day is midnight.
func NewTimestampMetadataValue(at time.Time) MetadataValue {
	return MetadataValue{kind: MetadataTimestamp, stringVal: at.Format(time.RFC3339Nano)}
}

// Kind returns the stable kind of v.
func (v MetadataValue) Kind() MetadataValueKind {
	return v.kind
}

// Export returns a detached YAML/JSON-like value. Date-only values preserve
// their authored calendar date; timestamps preserve their authored offset in
// RFC3339Nano form.
func (v MetadataValue) Export() any {
	switch v.kind {
	case MetadataNull:
		return nil
	case MetadataBoolean:
		return v.boolean
	case MetadataInteger, MetadataFloat:
		return json.Number(v.number)
	case MetadataString, MetadataDate, MetadataTimestamp:
		return v.stringVal
	case MetadataArray:
		out := make([]any, len(v.array))
		for index := range v.array {
			out[index] = v.array[index].Export()
		}
		return out
	case MetadataObject:
		out := make(map[string]any, len(v.object))
		for _, entry := range v.object {
			out[entry.Key] = entry.Value.Export()
		}
		return out
	default:
		return nil
	}
}

// Array returns a detached ordered copy when v is an array.
func (v MetadataValue) Array() []MetadataValue {
	return append([]MetadataValue(nil), v.array...)
}

// Object returns a detached deterministic key-sorted copy when v is an object.
func (v MetadataValue) Object() []MetadataObjectEntry {
	out := make([]MetadataObjectEntry, len(v.object))
	for index, entry := range v.object {
		out[index] = MetadataObjectEntry{Key: entry.Key, Value: entry.Value.clone()}
	}
	return out
}

// CanonicalJSON returns a deterministic JSON representation of v. It returns
// a copied byte slice so it is also safe for persistence/fingerprints.
func (v MetadataValue) CanonicalJSON() []byte {
	encoded, err := v.MarshalJSON()
	if err != nil {
		return nil
	}
	return append([]byte(nil), encoded...)
}

// MarshalJSON emits v without exposing mutable internal collections.
func (v MetadataValue) MarshalJSON() ([]byte, error) {
	switch v.kind {
	case MetadataNull:
		return []byte("null"), nil
	case MetadataBoolean:
		return []byte(strconv.FormatBool(v.boolean)), nil
	case MetadataInteger, MetadataFloat:
		return []byte(v.number), nil
	case MetadataString, MetadataDate, MetadataTimestamp:
		return json.Marshal(v.stringVal)
	case MetadataArray:
		return json.Marshal(v.array)
	case MetadataObject:
		var buffer bytes.Buffer
		buffer.WriteByte('{')
		for index, entry := range v.object {
			if index > 0 {
				buffer.WriteByte(',')
			}
			key, err := json.Marshal(entry.Key)
			if err != nil {
				return nil, err
			}
			value, err := entry.Value.MarshalJSON()
			if err != nil {
				return nil, err
			}
			buffer.Write(key)
			buffer.WriteByte(':')
			buffer.Write(value)
		}
		buffer.WriteByte('}')
		return buffer.Bytes(), nil
	default:
		return nil, fmt.Errorf("unknown metadata kind %q", v.kind)
	}
}

func (v MetadataValue) clone() MetadataValue {
	out := v
	out.array = make([]MetadataValue, len(v.array))
	for index := range v.array {
		out.array[index] = v.array[index].clone()
	}
	out.object = make([]MetadataObjectEntry, len(v.object))
	for index, entry := range v.object {
		out.object[index] = MetadataObjectEntry{Key: entry.Key, Value: entry.Value.clone()}
	}
	return out
}

func (v MetadataValue) valid() bool {
	switch v.kind {
	case MetadataNull, MetadataBoolean, MetadataString, MetadataTimestamp:
		return true
	case MetadataDate:
		parsed, err := time.Parse(time.DateOnly, v.stringVal)
		return err == nil && parsed.Format(time.DateOnly) == v.stringVal
	case MetadataInteger, MetadataFloat:
		return validateJSONNumber(v.number) == nil
	case MetadataArray:
		for _, item := range v.array {
			if !item.valid() {
				return false
			}
		}
		return true
	case MetadataObject:
		for index, entry := range v.object {
			if !entry.Value.valid() || (index > 0 && v.object[index-1].Key >= entry.Key) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

type metadataVisit struct {
	type_ reflect.Type
	kind  reflect.Kind
	ptr   uintptr
	len   int
}

func metadataValueFromReflect(value reflect.Value, visiting map[metadataVisit]bool) (MetadataValue, error) {
	if !value.IsValid() {
		return MetadataValue{kind: MetadataNull}, nil
	}
	if value.Type() == reflect.TypeFor[MetadataValue]() {
		metadata := value.Interface().(MetadataValue)
		if !metadata.valid() {
			return MetadataValue{}, fmt.Errorf("metadata value is invalid")
		}
		return metadata.clone(), nil
	}
	if value.Type() == reflect.TypeFor[time.Time]() {
		at := value.Interface().(time.Time)
		if at.Hour() == 0 && at.Minute() == 0 && at.Second() == 0 && at.Nanosecond() == 0 {
			return NewDateMetadataValue(at), nil
		}
		return NewTimestampMetadataValue(at), nil
	}
	if value.Type() == reflect.TypeFor[json.Number]() {
		number := value.Interface().(json.Number).String()
		kind, err := classifyJSONNumber(number)
		if err != nil {
			return MetadataValue{}, err
		}
		return MetadataValue{kind: kind, number: number}, nil
	}
	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			return MetadataValue{kind: MetadataNull}, nil
		}
		return metadataValueFromReflect(value.Elem(), visiting)
	case reflect.Bool:
		return MetadataValue{kind: MetadataBoolean, boolean: value.Bool()}, nil
	case reflect.String:
		return MetadataValue{kind: MetadataString, stringVal: value.String()}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return MetadataValue{kind: MetadataInteger, number: strconv.FormatInt(value.Int(), 10)}, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return MetadataValue{kind: MetadataInteger, number: strconv.FormatUint(value.Uint(), 10)}, nil
	case reflect.Float32, reflect.Float64:
		number := value.Float()
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return MetadataValue{}, fmt.Errorf("metadata number must be finite")
		}
		return MetadataValue{kind: MetadataFloat, number: strconv.FormatFloat(number, 'f', -1, value.Type().Bits())}, nil
	case reflect.Slice, reflect.Array:
		if value.Kind() == reflect.Slice && value.IsNil() {
			return MetadataValue{kind: MetadataNull}, nil
		}
		leave, err := enterMetadataVisit(value, visiting)
		if err != nil {
			return MetadataValue{}, err
		}
		defer leave()
		out := MetadataValue{kind: MetadataArray, array: make([]MetadataValue, value.Len())}
		for index := 0; index < value.Len(); index++ {
			item, err := metadataValueFromReflect(value.Index(index), visiting)
			if err != nil {
				return MetadataValue{}, err
			}
			out.array[index] = item
		}
		return out, nil
	case reflect.Map:
		if value.IsNil() {
			return MetadataValue{kind: MetadataNull}, nil
		}
		leave, err := enterMetadataVisit(value, visiting)
		if err != nil {
			return MetadataValue{}, err
		}
		defer leave()
		out := MetadataValue{kind: MetadataObject, object: make([]MetadataObjectEntry, 0, value.Len())}
		iterator := value.MapRange()
		for iterator.Next() {
			key := iterator.Key()
			if key.Kind() == reflect.Interface && !key.IsNil() {
				key = key.Elem()
			}
			if key.Kind() != reflect.String {
				return MetadataValue{}, fmt.Errorf("metadata object keys must be strings")
			}
			item, err := metadataValueFromReflect(iterator.Value(), visiting)
			if err != nil {
				return MetadataValue{}, err
			}
			out.object = append(out.object, MetadataObjectEntry{Key: key.String(), Value: item})
		}
		sort.Slice(out.object, func(i, j int) bool { return out.object[i].Key < out.object[j].Key })
		for index := 1; index < len(out.object); index++ {
			if out.object[index-1].Key == out.object[index].Key {
				return MetadataValue{}, fmt.Errorf("metadata object has duplicate key %q", out.object[index].Key)
			}
		}
		return out, nil
	default:
		return MetadataValue{}, fmt.Errorf("unsupported metadata value type %s", value.Type())
	}
}

var jsonNumberPattern = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)

func classifyJSONNumber(raw string) (MetadataValueKind, error) {
	if err := validateJSONNumber(raw); err != nil {
		return "", err
	}
	if strings.ContainsAny(raw, ".eE") {
		return MetadataFloat, nil
	}
	return MetadataInteger, nil
}

func validateJSONNumber(raw string) error {
	if !jsonNumberPattern.MatchString(raw) || !json.Valid([]byte(raw)) {
		return fmt.Errorf("metadata number %q is invalid", raw)
	}
	return nil
}

func enterMetadataVisit(value reflect.Value, visiting map[metadataVisit]bool) (func(), error) {
	if value.Kind() == reflect.Array {
		return func() {}, nil
	}
	// A shorter slice can share its parent's storage without referring to itself.
	visit := metadataVisit{type_: value.Type(), kind: value.Kind(), ptr: value.Pointer(), len: value.Len()}
	if visit.ptr == 0 {
		return func() {}, nil
	}
	if visiting[visit] {
		return nil, fmt.Errorf("metadata value contains a cycle")
	}
	visiting[visit] = true
	return func() { delete(visiting, visit) }, nil
}
