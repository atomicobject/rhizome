package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap/lane"
)

// EnsureIndexJob ensures a runtime and submits the explicit index job.
//
// A headless runtime can idle-exit between a successful probe and the first
// real request, so exactly one transport failure is retried with a fresh
// Ensure; anything else (and any second failure) is the caller's to handle.
func EnsureIndexJob(ctx context.Context, opts EnsureOptions) (*Client, IndexJobResponse, error) {
	for attempt := 0; ; attempt++ {
		result, err := Ensure(ctx, opts)
		if err != nil {
			return nil, IndexJobResponse{}, err
		}
		if result.Client == nil {
			return nil, IndexJobResponse{}, ErrNoRuntime
		}
		job, err := SubmitIndexJob(ctx, result.Client)
		if err == nil {
			return result.Client, job, nil
		}
		if attempt > 0 || ctx.Err() != nil || !transportFailure(err) {
			return nil, IndexJobResponse{}, err
		}
		if opts.Logf != nil {
			opts.Logf("vault runtime went away after the probe; retrying once")
		}
	}
}

// SubmitIndexJob creates or joins the runtime's explicit index job.
func SubmitIndexJob(ctx context.Context, c *Client) (IndexJobResponse, error) {
	req, err := c.NewRequest(ctx, http.MethodPost, IndexJobsPath, nil)
	if err != nil {
		return IndexJobResponse{}, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return IndexJobResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		return IndexJobResponse{}, statusError(resp, "submit index job")
	}
	var job IndexJobResponse
	if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
		return IndexJobResponse{}, fmt.Errorf("decode index job response: %w", err)
	}
	return job, nil
}

// CancelIndexJob asks the runtime to cancel the job. Cancellation is job-wide:
// every client joined to it sees the cancelled outcome.
func CancelIndexJob(ctx context.Context, c *Client, jobID string) error {
	req, err := c.NewRequest(ctx, http.MethodDelete, IndexJobsPathPrefix+jobID, nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= http.StatusBadRequest {
		return statusError(resp, "cancel index job")
	}
	return nil
}

// StreamIndexJob delivers the job's events until the terminal done event. The
// runtime always ends a stream with one, so a stream that closes without it is
// an error rather than a silent success.
func StreamIndexJob(ctx context.Context, c *Client, jobID string, onEvent func(lane.Event) error) error {
	req, err := c.NewRequest(ctx, http.MethodGet, IndexJobsPathPrefix+jobID+"/events", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return statusError(resp, "stream index job")
	}

	reader := bufio.NewReader(resp.Body)
	for {
		line, readErr := reader.ReadString('\n')
		if payload, ok := sseData(line); ok {
			var event lane.Event
			if err := json.Unmarshal([]byte(payload), &event); err != nil {
				return fmt.Errorf("decode index job event: %w", err)
			}
			if err := onEvent(event); err != nil {
				return err
			}
			if event.Type == lane.EventDone {
				return nil
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return fmt.Errorf("index job %s: event stream ended without a result", jobID)
			}
			return readErr
		}
	}
}

// sseData extracts one `data:` payload; comments (heartbeats), `event:` lines,
// and blank separators carry nothing the client needs.
func sseData(line string) (string, bool) {
	payload, ok := strings.CutPrefix(strings.TrimRight(line, "\r\n"), "data:")
	if !ok {
		return "", false
	}
	payload = strings.TrimSpace(payload)
	return payload, payload != ""
}

// transportFailure reports a request the runtime never answered, as opposed to
// one it answered with an error status or one the caller cancelled.
// StatusError is an HTTP answer from the runtime with a non-success status.
// It is never a transport failure: the runtime was there and said no.
type StatusError struct {
	Action string
	Status int
	Detail string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s: runtime returned %d: %s", e.Action, e.Status, e.Detail)
}

func transportFailure(err error) bool {
	var status *StatusError
	return err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) &&
		!errors.As(err, &status) && !errors.Is(err, ErrUnauthorized)
}

func statusError(resp *http.Response, action string) error {
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("%s: %w", action, ErrUnauthorized)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	detail := strings.TrimSpace(string(body))
	if detail == "" {
		detail = resp.Status
	}
	return &StatusError{Action: action, Status: resp.StatusCode, Detail: detail}
}
