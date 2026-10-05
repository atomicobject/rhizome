// Package namespacegit prepares the Git staging effect of admitted note moves.
// Publication, locking and recovery belong to the validation transaction owner.
package namespacegit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
)

// ErrFallback permits the caller's filesystem fallback before any live effect.
// Cancellation and malformed admission never return this classification.
var ErrFallback = errors.New("Git namespace preparation unavailable")

// Move is an already admitted canonical rename, in publication order.
type Move struct {
	Source, Destination string
	Overwrite           bool
}

// File is an original regular endpoint captured by the transaction owner.
// An absent destination has no File entry.
type File struct {
	Path    string
	Content []byte
	Mode    os.FileMode
}

// Witness records the raw live index that must be revalidated under index.lock.
type Witness struct {
	Exists bool
	Hash   string
	Mode   os.FileMode
}

// Snapshot is a standalone index preserving staging semantics. Split/sparse
// storage encoding and cache bookkeeping may differ from the raw original.
type Snapshot struct {
	Content []byte
	Hash    string
	Mode    os.FileMode
}

// Preparation contains no authority to publish or acquire a live Git lock.
// The owner durably installs these snapshots and revalidates RawOriginal before
// any authored publication. GitMoves contains only successful input pairs.
type Preparation struct {
	GitDir, IndexPath, LockPath string
	RawOriginal                 Witness
	Rollback, Candidate         Snapshot
	GitMoves                    []Move
}

// Prepare writes only below the exclusively owned scratch directory. It uses
// original endpoint snapshots, never rereading authored files. A nil result
// means the batch has no Git staging effect.
func Prepare(ctx context.Context, root, scratch string, moves []Move, originals []File) (*Preparation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := refuseGitSelectors(); err != nil {
		return nil, err
	}
	files, err := validateInputs(moves, originals)
	if err != nil {
		return nil, err
	}
	gitDir, err := bindGitDir(root)
	if err != nil {
		return nil, err
	}
	indexPath := filepath.Join(gitDir, "index")
	if err := validateScratch(scratch, gitDir); err != nil {
		return nil, err
	}
	raw, witness, err := readOriginal(indexPath)
	if err != nil {
		return nil, err
	}
	if !witness.Exists {
		return nil, nil
	}
	work, err := createShadow(ctx, scratch, files, moves)
	if err != nil {
		return nil, err
	}
	rollbackPath := filepath.Join(work, "rollback.index")
	candidatePath := filepath.Join(work, "candidate.index")
	for _, name := range []string{rollbackPath, candidatePath} {
		if err := os.WriteFile(name, raw, 0o600); err != nil {
			return nil, err
		}
	}
	git := gitCommand{dir: gitDir, work: filepath.Join(work, "tree")}
	if _, err := git.run(ctx, rollbackPath, "update-index", "--no-split-index"); err != nil {
		return nil, err
	}
	var gitMoves []Move
	for _, move := range moves {
		tracked, err := git.run(ctx, candidatePath, "ls-files", "--stage", "-z", "--", move.Source)
		if err != nil {
			return nil, err
		}
		if len(tracked) == 0 {
			if err := os.Rename(filepath.Join(git.work, filepath.FromSlash(move.Source)), filepath.Join(git.work, filepath.FromSlash(move.Destination))); err != nil {
				return nil, err
			}
			continue
		}
		args := []string{"mv"}
		if move.Overwrite {
			args = append(args, "-f")
		}
		args = append(args, "--", move.Source, move.Destination)
		if _, err := git.run(ctx, candidatePath, args...); err != nil {
			return nil, err
		}
		gitMoves = append(gitMoves, move)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(gitMoves) == 0 {
		return nil, nil
	}
	rollback, err := readSnapshot(rollbackPath, witness.Mode)
	if err != nil {
		return nil, err
	}
	candidate, err := readSnapshot(candidatePath, witness.Mode)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &Preparation{GitDir: gitDir, IndexPath: indexPath, LockPath: indexPath + ".lock", RawOriginal: witness, Rollback: rollback, Candidate: candidate, GitMoves: gitMoves}, nil
}

func validateInputs(moves []Move, originals []File) (map[string]File, error) {
	if len(moves) == 0 {
		return nil, fmt.Errorf("Git preparation requires a rename")
	}
	endpoints := make(map[string]int, 2*len(moves))
	exact := make(map[string]bool, 2*len(moves))
	for i, move := range moves {
		if move.Source == move.Destination {
			return nil, fmt.Errorf("Git move has identical endpoints")
		}
		for _, name := range []string{move.Source, move.Destination} {
			clean, err := paths.CleanRelPath(name)
			if err != nil || name == "" || clean.String() != name || strings.EqualFold(strings.Split(name, "/")[0], ".git") {
				return nil, fmt.Errorf("Git move endpoint is not a canonical authored path")
			}
			key := strings.ToLower(name)
			if previous, ok := endpoints[key]; ok && previous != i {
				return nil, fmt.Errorf("Git moves have overlapping endpoints")
			}
			endpoints[key] = i
			exact[name] = true
		}
	}
	files := make(map[string]File, len(originals))
	for _, file := range originals {
		if !exact[file.Path] || !file.Mode.IsRegular() {
			return nil, fmt.Errorf("Git original is not a regular admitted endpoint")
		}
		if _, exists := files[file.Path]; exists {
			return nil, fmt.Errorf("duplicate Git original endpoint")
		}
		files[file.Path] = file
	}
	for _, move := range moves {
		if _, exists := files[move.Source]; !exists {
			return nil, fmt.Errorf("Git original source is absent")
		}
		if _, exists := files[move.Destination]; exists && !move.Overwrite && !strings.EqualFold(move.Source, move.Destination) {
			return nil, fmt.Errorf("Git occupied destination lacks overwrite admission")
		}
	}
	return files, nil
}
