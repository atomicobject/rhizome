// Package diagnostics implements the offline CLI diagnostics reader.
package diagnostics

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	evidence "github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

type Options struct {
	Vault, Mode, OperationID, TraceID, Since, Until, Kind, Status, Level, Subsystem string
	Last                                                                            bool
	Limit                                                                           int
	MaxBytes                                                                        int64
	Now                                                                             time.Time
}

// Result preserves the stored records and makes evidence gaps explicit.
type Result struct {
	SchemaVersion  int               `json:"schema_version"`
	VaultRoot      string            `json:"vault_root"`
	Mode           string            `json:"mode"`
	Reports        []evidence.Report `json:"reports,omitempty"`
	Events         []evidence.Event  `json:"events,omitempty"`
	Coverage       evidence.Coverage `json:"coverage"`
	IndexSummaries []IndexSummary    `json:"index_summaries,omitempty"`
	Error          string            `json:"error,omitempty"`
}

func Read(opts Options) (Result, error) {
	out := Result{SchemaVersion: evidence.SchemaVersion, Mode: opts.Mode}
	root, err := ResolveRoot(opts.Vault)
	if err != nil {
		return out, err
	}
	out.VaultRoot = root
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	if opts.Limit < 0 || opts.MaxBytes < 0 {
		return out, errors.New("limit and max-bytes must be nonnegative")
	}
	if opts.Last && (opts.Since != "" || opts.Until != "") {
		return out, errors.New("--last and history time filters select different reads; use one")
	}
	if opts.Mode == "report" || opts.Mode == "index" && (opts.Last || opts.Since == "" && opts.Until == "") {
		var report evidence.Report
		if opts.Mode == "report" {
			report, err = evidence.ReadReport(root, opts.OperationID)
		} else {
			report, err = evidence.ReadLatest(root, "index")
		}
		if errors.Is(err, evidence.ErrNotFound) && opts.Mode == "index" {
			out.Coverage.Warnings = []string{"No completed index attempt has a retained latest report."}
			return out, nil
		}
		if err != nil {
			return out, err
		}
		out.Reports = []evidence.Report{report}
		out.Coverage = evidence.Coverage{Since: report.StartedAt, Until: report.FinishedAt, Files: 1,
			Warnings: []string{"This is one retained report, independent of the history retention window; it does not prove complete history."}}
	} else {
		since := opts.Since
		if since == "" {
			since = "7d"
		}
		filter := evidence.Filter{Kind: opts.Kind, Status: opts.Status, OperationID: opts.OperationID, TraceID: opts.TraceID, Subsystem: opts.Subsystem, Limit: opts.Limit, MaxBytes: opts.MaxBytes}
		filter.Since, err = ParseTime(since, opts.Now)
		if err != nil {
			return out, fmt.Errorf("since: %w", err)
		}
		filter.Until, err = ParseTime(opts.Until, opts.Now)
		if err != nil {
			return out, fmt.Errorf("until: %w", err)
		}
		if !filter.Until.IsZero() && filter.Until.Before(filter.Since) {
			return out, errors.New("until must not precede since")
		}
		switch opts.Mode {
		case "index", "reports":
			if opts.Mode == "index" {
				filter.Kind = "index"
			}
			result, readErr := evidence.ReadReports(root, filter)
			out.Reports, out.Coverage, err = result.Reports, result.Coverage, readErr
		case "logs":
			filter.Level, err = ParseLevel(opts.Level)
			if err != nil {
				return out, err
			}
			result, readErr := evidence.ReadEvents(root, filter)
			out.Events, out.Coverage, err = result.Events, result.Coverage, readErr
		default:
			return out, fmt.Errorf("unknown diagnostics mode %q", opts.Mode)
		}
		if err != nil {
			return out, err
		}
	}
	for _, report := range out.Reports {
		if report.Kind == "index" || report.Kind == "indexing-job" {
			out.IndexSummaries = append(out.IndexSummaries, SummarizeIndex(report))
		}
	}
	return out, nil
}

// ResolveRoot deliberately avoids loading repository configuration or an index.
// Existing directories take precedence over registered vault names.
func ResolveRoot(input string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	base, err := paths.NewVaultPaths(cwd)
	if err != nil {
		return "", err
	}
	if input != "" {
		root := paths.AbsFromInputWithVaultPaths(base, cwd, input).String()
		if info, statErr := os.Stat(root); statErr == nil && info.IsDir() {
			return root, nil
		}
		if strings.ContainsAny(input, "/\\") || strings.HasPrefix(input, ".") {
			return "", fmt.Errorf("vault directory %q does not exist", input)
		}
		vault := &obsidian.Vault{Name: input}
		root, err = vault.Path()
		if err != nil {
			return "", fmt.Errorf("resolve vault: %w; use --vault /absolute/vault/path to bypass configuration", err)
		}
		resolved, pathErr := paths.NewVaultPaths(root)
		if pathErr != nil {
			return "", pathErr
		}
		return resolved.Root(), nil
	}
	for root := base.Root(); root != ""; root = filepath.Dir(root) {
		if info, statErr := os.Stat(filepath.Join(root, ".rhizome")); statErr == nil && info.IsDir() {
			return root, nil
		}
		if filepath.Dir(root) == root {
			break
		}
	}
	return base.Root(), nil
}

// ParseTime accepts RFC3339 timestamps, Go durations, and whole or fractional days.
func ParseTime(input string, now time.Time) (time.Time, error) {
	if input == "" {
		return time.Time{}, nil
	}
	if absolute, err := time.Parse(time.RFC3339Nano, input); err == nil {
		return absolute, nil
	}
	durationInput := input
	if strings.HasSuffix(input, "d") {
		days, err := strconv.ParseFloat(strings.TrimSuffix(input, "d"), 64)
		if err != nil || days < 0 || days > 36500 {
			return time.Time{}, fmt.Errorf("invalid time %q", input)
		}
		durationInput = strconv.FormatFloat(days*24, 'f', -1, 64) + "h"
	}
	duration, err := time.ParseDuration(durationInput)
	if err != nil || duration < 0 {
		return time.Time{}, fmt.Errorf("invalid time %q; use 7d, 2h, or RFC3339", input)
	}
	return now.Add(-duration), nil
}

func ParseLevel(input string) (slog.Level, error) {
	switch strings.ToLower(input) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("invalid level %q; use debug, info, warn, or error", input)
	}
}
