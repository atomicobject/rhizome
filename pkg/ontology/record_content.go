package ontology

import (
	"bytes"
	"encoding/json"
	"github.com/atomicobject/rhizome/pkg/noteformat"
)

// Native source content retains entry and item order. It never enters a prose
// renderer. The provider's original bytes remain separately available.
func (p *PreparedDocument) sourceValueJSON(id noteformat.ValueID) []byte {
	value, ok := p.value(id)
	if !ok {
		return nil
	}
	var out bytes.Buffer
	switch value.Kind {
	case noteformat.MetadataArray:
		out.WriteByte('[')
		for i, item := range value.Items {
			if i > 0 {
				out.WriteByte(',')
			}
			out.Write(p.sourceValueJSON(item))
		}
		out.WriteByte(']')
	case noteformat.MetadataObject:
		out.WriteByte('{')
		for i, entry := range value.Entries {
			if i > 0 {
				out.WriteByte(',')
			}
			key, _ := json.Marshal(sourceScalarText(entry.Key))
			out.Write(key)
			out.WriteByte(':')
			out.Write(p.sourceValueJSON(entry.Value))
		}
		out.WriteByte('}')
	default:
		if value.Scalar != nil {
			out.Write(value.Scalar.CanonicalJSON())
		}
	}
	return out.Bytes()
}
