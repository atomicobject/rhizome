package diagnostics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/atomicobject/rhizome/pkg/fileio"
)

func (r *Recorder) prepareReport(ctx context.Context, report Report) (Report, []byte, error) {
	op := OperationFromContext(ctx)
	if report.OperationID == "" {
		report.OperationID = op.ID
	}
	if !validID.MatchString(report.OperationID) {
		return report, nil, fmt.Errorf("invalid diagnostics operation id")
	}
	if report.ParentOperationID == "" {
		report.ParentOperationID = op.ParentID
	}
	if report.TraceID == "" {
		report.TraceID = op.TraceID
	}
	if report.Kind == "" {
		report.Kind = op.Kind
	}
	if report.Trigger == "" {
		report.Trigger = op.Trigger
	}
	if report.StartedAt.IsZero() {
		report.StartedAt = op.StartedAt
	}
	if report.StartedAt.IsZero() {
		report.StartedAt = r.opts.Now()
	}
	if report.FinishedAt.IsZero() {
		report.FinishedAt = r.opts.Now()
	}
	report.StartedAt = report.StartedAt.UTC()
	report.FinishedAt = report.FinishedAt.UTC()
	if report.FinishedAt.Before(report.StartedAt) {
		return report, nil, fmt.Errorf("diagnostics report finishes before it starts")
	}
	if report.DurationMS == 0 {
		report.DurationMS = report.FinishedAt.Sub(report.StartedAt).Milliseconds()
	}
	if report.Kind == "" {
		return report, nil, fmt.Errorf("diagnostics report requires operation kind")
	}
	switch report.Status {
	case "success", "error", "canceled", "preempted", "skipped":
	default:
		return report, nil, fmt.Errorf("invalid diagnostics report status")
	}
	report.SchemaVersion = SchemaVersion
	report.ProcessID = r.processID
	report.PID = os.Getpid()
	report.Role = r.opts.Role
	report.Version = r.opts.Version
	var cut bool
	for _, field := range []*string{&report.ParentOperationID, &report.TraceID, &report.Kind, &report.Trigger, &report.ReasonCode} {
		*field, cut = boundedString(*field, 128)
		report.Truncated = report.Truncated || cut
	}
	report.Error, cut = boundedString(report.Error, 2048)
	report.Truncated = report.Truncated || cut
	report.Error = redact(report.Error)
	report.Summary, cut = boundedString(report.Summary, 8192)
	report.Truncated = report.Truncated || cut
	report.Summary = redact(report.Summary)
	report.Attributes, cut = boundedAttrs(report.Attributes)
	report.Truncated = report.Truncated || cut
	if len(report.Metrics) > r.opts.MaxReportBytes {
		report.Metrics = nil
		report.Truncated = true
	} else if len(report.Metrics) > 0 {
		var err error
		report.Metrics, cut, err = sanitizeMetrics(report.Metrics)
		if err != nil {
			return report, nil, err
		}
		report.Truncated = report.Truncated || cut
	}
	data, err := json.Marshal(report)
	if err != nil {
		return report, nil, err
	}
	if len(data) > r.opts.MaxReportBytes {
		report.Metrics = nil
		report.Summary = ""
		report.Attributes = nil
		report.Truncated = true
		data, err = json.Marshal(report)
	}
	if len(data) > r.opts.MaxReportBytes {
		return report, nil, fmt.Errorf("diagnostics report exceeds byte limit")
	}
	return report, data, err
}

// The caller owns which facts are safe to record. This additional boundary
// rejects objects beyond bounded JSON and redacts recognizable payload fields.
func sanitizeMetrics(data []byte) (json.RawMessage, bool, error) {
	// Validate nesting before decoding containers. The byte-limited input is
	// already the memory bound; producer-owned series and sample limits remain
	// authoritative rather than losing random metric families here.
	check := json.NewDecoder(bytes.NewReader(data))
	depth := 0
	for {
		token, err := check.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, false, fmt.Errorf("invalid diagnostics report metrics JSON")
		}
		if delim, ok := token.(json.Delim); ok {
			if delim == '{' || delim == '[' {
				depth++
				if depth > 32 {
					return nil, false, fmt.Errorf("diagnostics report metrics nesting limit reached")
				}
			} else {
				depth--
			}
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, false, fmt.Errorf("invalid diagnostics report metrics JSON")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, false, fmt.Errorf("invalid diagnostics report metrics JSON")
	}
	truncated := false
	var walk func(any) any
	walk = func(value any) any {
		switch v := value.(type) {
		case map[string]any:
			for key, item := range v {
				if sensitiveKey(key) {
					v[key] = "[redacted]"
					truncated = true
					continue
				}
				v[key] = walk(item)
			}
			return v
		case []any:
			for i, item := range v {
				v[i] = walk(item)
			}
			return v
		case string:
			text := redact(v)
			truncated = truncated || text != v
			return text
		default:
			return v
		}
	}
	clean := walk(value)
	result, err := json.Marshal(clean)
	return result, truncated, err
}

func (r *Recorder) publishReportLocked(report Report, data []byte) error {
	// Terminal evidence gets a bounded chance to outlast another publisher.
	// Caller cancellation must not discard the report describing cancellation.
	return r.withGuardWait(reportGuardWait, func() error {
		stamp := report.FinishedAt.Format("20060102T150405") + fmt.Sprintf("%09dZ", report.FinishedAt.Nanosecond())
		path := filepath.Join(r.dir, "reports", stamp+"-"+report.OperationID+".json")
		// A duplicate must not change latest or prune retained history.
		if _, err := os.Lstat(path); err == nil {
			return fmt.Errorf("diagnostics report already published")
		} else if !os.IsNotExist(err) {
			return err
		}
		latest := report.Kind == "index"
		latestPath := filepath.Join(r.dir, "latest-index.json")
		var oldLatestBytes int64
		replacing := false
		if latest {
			if info, err := os.Lstat(latestPath); err == nil {
				if !info.Mode().IsRegular() {
					return fmt.Errorf("latest index report must be a regular file")
				}
				oldLatestBytes = info.Size()
				replacing = true
			} else if err != nil && !os.IsNotExist(err) {
				return err
			}
			previous, err := ReadLatest(filepath.Dir(filepath.Dir(r.dir)), "index")
			if err == nil && previous.FinishedAt.After(report.FinishedAt) {
				latest = false
			}
		}
		// Replace latest first, so its old bytes are released before history
		// publication. Account for both possible peaks: old latest plus its
		// replacement temporary, and new latest plus new history temporary.
		bytes := int64(len(data))
		count := 1
		if latest {
			if replacing {
				bytes = max(bytes, 2*bytes-oldLatestBytes)
			} else {
				bytes *= 2
				count = 2
			}
		}
		if err := r.admitLocked(bytes, count, r.opts.Now().UTC()); err != nil {
			return err
		}
		if latest {
			if err := atomicWrite(r.dir, latestPath, data); err != nil {
				return err
			}
		}
		return atomicWrite(r.dir, path, data)
	})
}

func atomicWrite(dir, target string, data []byte) error {
	path := filepath.Join(dir, ".tmp-"+newID())
	file, err := openRegular(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY)
	if err != nil {
		return err
	}
	defer os.Remove(path)
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return fileio.Replace(path, target)
}
