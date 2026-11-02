package traffic

import (
	"encoding/json"
	"io"
)

// usageValidation checks the complete response grammar while bytes stream to
// the caller. The decoder retains individual tokens, not an embedding array.
type usageValidation struct {
	writer *io.PipeWriter
	done   chan bool
	closed bool
	valid  bool
}

func newUsageValidation() *usageValidation {
	r, w := io.Pipe()
	v := &usageValidation{writer: w, done: make(chan bool, 1)}
	go func() {
		valid := validUsageResponse(r)
		// Malformed metadata cannot interrupt the original upstream response.
		// Drain until the relay closes, including when validation failed early.
		_, _ = io.Copy(io.Discard, r)
		_ = r.Close()
		v.done <- valid
	}()
	return v
}

func (v *usageValidation) write(p []byte) {
	_, _ = v.writer.Write(p)
}

func (v *usageValidation) close() bool {
	if !v.closed {
		_ = v.writer.Close()
		v.valid = <-v.done
		v.closed = true
	}
	return v.valid
}

func validUsageResponse(r io.Reader) bool {
	dec := json.NewDecoder(r)
	dec.UseNumber()
	first, err := dec.Token()
	if err != nil || first != json.Delim('{') {
		return false
	}
	depth, usages := 1, 0
	wantKey := true
	for depth != 0 {
		tok, err := dec.Token()
		if err != nil {
			return false
		}
		if delim, ok := tok.(json.Delim); ok {
			switch delim {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 1 {
					wantKey = true
				}
			}
		} else if depth == 1 {
			if wantKey && tok == "usage" {
				usages++
				if usages > 1 {
					return false
				}
			}
			wantKey = !wantKey
		}
	}
	_, err = dec.Token()
	return err == io.EOF && usages == 1
}
