package sqlite

import (
	"encoding/binary"
	"math"

	"github.com/atomicobject/rhizome/pkg/embeddings"
)

func embedToBytes(e embeddings.Embedding) []byte {
	b := make([]byte, 4*len(e))
	for i, f := range e {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

func bytesToEmbed(b []byte) embeddings.Embedding {
	if len(b)%4 != 0 {
		return nil
	}
	n := len(b) / 4
	e := make(embeddings.Embedding, n)
	for i := 0; i < n; i++ {
		e[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return e
}

func dotFloat64(a, b embeddings.Embedding) float64 {
	var dot float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
	}
	return dot
}
