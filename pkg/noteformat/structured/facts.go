package structured

import (
	"encoding/json"
	"time"

	"github.com/atomicobject/rhizome/pkg/noteformat"
)

// Each parsed occurrence becomes one owner and one value. YAML supplies start
// positions, not exact end bytes, so this adapter does not manufacture ranges.
func structureFacts(root *Value) (*noteformat.StructureFacts, error) {
	facts := &noteformat.StructureFacts{Root: "value:"}
	var walk func(*Value, noteformat.OwnerKey, int) (noteformat.ValueID, error)
	walk = func(v *Value, parent noteformat.OwnerKey, ordinal int) (noteformat.ValueID, error) {
		id := noteformat.ValueID(len(facts.Values) + 1)
		facts.Values = append(facts.Values, noteformat.SourceValueFact{})
		key := noteformat.OwnerKey("value:" + v.Address())
		shape := noteformat.OwnerScalar
		if v.Kind() == Object {
			shape = noteformat.OwnerMapping
		}
		if v.Kind() == Array {
			shape = noteformat.OwnerSequence
		}
		if parent == "" {
			shape = noteformat.OwnerDocument
		}
		ownerIndex := len(facts.Owners)
		facts.Owners = append(facts.Owners, noteformat.OwnerFact{Key: key, Parent: parent, Collection: "values", Ordinal: ordinal, Order: ownerIndex, Shape: shape, Address: v.Address(), Value: id})
		value := noteformat.SourceValueFact{}
		switch v.Kind() {
		case Object:
			value.Kind = noteformat.MetadataObject
			for i, entry := range v.Entries() {
				child, err := walk(entry.Value, key, i)
				if err != nil {
					return 0, err
				}
				var raw any = entry.Key
				if entry.KeyKind == Number {
					raw = json.Number(entry.Key)
				}
				k, err := noteformat.NewMetadataValue(raw)
				if err != nil {
					return 0, err
				}
				value.Entries = append(value.Entries, noteformat.SourceEntryFact{Key: k, Value: child})
				// Empty mapping keys remain in raw source values; property bindings
				// require a nonempty name.
				if entry.Key != "" {
					facts.Owners[ownerIndex].Properties = append(facts.Owners[ownerIndex].Properties, noteformat.PropertyOccurrenceFact{Key: entry.Key, Source: "property", Value: child})
				}
			}
		case Array:
			value.Kind = noteformat.MetadataArray
			for i, item := range v.Items() {
				child, err := walk(item, key, i)
				if err != nil {
					return 0, err
				}
				value.Items = append(value.Items, child)
			}
		default:
			scalar, err := metadataScalar(v)
			if err != nil {
				return 0, err
			}
			value.Kind, value.Scalar = scalar.Kind(), &scalar
		}
		facts.Values[id-1] = value
		return id, nil
	}
	if _, err := walk(root, "", 0); err != nil {
		return nil, err
	}
	return noteformat.NewStructureFacts(*facts, -1)
}

func metadataScalar(v *Value) (noteformat.MetadataValue, error) {
	var raw any
	switch v.Kind() {
	case Null:
	case Bool:
		raw = v.Text() == "true"
	case Number:
		raw = json.Number(v.Text())
	case Date:
		at, err := time.Parse(time.DateOnly, v.Text())
		return noteformat.NewDateMetadataValue(at), err
	case Timestamp:
		at, err := time.Parse(time.RFC3339Nano, v.Text())
		return noteformat.NewTimestampMetadataValue(at), err
	default:
		raw = v.Text()
	}
	return noteformat.NewMetadataValue(raw)
}
