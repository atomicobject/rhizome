package noteformat

// SourceValueFact stores each value once. Zero is absent; a present null has a
// nonzero ValueID and a MetadataNull scalar. Entries retain authored order.
type SourceValueFact struct {
	Kind    MetadataValueKind   `json:"kind"`
	Scalar  *MetadataValue      `json:"scalar,omitempty"`
	Items   []ValueID           `json:"items,omitempty"`
	Entries []SourceEntryFact   `json:"entries,omitempty"`
	Range   OptionalSourceRange `json:"range"`
}

type SourceEntryFact struct {
	Key      MetadataValue
	KeyRange OptionalSourceRange
	Value    ValueID
}

// AddMetadataValue appends an existing immutable metadata tree. Structured
// parsers instead append entries in authored order, preserving key kinds.
func (s *StructureFacts) AddMetadataValue(value MetadataValue) ValueID {
	v := SourceValueFact{Kind: value.Kind()}
	switch value.Kind() {
	case MetadataArray:
		for _, item := range value.Array() {
			v.Items = append(v.Items, s.AddMetadataValue(item))
		}
	case MetadataObject:
		for _, entry := range value.Object() {
			key, _ := NewMetadataValue(entry.Key)
			v.Entries = append(v.Entries, SourceEntryFact{Key: key, Value: s.AddMetadataValue(entry.Value)})
		}
	default:
		scalar := value.clone()
		v.Scalar = &scalar
	}
	s.Values = append(s.Values, v)
	return ValueID(len(s.Values))
}
