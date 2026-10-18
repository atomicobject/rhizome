package embeddings

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDoWithRetry_ReplaysRequestBody(t *testing.T) {
	ctx := context.Background()
	payload := []byte(`{"input":"hello"}`)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://example.invalid/embeddings", io.NopCloser(bytes.NewReader(payload)))
	require.NoError(t, err)

	var calls int32
	var bodies [][]byte
	client := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			atomic.AddInt32(&calls, 1)
			body, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			bodies = append(bodies, body)
			require.Equal(t, int64(len(payload)), req.ContentLength)
			if calls == 1 {
				return &http.Response{
					StatusCode: http.StatusInternalServerError,
					Body:       io.NopCloser(bytes.NewReader([]byte("retry"))),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte("ok"))),
			}, nil
		}),
	}

	resp, err := doWithRetry(client, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	_ = resp.Body.Close()

	require.Equal(t, int32(2), atomic.LoadInt32(&calls))
	require.Len(t, bodies, 2)
	require.Equal(t, payload, bodies[0])
	require.Equal(t, payload, bodies[1])
}
