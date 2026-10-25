package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"
)

// readWithin reads the payload, giving up when ctx ends: a host that never
// closes standard input must not hold the dispatcher past its deadline.
func readWithin(ctx context.Context, r io.Reader) ([]byte, error) {
	type read struct {
		data []byte
		err  error
	}
	done := make(chan read, 1)
	go func() {
		data, err := io.ReadAll(io.LimitReader(r, maxPayloadBytes+1))
		done <- read{data, err}
	}()
	select {
	case got := <-done:
		return got.data, got.err
	case <-ctx.Done():
		return nil, errors.New("standard input stayed open past the deadline")
	}
}

func deadlineOf(ctx context.Context) time.Time {
	if d, ok := ctx.Deadline(); ok {
		return d
	}
	return time.Now().Add(Deadline)
}

// writeOutput emits context in the host's event-specific JSON form. Claude
// Code and Codex both read hookSpecificOutput.additionalContext.
func writeOutput(w io.Writer, ev Event, text string) error {
	return writeJSONLine(w, map[string]any{"hookSpecificOutput": map[string]string{
		"hookEventName":     HostEvent(ev.Event),
		"additionalContext": text,
	}})
}

func writeJSONLine(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = w.Write(append(data, '\n'))
	return err
}

// boundedBuffer keeps at most limit bytes and notes whether more arrived.
type boundedBuffer struct {
	bytes.Buffer
	limit    int
	overflow bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if room := b.limit - b.Len(); len(p) > room {
		b.overflow = true
		if room > 0 {
			b.Buffer.Write(p[:room])
		}
		return len(p), nil
	}
	return b.Buffer.Write(p)
}
