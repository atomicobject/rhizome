package store

import (
	"encoding/binary"
	"github.com/atomicobject/rhizome/pkg/embeddings"
	"math"
)

func embedToBytes(emb embeddings.Embedding) []byte {
	if len(emb) == 0 {
		return nil
	}
	b := make([]byte, 4*len(emb))
	for i, f := range emb {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

func bytesToEmbed(b []byte) embeddings.Embedding {
	if len(b)%4 != 0 {
		return nil
	}
	out := make(embeddings.Embedding, len(b)/4)
	for i := range out {
		bits := binary.LittleEndian.Uint32(b[i*4:])
		out[i] = math.Float32frombits(bits)
	}
	return out
}

func dotFloat64(a, b embeddings.Embedding) float64 {
	if len(a) != len(b) {
		return 0
	}
	var sum float64
	for i := range a {
		sum += float64(a[i]) * float64(b[i])
	}
	return sum
}
