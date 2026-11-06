// Package structured parses read-only YAML and JSON without ontology bindings.
package structured

import "strings"

// ScalarProfile versions the provider-local PyYAML 6.0.3 SafeLoader subset.
const ScalarProfile = "pyyaml-6.0.3-v1"

type Kind string

const (
	Null      Kind = "null"
	Bool      Kind = "bool"
	Number    Kind = "number"
	String    Kind = "string"
	Date      Kind = "date"
	Timestamp Kind = "timestamp"
	Array     Kind = "array"
	Object    Kind = "object"
)

// Position is a one-based line/Unicode column and zero-based original byte start.
// A zero Line means the parser supplied no reliable location. No end is claimed.
type Position struct{ Line, Column, Offset int }

type Diagnostic struct {
	Code    string
	Message string
	Address string
	Start   Position
	Related []Position
}

// Entry retains authored order and the supported scalar key kind.
type Entry struct {
	Key     string
	KeyKind Kind
	Start   Position
	Value   *Value
}

// Value is immutable. Collection accessors return detached slices; children are
// immutable too. Text is canonical scalar text; Lexeme preserves decoded spelling.
type Value struct {
	kind                  Kind
	text, lexeme, address string
	start                 Position
	items                 []*Value
	entries               []Entry
}

func (v *Value) Kind() Kind      { return v.kind }
func (v *Value) Text() string    { return v.text }
func (v *Value) Lexeme() string  { return v.lexeme }
func (v *Value) Address() string { return v.address }
func (v *Value) Start() Position { return v.start }
func (v *Value) Items() []*Value {
	items := make([]*Value, len(v.items))
	for i, item := range v.items {
		items[i] = detached(item)
	}
	return items
}
func (v *Value) Entries() []Entry {
	entries := append([]Entry(nil), v.entries...)
	for i := range entries {
		entries[i].Value = detached(entries[i].Value)
	}
	return entries
}
func detached(v *Value) *Value { copy := *v; return &copy }

// Lookup distinguishes a missing key from a present null or empty value.
func (v *Value) Lookup(key string) (*Value, bool) {
	for _, e := range v.entries {
		if e.Key == key {
			return detached(e.Value), true
		}
	}
	return nil, false
}

func childAddress(parent, key string) string {
	return parent + "/" + strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}
