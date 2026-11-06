package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func streamJSONEvents[T any](w http.ResponseWriter, r *http.Request, flusher http.Flusher, events <-chan T, frame func(T) (id, name string)) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			_, _ = fmt.Fprintf(w, ": heartbeat %d\n\n", time.Now().Unix())
			flusher.Flush()
		case event, ok := <-events:
			if !ok {
				return
			}
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			id, name := frame(event)
			_, _ = fmt.Fprintf(w, "id: %s\n", id)
			if name != "" {
				_, _ = fmt.Fprintf(w, "event: %s\n", name)
			}
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}
