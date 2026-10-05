//go:build unix

package actions_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/coderefs"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

type namespaceOptionalResult struct {
	mutation validate.NamespaceMutationResult
	updates  int
	err      error
}

type namespaceStartedVault struct {
	namespaceVault
	started chan struct{}
	once    sync.Once
}

func (v *namespaceStartedVault) Path() (string, error) {
	v.once.Do(func() { close(v.started) })
	return v.path, nil
}

func TestNamespaceOptionalCodeWritesFollowCommitOrder(t *testing.T) {
	for _, route := range []string{"rename", "move"} {
		t.Run(route, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, "A.md"), []byte("# A\n"), 0o644))
			require.NoError(t, os.WriteFile(filepath.Join(root, "Ref.md"), []byte("[[A]]\n"), 0o644))
			codePath := filepath.Join(root, "sample.go")
			require.NoError(t, os.WriteFile(codePath, []byte("package sample\n// [[A]]\n"), 0o644))
			gatePath := filepath.Join(root, "gate.refs")
			require.NoError(t, unix.Mkfifo(gatePath, 0o600))
			config := coderefs.NewConfig(true, []string{"gate.refs", "sample.go"}, nil)
			metadata, refresher := namespaceTestMetadata(t), namespaceTestRefresher(t, root)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			run := func(vault obsidian.VaultManager, from, to string) namespaceOptionalResult {
				if route == "rename" {
					result, err := actions.RenameNote(vault, actions.RenameParams{
						Context: ctx, NoteMetadata: metadata, PostApplyRefresher: refresher,
						Source: from, Target: to, UpdateBacklinks: true, CodeRefConfig: config,
					})
					return namespaceOptionalResult{result.Mutation, result.CodeRefUpdates, err}
				}
				result, err := actions.MoveNotes(vault, nil, actions.MoveParams{
					Context: ctx, NoteMetadata: metadata, PostApplyRefresher: refresher,
					Moves: []actions.MoveRequest{{Source: from, Target: to}}, UpdateBacklinks: true, CodeRefConfig: config,
				})
				return namespaceOptionalResult{result.Mutation, result.TotalCodeRefUpdates, err}
			}
			firstDone := make(chan namespaceOptionalResult, 1)
			var secondDone chan namespaceOptionalResult
			var writer *os.File
			firstReturned, secondReturned := false, false
			defer func() {
				if writer != nil {
					_ = writer.Close()
				}
				cancel()
				for _, pending := range []struct {
					done     chan namespaceOptionalResult
					returned bool
				}{{firstDone, firstReturned}, {secondDone, secondReturned}} {
					if pending.done != nil && !pending.returned {
						select {
						case <-pending.done:
						case <-time.After(10 * time.Second):
							t.Error("namespace request did not finish during cleanup")
						}
					}
				}
			}()
			go func() { firstDone <- run(namespaceVault{path: root}, "A.md", "B.md") }()
			// A nonblocking FIFO writer opens only after the real optional read
			// has opened its reader. Hold EOF to pause before sample.go is read.
			require.Eventually(t, func() bool {
				fd, err := unix.Open(gatePath, unix.O_WRONLY|unix.O_NONBLOCK, 0)
				if err != nil {
					return false
				}
				writer = os.NewFile(uintptr(fd), gatePath)
				return true
			}, 10*time.Second, 10*time.Millisecond)
			require.FileExists(t, filepath.Join(root, "B.md"))
			require.NoFileExists(t, filepath.Join(root, "A.md"))
			// Move the gate outside the configured glob while its first reader
			// stays open, so the second request will read only the actual code.
			require.NoError(t, os.Rename(gatePath, filepath.Join(root, "completed-gate")))
			release, acquired, err := indexlock.TryAcquire(filepath.Join(root, ".rhizome", "index.lock"))
			require.NoError(t, err)
			if acquired {
				require.NoError(t, release())
			}
			assert.False(t, acquired, "optional read/write must retain namespace exclusion")
			started := make(chan struct{})
			secondDone = make(chan namespaceOptionalResult, 1)
			go func() {
				secondDone <- run(&namespaceStartedVault{namespaceVault: namespaceVault{path: root}, started: started}, "B.md", "C.md")
			}()
			select {
			case <-started:
			case <-ctx.Done():
				t.Fatal("second request did not start")
			}
			var second namespaceOptionalResult
			if acquired {
				// The broken owner allows the successor to finish first. Observe
				// that real completion before releasing the earlier optional read.
				select {
				case second = <-secondDone:
					secondReturned = true
				case <-ctx.Done():
					t.Fatal("successor did not finish with the lease available")
				}
			} else {
				require.NoFileExists(t, filepath.Join(root, "C.md"))
			}
			require.NoError(t, writer.Close())
			writer = nil
			var first namespaceOptionalResult
			select {
			case first = <-firstDone:
				firstReturned = true
			case <-ctx.Done():
				t.Fatal("first request did not finish")
			}
			if !secondReturned {
				select {
				case second = <-secondDone:
					secondReturned = true
				case <-ctx.Done():
					t.Fatal("second request did not finish")
				}
			}
			for _, result := range []namespaceOptionalResult{first, second} {
				require.NoError(t, result.err)
				require.Equal(t, validate.NamespaceCommitted, result.mutation.Current.Decision)
				require.False(t, result.mutation.Current.RecoveryPending)
				assert.Equal(t, 1, result.updates)
			}
			require.FileExists(t, filepath.Join(root, "C.md"))
			code, err := os.ReadFile(codePath)
			require.NoError(t, err)
			require.Equal(t, "package sample\n// [[C]]\n", string(code))
			ref, err := os.ReadFile(filepath.Join(root, "Ref.md"))
			require.NoError(t, err)
			require.Equal(t, "[[C]]\n", string(ref))
		})
	}
}

func TestNamespaceOptionalCodeWriteFailureKeepsRequiredCommit(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires ordinary user write permissions")
	}
	for _, route := range []string{"rename", "move"} {
		t.Run(route, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, "A.md"), []byte("# A\n"), 0o644))
			require.NoError(t, os.WriteFile(filepath.Join(root, "Ref.md"), []byte("[[A]]\n"), 0o644))
			codePath := filepath.Join(root, "sample.go")
			require.NoError(t, os.WriteFile(codePath, []byte("package sample\n// [[A]]\n"), 0o444))
			metadata, refresher := namespaceTestMetadata(t), namespaceTestRefresher(t, root)
			config := coderefs.NewConfig(true, []string{"*.go"}, nil)
			var mutation validate.NamespaceMutationResult
			var updates int
			if route == "rename" {
				result, err := actions.RenameNote(namespaceVault{path: root}, actions.RenameParams{
					NoteMetadata: metadata, PostApplyRefresher: refresher, Source: "A.md", Target: "B.md",
					UpdateBacklinks: true, CodeRefConfig: config,
				})
				require.NoError(t, err)
				mutation, updates = result.Mutation, result.CodeRefUpdates
			} else {
				result, err := actions.MoveNotes(namespaceVault{path: root}, nil, actions.MoveParams{
					NoteMetadata: metadata, PostApplyRefresher: refresher, Moves: []actions.MoveRequest{{Source: "A.md", Target: "B.md"}},
					UpdateBacklinks: true, CodeRefConfig: config,
				})
				require.NoError(t, err)
				mutation, updates = result.Mutation, result.TotalCodeRefUpdates
			}
			require.Equal(t, validate.NamespaceCommitted, mutation.Current.Decision)
			require.False(t, mutation.Current.RecoveryPending)
			require.Zero(t, updates)
			code, err := os.ReadFile(codePath)
			require.NoError(t, err)
			require.Equal(t, "package sample\n// [[A]]\n", string(code))
			ref, err := os.ReadFile(filepath.Join(root, "Ref.md"))
			require.NoError(t, err)
			require.Equal(t, "[[B]]\n", string(ref))
			next, err := actions.MoveNotes(namespaceVault{path: root}, nil, actions.MoveParams{
				NoteMetadata: metadata, PostApplyRefresher: refresher, Moves: []actions.MoveRequest{{Source: "B.md", Target: "C.md"}}, UpdateBacklinks: true,
			})
			require.NoError(t, err)
			require.Equal(t, validate.NamespaceCommitted, next.Mutation.Current.Decision)
			require.Empty(t, next.Mutation.Recovered)
			require.FileExists(t, filepath.Join(root, "C.md"))
		})
	}
}
