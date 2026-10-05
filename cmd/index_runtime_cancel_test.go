package cmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	appruntime "github.com/atomicobject/rhizome/pkg/app/runtime"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestInterruptedIndexDoesNotWaitForeverForRuntimeCompletion(t *testing.T) {
	for _, status := range []int{http.StatusAccepted, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			streaming := make(chan struct{})
			cancelled := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodDelete {
					close(cancelled)
					w.WriteHeader(status)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
				close(streaming)
				<-r.Context().Done()
			}))
			defer server.Close()
			defer server.CloseClientConnections()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			client := &appruntime.Client{Manifest: appruntime.InstanceManifest{HTTPURL: server.URL}, HTTP: server.Client()}
			command := &cobra.Command{}
			var output bytes.Buffer
			command.SetErr(&output)
			done := make(chan error, 1)
			go func() { done <- renderRuntimeIndexJob(ctx, command, client, "job-1") }()
			select {
			case <-streaming:
			case <-time.After(5 * time.Second):
				t.Fatal("index did not open the event stream")
			}
			cancel()
			select {
			case <-cancelled:
			case <-time.After(5 * time.Second):
				t.Fatal("index did not request cancellation")
			}
			select {
			case err := <-done:
				var coded interface{ ExitCode() int }
				require.ErrorAs(t, err, &coded)
				require.Equal(t, 130, coded.ExitCode())
				require.Contains(t, output.String(), "did not confirm")
			case <-time.After(appruntime.StopGrace + 5*time.Second):
				t.Fatal("canceled index remained stuck on the worker's event stream")
			}
		})
	}
}
