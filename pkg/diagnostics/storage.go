package diagnostics

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var errGuardBusy = errors.New("diagnostics storage guard busy")
var errQuota = errors.New("diagnostics byte or file limit reached")
var eventFilename = regexp.MustCompile(`^(\d{8})-([a-zA-Z0-9_-]+)-(\d{6,})-(\d+)\.jsonl$`)
var reportFilename = regexp.MustCompile(`^\d{8}T\d{15}Z-[a-zA-Z0-9_-]+\.json$`)
var tempFilename = regexp.MustCompile(`^\.tmp-[0-9a-f]{32}$`)
var validID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

const maxStoredFiles = 4096
const maxScannedEntries = 16384
const reportGuardWait = 2 * time.Second
const segmentGuardWait = reportGuardWait

func ensureDirectories(dir string) error {
	for _, path := range []string{dir, filepath.Join(dir, "events"), filepath.Join(dir, "reports")} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return fmt.Errorf("create diagnostics directory: %w", err)
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("diagnostics path must be a directory")
		}
	}
	return nil
}

func openRegular(path string, flags int) (*os.File, error) {
	if info, err := os.Lstat(path); err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return nil, fmt.Errorf("diagnostics target must be a regular file")
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return os.OpenFile(path, flags, 0o600)
}

func (r *Recorder) withGuard(fn func() error) error {
	return r.withGuardWait(0, fn)
}

func (r *Recorder) withGuardWait(wait time.Duration, fn func() error) error {
	if err := ensureDirectories(r.dir); err != nil {
		return err
	}
	guard, err := openRegular(filepath.Join(r.dir, ".guard"), os.O_CREATE|os.O_RDWR)
	if err != nil {
		return err
	}
	defer guard.Close()
	deadline := time.Now().Add(wait)
	for {
		err := tryFileLock(guard)
		if err == nil {
			break
		}
		if !errors.Is(err, errGuardBusy) || wait == 0 || !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(time.Millisecond)
	}
	defer unlockFile(guard)
	return fn()
}

func (r *Recorder) appendEventLocked(data []byte, durable bool) error {
	now := r.opts.Now().UTC()
	date := now.Format("20060102")
	capacity := r.segmentBytes()
	if r.file == nil || r.date != date || r.size+int64(len(data)) > capacity {
		if err := r.closeSegmentLocked(); err != nil {
			return err
		}
		err := r.withGuardWait(segmentGuardWait, func() error {
			// Active segment reservations must leave enough headroom for both
			// atomic report copies, rather than crowding out the latest attempt.
			if err := r.admitLocked(capacity+2*int64(r.opts.MaxReportBytes), 3, now); err != nil {
				return err
			}
			r.segment++
			name := fmt.Sprintf("%s-%s-%06d-%d.jsonl", date, r.processID, r.segment, capacity)
			file, err := openRegular(filepath.Join(r.dir, "events", name), os.O_CREATE|os.O_EXCL|os.O_RDWR)
			if err != nil {
				return err
			}
			if err := tryFileLock(file); err != nil {
				_ = file.Close()
				return err
			}
			r.file = file
			r.date = date
			r.size = 0
			return nil
		})
		if err != nil {
			return err
		}
	}
	if int64(len(data)) > capacity {
		return errQuota
	}
	n, err := r.file.Write(data)
	r.size += int64(n)
	if err != nil {
		_ = r.closeSegmentLocked()
		return err
	}
	if n != len(data) {
		_ = r.closeSegmentLocked()
		return io.ErrShortWrite
	}
	if durable {
		return r.file.Sync()
	}
	return nil
}

func (r *Recorder) closeSegmentLocked() error {
	if r.file == nil {
		return nil
	}
	file := r.file
	err := file.Sync()
	released := false
	if err == nil {
		// A zero capacity marks immutable, cleanly closed history. Serialize
		// the rename with admissions so a scanner never sees the final name
		// while this recorder can still append to it. Readers can hold the
		// already-open old file through this atomic namespace change.
		finalizeErr := r.withGuardWait(segmentGuardWait, func() error {
			// Windows writer handles do not share deletion. Close while the
			// guard still protects this namespace, then rename the final file.
			released = true
			if err := errors.Join(unlockFile(file), file.Close()); err != nil {
				return err
			}
			name := fmt.Sprintf("%s-%s-%06d-0.jsonl", r.date, r.processID, r.segment)
			return os.Rename(file.Name(), filepath.Join(r.dir, "events", name))
		})
		// A stalled guard can only postpone the optimization. Releasing the
		// ownership lock still makes the original segment reclaimable.
		if !errors.Is(finalizeErr, errGuardBusy) {
			err = finalizeErr
		}
	}
	if !released {
		err = errors.Join(err, unlockFile(file), file.Close())
	}
	r.file = nil
	r.size = 0
	return err
}

type storedFile struct {
	path              string
	size              int64
	time              time.Time
	protected, active bool
}

// Parse the same format used by eventFilename without allocating captures
// for every retained segment. The two rightmost separators delimit numeric
// segment and reservation fields; process IDs can themselves contain '-'.
func eventReservation(name string) (string, bool) {
	stem, ok := strings.CutSuffix(name, ".jsonl")
	if !ok || len(stem) < 19 || stem[8] != '-' || !decimal(stem[:8]) {
		return "", false
	}
	capacityAt := strings.LastIndexByte(stem, '-')
	segmentAt := strings.LastIndexByte(stem[:capacityAt], '-')
	if segmentAt <= 9 || capacityAt-segmentAt < 7 || !decimal(stem[segmentAt+1:capacityAt]) || !decimal(stem[capacityAt+1:]) {
		return "", false
	}
	for _, char := range []byte(stem[9:segmentAt]) {
		if !(char >= 'a' && char <= 'z') && !(char >= 'A' && char <= 'Z') && !(char >= '0' && char <= '9') && char != '_' && char != '-' {
			return "", false
		}
	}
	return stem[capacityAt+1:], true
}

func decimal(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range []byte(value) {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

// Reserve the full capacity of every locked event segment. Appends then need
// no global guard or directory scan, while peer processes cannot oversubscribe.
func (r *Recorder) scanLocked() ([]storedFile, error) {
	var files []storedFile
	scanned := 0
	for _, sub := range []string{"", "events", "reports"} {
		dir, err := os.Open(filepath.Join(r.dir, sub))
		if err != nil {
			return nil, err
		}
		for {
			entries, readErr := dir.ReadDir(64)
			for _, entry := range entries {
				scanned++
				if scanned > maxScannedEntries {
					_ = dir.Close()
					return nil, fmt.Errorf("diagnostics directory scan limit reached")
				}
				name := entry.Name()
				protected := sub == "" && name == "latest-index.json"
				var reservation string
				var event bool
				if sub == "events" {
					reservation, event = eventReservation(name)
				}
				temporary := strings.HasPrefix(name, ".tmp-") && tempFilename.MatchString(name)
				known := protected || event || (sub == "reports" && reportFilename.MatchString(name)) || temporary
				if !known || entry.Type()&os.ModeSymlink != 0 {
					continue
				}
				info, err := entry.Info()
				if err != nil {
					_ = dir.Close()
					return nil, err
				}
				if !info.Mode().IsRegular() {
					continue
				}
				path := filepath.Join(r.dir, sub, name)
				if temporary {
					// All publication temporaries are created under this guard.
					if err := os.Remove(path); err != nil {
						_ = dir.Close()
						return nil, err
					}
					continue
				}
				row := storedFile{path: path, size: info.Size(), time: info.ModTime(), protected: protected}
				if event && reservation != "0" {
					f, err := openRegular(path, os.O_RDWR)
					if err != nil {
						_ = dir.Close()
						return nil, err
					}
					lockErr := tryFileLock(f)
					if lockErr == nil {
						_ = unlockFile(f)
					}
					_ = f.Close()
					if errors.Is(lockErr, errGuardBusy) {
						row.active = true
						capacity, _ := strconv.ParseInt(reservation, 10, 64)
						if capacity <= 0 || capacity > DefaultMaxBytes {
							_ = dir.Close()
							return nil, fmt.Errorf("invalid diagnostics segment reservation")
						}
						row.size = max(row.size, capacity)
					} else if lockErr != nil {
						_ = dir.Close()
						return nil, lockErr
					}
				}
				files = append(files, row)
				if len(files) > maxStoredFiles*2 {
					_ = dir.Close()
					return nil, fmt.Errorf("diagnostics artifact scan limit reached")
				}
			}
			if readErr != nil {
				_ = dir.Close()
				if readErr == io.EOF {
					break
				}
				return nil, readErr
			}
		}
	}
	return files, nil
}

func (r *Recorder) admitLocked(bytes int64, count int, now time.Time) error {
	files, err := r.scanLocked()
	if err != nil {
		return err
	}
	total := int64(0)
	for _, file := range files {
		total += file.size
	}
	remaining := len(files)
	cutoff := now.AddDate(0, 0, -r.opts.RetentionDays)
	retained := files[:0]
	for _, file := range files {
		if file.active || file.protected {
			continue
		}
		if !file.time.Before(cutoff) {
			retained = append(retained, file)
			continue
		}
		if err := os.Remove(file.path); err != nil {
			return fmt.Errorf("prune diagnostics artifact: %w", err)
		}
		total -= file.size
		remaining--
	}
	if total+bytes <= r.opts.MaxBytes && remaining+count <= maxStoredFiles {
		return nil
	}
	// Sort only when quota pressure needs oldest-first eviction. Ordinary
	// admissions and retention cleanup do not need an ordering pass.
	sort.Slice(retained, func(i, j int) bool { return retained[i].time.Before(retained[j].time) })
	for _, file := range retained {
		if total+bytes <= r.opts.MaxBytes && remaining+count <= maxStoredFiles {
			return nil
		}
		if err := os.Remove(file.path); err != nil {
			return fmt.Errorf("prune diagnostics artifact: %w", err)
		}
		total -= file.size
		remaining--
	}
	if total+bytes > r.opts.MaxBytes || remaining+count > maxStoredFiles {
		return errQuota
	}
	return nil
}
