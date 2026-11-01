package diagnostics

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
)

func TestStreamRetainsRedactedJSONLines(t *testing.T) {
	var out bytes.Buffer
	rec := OpenStream(&out, Options{Role: "team-api"})
	rec.Event(context.Background(), slog.LevelWarn, "teamapi", "request", "", slog.String("authorization", "Bearer synthetic"), slog.Int("status", 403))
	rec.Event(context.Background(), slog.LevelInfo, "teamapi", "ready", "")
	dec := json.NewDecoder(&out)
	var first, second Event
	if err := dec.Decode(&first); err != nil {
		t.Fatal(err)
	}
	if err := dec.Decode(&second); err != nil {
		t.Fatal(err)
	}
	if first.Attributes["authorization"] != "[redacted]" || first.Role != "team-api" || second.Sequence != 2 {
		t.Fatalf("unexpected events: %+v %+v", first, second)
	}
	if err := dec.Decode(&Event{}); err != io.EOF {
		t.Fatalf("extra output: %v", err)
	}
	if err := rec.Close(); err != nil {
		t.Fatal(err)
	}
	// Close never takes ownership of the destination; late events are dropped.
	rec.Event(context.Background(), slog.LevelInfo, "teamapi", "late", "")
	if out.Len() != 0 {
		t.Fatal("closed recorder wrote an event")
	}
}
