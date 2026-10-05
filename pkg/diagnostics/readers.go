package diagnostics

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/fileio"
)

func readerRoot(vaultRoot string) (string, error) {
	dir := filepath.Join(vaultRoot, ".rhizome", "diagnostics")
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return dir, ErrNotFound
	}
	if err != nil {
		return dir, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return dir, fmt.Errorf("diagnostics path must be a directory")
	}
	return dir, nil
}

func ReadLatest(vaultRoot, kind string) (Report, error) {
	if kind != "index" {
		return Report{}, fmt.Errorf("latest report is available only for index operations")
	}
	dir, err := readerRoot(vaultRoot)
	if err != nil {
		return Report{}, err
	}
	report, _, err := readReportFile(filepath.Join(dir, "latest-index.json"), DefaultMaxReportBytes)
	if err == nil && report.Kind != "index" {
		return Report{}, fmt.Errorf("invalid latest index report kind")
	}
	return report, err
}

func ReadReport(vaultRoot, operationID string) (Report, error) {
	if !validID.MatchString(operationID) {
		return Report{}, fmt.Errorf("invalid diagnostics operation id")
	}
	dir, err := readerRoot(vaultRoot)
	if err != nil {
		return Report{}, err
	}
	coverage := Coverage{}
	files, err := readerFiles(filepath.Join(dir, "reports"), reportFilename.MatchString, &coverage, false)
	if err != nil {
		return Report{}, err
	}
	var historyErr error
	for _, path := range files {
		name := filepath.Base(path)
		// The timestamp prefix has fixed width; suffix matching would confuse
		// operation IDs containing hyphens with a different longer ID.
		if name[26:len(name)-5] == operationID {
			report, _, err := readReportFile(path, DefaultMaxReportBytes)
			if err == nil && report.OperationID != operationID {
				err = fmt.Errorf("diagnostics report identity mismatch")
			}
			if err == nil {
				return report, nil
			}
			historyErr = err
		}
	}
	report, err := ReadLatest(vaultRoot, "index")
	if err == nil && report.OperationID == operationID {
		return report, nil
	}
	if historyErr != nil {
		return Report{}, historyErr
	}
	return Report{}, ErrNotFound
}

func readReportFile(path string, maxBytes int64) (Report, int64, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return Report{}, 0, ErrNotFound
	}
	if err != nil {
		return Report{}, 0, err
	}
	if !info.Mode().IsRegular() {
		return Report{}, 0, fmt.Errorf("diagnostics report must be a regular file")
	}
	if info.Size() > maxBytes {
		return Report{}, 0, fmt.Errorf("diagnostics report exceeds read byte limit")
	}
	file, err := fileio.OpenRead(path)
	if err != nil {
		return Report{}, 0, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxBytes))
	if err != nil {
		return Report{}, int64(len(data)), err
	}
	if int64(len(data)) > maxBytes {
		return Report{}, int64(len(data)), fmt.Errorf("diagnostics report exceeds read byte limit")
	}
	var report Report
	if err := json.Unmarshal(data, &report); err != nil {
		return Report{}, int64(len(data)), fmt.Errorf("malformed diagnostics report")
	}
	if report.SchemaVersion != SchemaVersion {
		return Report{}, int64(len(data)), fmt.Errorf("unsupported diagnostics report schema")
	}
	if !validID.MatchString(report.OperationID) || report.Kind == "" || report.FinishedAt.IsZero() {
		return Report{}, int64(len(data)), fmt.Errorf("incomplete diagnostics report")
	}
	return report, int64(len(data)), nil
}

func readerLimits(filter Filter) Filter {
	if filter.Limit <= 0 {
		filter.Limit = 100
	}
	if filter.Limit > 1000 {
		filter.Limit = 1000
	}
	if filter.MaxBytes <= 0 {
		filter.MaxBytes = 4 << 20
	}
	if filter.MaxBytes > 16<<20 {
		filter.MaxBytes = 16 << 20
	}
	return filter
}

func readerFiles(dir string, known func(string) bool, coverage *Coverage, newestModified bool) ([]string, error) {
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("diagnostics artifact path must be a directory")
	}
	file, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var files []string
	modified := make(map[string]time.Time)
	order := func() {
		sort.Slice(files, func(i, j int) bool {
			if newestModified && !modified[files[i]].Equal(modified[files[j]]) {
				return modified[files[i]].After(modified[files[j]])
			}
			return files[i] > files[j]
		})
	}
	count := 0
	for {
		entries, err := file.ReadDir(64)
		for _, entry := range entries {
			count++
			if count > maxScannedEntries {
				coverage.Truncated = true
				coverage.warn("diagnostics directory scan limit reached")
				order()
				return files, nil
			}
			if !known(entry.Name()) || entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
				continue
			}
			info, infoErr := entry.Info()
			if infoErr != nil {
				coverage.warn("diagnostics artifact metadata unavailable")
				continue
			}
			if !info.Mode().IsRegular() {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			files = append(files, path)
			modified[path] = info.ModTime()
			if len(files) > maxStoredFiles {
				order()
				delete(modified, files[maxStoredFiles])
				files = files[:maxStoredFiles]
				coverage.Truncated = true
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	order()
	return files, nil
}

func (c *Coverage) warn(message string) {
	if len(c.Warnings) < 15 {
		c.Warnings = append(c.Warnings, message)
	} else if len(c.Warnings) == 15 {
		c.Warnings = append(c.Warnings, "additional diagnostics warnings omitted")
	}
}

func (c *Coverage) observe(at time.Time) {
	if c.Since.IsZero() || at.Before(c.Since) {
		c.Since = at
	}
	if c.Until.IsZero() || at.After(c.Until) {
		c.Until = at
	}
}

func (c *Coverage) finish(filter Filter) {
	if c.Since.IsZero() {
		c.warn("no retained diagnostics records were examined; coverage is unknown")
	} else if !filter.Since.IsZero() && filter.Since.Before(c.Since) {
		c.warn("requested start precedes examined retained evidence; earlier coverage is unknown")
	}
}

func within(at time.Time, filter Filter) bool {
	return (filter.Since.IsZero() || !at.Before(filter.Since)) && (filter.Until.IsZero() || !at.After(filter.Until))
}

func ReadReports(vaultRoot string, filter Filter) (ReportResult, error) {
	result := ReportResult{Reports: []Report{}}
	dir, err := readerRoot(vaultRoot)
	if errors.Is(err, ErrNotFound) {
		result.Coverage.finish(filter)
		return result, nil
	}
	if err != nil {
		return result, err
	}
	filter = readerLimits(filter)
	files, err := readerFiles(filepath.Join(dir, "reports"), reportFilename.MatchString, &result.Coverage, false)
	if err != nil {
		return result, err
	}
	for _, path := range files {
		remaining := filter.MaxBytes - result.Coverage.Bytes
		if remaining <= 0 {
			result.Coverage.Truncated = true
			result.Coverage.warn("report read byte limit reached")
			break
		}
		report, bytes, err := readReportFile(path, min(remaining, int64(DefaultMaxReportBytes)))
		result.Coverage.Files++
		result.Coverage.Bytes += min(bytes, remaining)
		if err != nil {
			if strings.Contains(err.Error(), "read byte limit") {
				result.Coverage.Truncated = true
			}
			result.Coverage.warn(filepath.Base(path) + ": " + err.Error())
			continue
		}
		result.Coverage.observe(report.FinishedAt)
		if !within(report.FinishedAt, filter) || (filter.Kind != "" && report.Kind != filter.Kind) ||
			(filter.Status != "" && report.Status != filter.Status) || (filter.OperationID != "" && report.OperationID != filter.OperationID) ||
			(filter.TraceID != "" && report.TraceID != filter.TraceID) {
			continue
		}
		if len(result.Reports) == filter.Limit {
			result.Coverage.Truncated = true
			continue
		}
		result.Reports = append(result.Reports, report)
	}
	sort.Slice(result.Reports, func(i, j int) bool { return result.Reports[i].FinishedAt.After(result.Reports[j].FinishedAt) })
	result.Coverage.finish(filter)
	return result, nil
}

func ReadEvents(vaultRoot string, filter Filter) (EventResult, error) {
	result := EventResult{Events: []Event{}}
	dir, err := readerRoot(vaultRoot)
	if errors.Is(err, ErrNotFound) {
		result.Coverage.finish(filter)
		return result, nil
	}
	if err != nil {
		return result, err
	}
	filter = readerLimits(filter)
	files, err := readerFiles(filepath.Join(dir, "events"), eventFilename.MatchString, &result.Coverage, true)
	if err != nil {
		return result, err
	}
	for i, path := range files {
		remaining := filter.MaxBytes - result.Coverage.Bytes
		if remaining <= 0 {
			result.Coverage.Truncated = true
			result.Coverage.warn("event read byte limit reached")
			break
		}
		file, err := fileio.OpenRead(path)
		if err != nil {
			result.Coverage.warn(filepath.Base(path) + ": cannot open event file")
			continue
		}
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			result.Coverage.warn(filepath.Base(path) + ": event metadata unavailable")
			_ = file.Close()
			continue
		}
		result.Coverage.Files++
		// Share the remaining budget without shrinking dense histories' tails
		// below a useful record window. Small files leave bytes for later files.
		allowance := min(remaining, max(int64(256<<10), remaining/int64(len(files)-i)))
		start := max(int64(0), info.Size()-allowance)
		readBytes := min(info.Size(), allowance)
		limited := &io.LimitedReader{R: io.NewSectionReader(file, start, readBytes), N: readBytes}
		reader := bufio.NewReaderSize(limited, DefaultMaxEventBytes)
		if start > 0 {
			result.Coverage.Truncated = true
			result.Coverage.warn("event read byte limit reached")
			// Discard the first line because its beginning is outside our
			// coverage. A byte-budget boundary is not record corruption.
			lineErr := bufio.ErrBufferFull
			for lineErr == bufio.ErrBufferFull {
				_, lineErr = reader.ReadSlice('\n')
			}
			if lineErr != nil && lineErr != io.EOF {
				result.Coverage.warn(filepath.Base(path) + ": event read failed")
			}
		}
		for {
			line, lineErr := reader.ReadSlice('\n')
			if lineErr == bufio.ErrBufferFull {
				result.Coverage.warn(filepath.Base(path) + ": oversized event record omitted")
				for lineErr == bufio.ErrBufferFull {
					_, lineErr = reader.ReadSlice('\n')
				}
				if lineErr != nil {
					break
				}
				continue
			}
			if len(line) > 0 {
				var event Event
				if json.Unmarshal(line, &event) != nil {
					result.Coverage.warn(filepath.Base(path) + ": malformed event record")
				} else if event.SchemaVersion != SchemaVersion {
					result.Coverage.warn(filepath.Base(path) + ": unsupported event schema")
				} else if event.Time.IsZero() || event.ProcessID == "" {
					result.Coverage.warn(filepath.Base(path) + ": incomplete event record")
				} else {
					result.Coverage.observe(event.Time)
					var level slog.Level
					if level.UnmarshalText([]byte(event.Level)) != nil {
						result.Coverage.warn(filepath.Base(path) + ": invalid event level")
					} else if level >= filter.Level && within(event.Time, filter) &&
						(filter.OperationID == "" || event.OperationID == filter.OperationID) &&
						(filter.TraceID == "" || event.TraceID == filter.TraceID) &&
						(filter.Subsystem == "" || event.Subsystem == filter.Subsystem) {
						result.Events = append(result.Events, event)
						if len(result.Events) > filter.Limit {
							sort.Slice(result.Events, func(i, j int) bool { return eventBefore(result.Events[i], result.Events[j]) })
							result.Events = result.Events[1:]
							result.Coverage.Truncated = true
						}
					}
				}
				if lineErr == io.EOF {
					result.Coverage.warn(filepath.Base(path) + ": unterminated final event record")
				}
			}
			if lineErr != nil {
				if lineErr != io.EOF {
					result.Coverage.warn(filepath.Base(path) + ": event read failed")
				}
				break
			}
		}
		result.Coverage.Bytes += readBytes - limited.N
		_ = file.Close()
	}
	sort.Slice(result.Events, func(i, j int) bool { return eventBefore(result.Events[i], result.Events[j]) })
	result.Coverage.finish(filter)
	return result, nil
}

func eventBefore(a, b Event) bool {
	if !a.Time.Equal(b.Time) {
		return a.Time.Before(b.Time)
	}
	if a.ProcessID != b.ProcessID {
		return a.ProcessID < b.ProcessID
	}
	return a.Sequence < b.Sequence
}
