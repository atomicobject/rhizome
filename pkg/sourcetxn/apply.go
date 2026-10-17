package sourcetxn

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
)

var ErrSourceChanged = errors.New("source changed")

type SourceConflictError struct {
	Path string
	Err  error
}

func (e *SourceConflictError) Error() string {
	return fmt.Sprintf("source changed at %s: %v", e.Path, e.Err)
}
func (e *SourceConflictError) Unwrap() error { return errors.Join(ErrSourceChanged, e.Err) }
func ConflictPath(err error) string {
	var conflict *SourceConflictError
	if errors.As(err, &conflict) {
		return conflict.Path
	}
	return ""
}

// RecoveryRequiredError means a failed commit could not be fully undone. Its
// sources and journal must remain visible to recovery, even for a conflict.
type RecoveryRequiredError struct{ CommitErr, RecoveryErr error }

func (e *RecoveryRequiredError) Error() string {
	return fmt.Sprintf("source transaction requires recovery: %v; %v", e.CommitErr, e.RecoveryErr)
}
func (e *RecoveryRequiredError) Unwrap() error { return errors.Join(e.CommitErr, e.RecoveryErr) }

type CommittedError struct{ Err error }

func (e *CommittedError) Error() string {
	return "source committed with pending completion: " + e.Err.Error()
}
func (e *CommittedError) Unwrap() error { return e.Err }

type FilePatch struct {
	Path         string
	Original     []byte `json:"-"`
	OriginalMode os.FileMode
	Updated      []byte `json:"-"`
}
type Intent struct {
	TransactionID string
	Fingerprint   string
	Files         []FilePatch
}

// Apply publishes a source-only intent through this writer's classified vault.
func (w *Writer) Apply(ctx context.Context, intent Intent) error {
	if err := w.check(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if intent.Fingerprint == "" {
		return fmt.Errorf("source intent fingerprint is required")
	}
	files := append([]FilePatch(nil), intent.Files...)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	affected := make([]string, 0, len(files))
	states := make([]fileState, 0, len(files))
	seen := map[string]bool{}
	var identities []os.FileInfo
	run := w.run
	for _, file := range files {
		canonical, pathErr := canonicalRepairPath(file.Path)
		if pathErr != nil || canonical == "" || canonical != file.Path {
			return fmt.Errorf("source path is not canonical: %s", file.Path)
		}
		key := paths.CaseKey(file.Path)
		if seen[key] {
			return fmt.Errorf("source intent has duplicate path %s", file.Path)
		}
		seen[key] = true
		absolute, pathErr := repairAbsPath(run, file.Path)
		if pathErr != nil {
			return pathErr
		}
		info, statErr := os.Lstat(absolute)
		if statErr != nil {
			return &SourceConflictError{Path: file.Path, Err: statErr}
		}
		for _, previous := range identities {
			if os.SameFile(previous, info) {
				return fmt.Errorf("source intent has duplicate filesystem path %s", file.Path)
			}
		}
		identities = append(identities, info)
		if err := validateRepairFileSnapshot(absolute, file.Path, true, Hash(file.Original), RepairModeBits(file.OriginalMode)); err != nil {
			return &SourceConflictError{Path: file.Path, Err: err}
		}
		if bytes.Equal(file.Original, file.Updated) {
			continue
		}
		affected = append(affected, file.Path)
		states = append(states, fileState{Path: file.Path, Absolute: absolute, OriginalPath: file.Path, OriginalExists: true, OriginalContent: file.Original, OriginalMode: file.OriginalMode, FinalExists: true, FinalContent: file.Updated, FinalMode: file.OriginalMode})
	}
	if len(states) == 0 {
		return nil
	}
	id := intent.TransactionID
	if strings.TrimSpace(id) == "" {
		id = "source:" + Hash([]byte(intent.Fingerprint+"\x00"+strings.Join(affected, "\x00")))
	}
	request := RepairRequest{TransactionID: id, Fingerprint: intent.Fingerprint, Changed: affected}
	for _, state := range states {
		request.Files = append(request.Files, Change{Path: state.Path, OriginalPath: state.OriginalPath, Before: Snapshot{Exists: state.OriginalExists, Content: state.OriginalContent, Mode: state.OriginalMode}, After: Snapshot{Exists: state.FinalExists, Content: state.FinalContent, Mode: state.FinalMode}})
	}
	t, err := w.prepare(request, nil, true)
	if err != nil {
		return err
	}
	completion, err := w.Publish(t)
	if err != nil {
		return err
	}
	if err := w.Complete(completion); err != nil {
		return &CommittedError{Err: err}
	}
	return nil
}
