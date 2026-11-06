package traffic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/atomicobject/rhizome/services/team-api/internal/ledger"
)

type pausedSettlement struct {
	ledger.Ledger
	entered, release chan struct{}
}

func (l *pausedSettlement) Settle(ctx context.Context, sub, key string, outcome ledger.Outcome, credits int64, dollars ...int64) error {
	close(l.entered)
	<-l.release
	return l.Ledger.Settle(ctx, sub, key, outcome, credits, dollars...)
}

func TestStreamSettlementCompletesBeforeEOF(t *testing.T) {
	// Larger than the HTTP write buffer, so JSON reaches the client before
	// the handler returns. Settlement is held by an event, never a sleep.
	h := newHarness(t, opts{voyage: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":"`+strings.Repeat("x", 32<<10)+`","usage":{"total_tokens":10}}`)
	})})
	l := &pausedSettlement{Ledger: h.tr.ledger, entered: make(chan struct{}), release: make(chan struct{})}
	h.tr.ledger = l
	var release sync.Once
	unblock := func() { release.Do(func() { close(l.release) }) }
	defer unblock()
	req, err := http.NewRequest(http.MethodPost, h.gw.URL+wire.RouteVoyageEmbed, strings.NewReader(voyageBody("x")))
	require.NoError(t, err)
	req.Header.Set("X-Test-Sub", "alice")
	req.Header.Set(wire.HeaderIdempotencyKey, key(1))
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var body json.RawMessage
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	<-l.entered
	// Decoding JSON alone does not wait for settlement.
	require.Equal(t, wire.CodeInProgress, h.voyage("alice", key(1), voyageBody("x")).code())
	unblock()
	_, err = io.Copy(io.Discard, resp.Body)
	require.NoError(t, err)
	require.Equal(t, wire.CodeResultLost, h.voyage("alice", key(1), voyageBody("x")).code())
}
