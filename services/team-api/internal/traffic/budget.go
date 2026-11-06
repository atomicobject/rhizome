package traffic

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"math"

	"github.com/atomicobject/rhizome/services/team-api/internal/providerbudget"
)

// billingTokens rejects ambiguous usage instead of refunding a guessed charge.
func billingTokens(usage []byte, field string) (int64, bool) {
	dec := json.NewDecoder(bytes.NewReader(usage))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return 0, false
	}
	seen := map[string]bool{}
	var tokens *int64
	for dec.More() {
		tok, err := dec.Token()
		key, ok := tok.(string)
		if err != nil || !ok || seen[key] {
			return 0, false
		}
		seen[key] = true
		if key == field {
			if err := dec.Decode(&tokens); err != nil {
				return 0, false
			}
		} else {
			var ignored json.RawMessage
			if err := dec.Decode(&ignored); err != nil {
				return 0, false
			}
		}
	}
	if tok, err := dec.Token(); err != nil || tok != json.Delim('}') {
		return 0, false
	}
	if _, err := dec.Token(); err != io.EOF || tokens == nil || *tokens < 0 {
		return 0, false
	}
	return *tokens, true
}

func (t *Traffic) billedNanodollars(ctx context.Context, ev *event, usage []byte, provider string, price providerbudget.Price) int64 {
	field := "input_tokens"
	if provider == providerbudget.Voyage {
		field = "total_tokens"
	}
	tokens, ok := billingTokens(usage, field)
	if !ok {
		return -1
	}
	n, ok := providerbudget.Cost(price, tokens)
	if !ok {
		// A valid integer usage above arithmetic range must still fence the
		// reviewed bound, rather than being mistaken for absent usage.
		n = math.MaxInt64
	}
	if tokens > price.MaxBillableTokens && t.rec != nil {
		t.rec.Event(context.WithoutCancel(ctx), slog.LevelError, "teamapi.traffic", "teamapi.price_bound_exceeded", "provider usage exceeds the reviewed request bound",
			slog.String("request_id", ev.requestID), slog.String("provider", provider), slog.String("model", price.Model),
			slog.String("price_version", price.Version), slog.Int64("billable_tokens", tokens), slog.Int64("max_billable_tokens", price.MaxBillableTokens))
	}
	return n
}
