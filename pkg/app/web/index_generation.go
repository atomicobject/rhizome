package web

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	semdb "github.com/atomicobject/rhizome/pkg/store"
)

// GenerationHeader carries the index generation on view-execution, catalog,
// and invocation responses (SPEC-0136 R4).
const GenerationHeader = "Rhizome-Generation"

// indexGenerationState holds the parts of the index generation that do not
// change per request: the process epoch, the config component for servers
// without a live runtime, the validation generation memoized per writes
// component, and the connection that reads the store's data version.
type indexGenerationState struct {
	once         sync.Once
	epoch        string
	staticConfig string

	mu               sync.Mutex
	validationWrites string
	validation       string

	dataVersionMu    sync.Mutex
	dataVersionStore *semdb.Store
	dataVersion      *semdb.DataVersion
}

// IndexGeneration returns the opaque index generation
// `epoch.writes.schema.views.validation.config` (SPEC-0136 R4). Two values are
// compared for equality only. Every component is read before the caller
// executes its read, so data read afterwards is at least as new as the value.
func (s *Server) IndexGeneration(ctx context.Context) string {
	generation, _ := s.indexGeneration(ctx)
	return generation
}

// indexGeneration also reports whether reads at the generation may use the
// read cache: not while a store write is open, and not when the store's data
// version could not be read.
func (s *Server) indexGeneration(ctx context.Context) (string, bool) {
	state := &s.generation
	state.once.Do(func() {
		var epoch [6]byte
		_, _ = rand.Read(epoch[:])
		state.epoch = hex.EncodeToString(epoch[:])
		state.staticConfig = configFilesHash(s.cfg.VaultPath)
	})
	writes, cacheable := s.writesGeneration(ctx)
	return fmt.Sprintf("%s.%s.%s.%s.%s.%s", state.epoch, writes, s.schemaGeneration(), s.viewsGeneration(), s.validationGeneration(ctx, writes), s.configGeneration()), cacheable
}

// writesGeneration combines this process's write sequence with the store's
// data version, which moves for commits from other processes too (note
// operations and validation projections can write the live database). The
// sequence is read first, so a write that starts after it and commits before
// the data version is read only makes the label older than the data.
func (s *Server) writesGeneration(ctx context.Context) (string, bool) {
	sequence := semdb.WriteSequence()
	store := s.runtime.Intel()
	if store == nil {
		return fmt.Sprintf("%x-none", sequence), !semdb.WritesOpen(sequence)
	}
	version, err := s.readDataVersion(ctx, store)
	if err != nil {
		return fmt.Sprintf("%x-unwitnessed", sequence), false
	}
	return fmt.Sprintf("%x-%x", sequence, version), !semdb.WritesOpen(sequence)
}

// readDataVersion reads the data version on the connection reserved for
// store, opening it on first use and again after the runtime swaps stores.
func (s *Server) readDataVersion(ctx context.Context, store *semdb.Store) (int64, error) {
	state := &s.generation
	state.dataVersionMu.Lock()
	if state.dataVersionStore != store || state.dataVersion == nil {
		if state.dataVersion != nil {
			_ = state.dataVersion.Close()
		}
		version, err := store.OpenDataVersion(ctx)
		if err != nil {
			state.dataVersionStore, state.dataVersion = nil, nil
			state.dataVersionMu.Unlock()
			return 0, err
		}
		state.dataVersionStore, state.dataVersion = store, version
	}
	version := state.dataVersion
	state.dataVersionMu.Unlock()
	return version.Read(ctx)
}

// closeDataVersion releases the reserved connection when the server closes.
func (s *Server) closeDataVersion() error {
	state := &s.generation
	state.dataVersionMu.Lock()
	defer state.dataVersionMu.Unlock()
	if state.dataVersion == nil {
		return nil
	}
	err := state.dataVersion.Close()
	state.dataVersionStore, state.dataVersion = nil, nil
	return err
}

// setGenerationHeader writes the index generation read before the response's
// data, for handlers that answer with something other than GraphQL.
func setGenerationHeader(w http.ResponseWriter, generation string) {
	w.Header().Set(GenerationHeader, generation)
}

func (s *Server) schemaGeneration() string {
	defs, err := s.ontologyDefinitions()
	switch {
	case err != nil:
		return "error"
	case defs == nil || defs.schema == nil:
		return "none"
	}
	return shortHash(defs.schema.Hash)
}

// viewsGeneration stamps every views folder, flat and module, by name, size,
// and modification time, so any definition or view file change shows. While
// an event stream keeps the views poller running, it is the poller's latest
// stamp, so a change moves the generation when views.changed announces it.
func (s *Server) viewsGeneration() string {
	stamp := s.viewWatch.stamp.Load()
	if stamp == nil {
		whole := s.viewRootsStamp(nil)
		stamp = &whole
	}
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(*stamp))
	return fmt.Sprintf("%x", hash.Sum64())
}

// validationGeneration reads the validation run counter only when the writes
// component moved, since the counter changes only through store writes. The
// read runs without the lock; a value read after writes is at least as new
// as writes, and every later write moves writes.
func (s *Server) validationGeneration(ctx context.Context, writes string) string {
	state := &s.generation
	state.mu.Lock()
	if state.validationWrites == writes && state.validation != "" {
		value := state.validation
		state.mu.Unlock()
		return value
	}
	state.mu.Unlock()
	store := s.runtime.Intel()
	if store == nil {
		return "0"
	}
	generation, err := store.ValidationGeneration(ctx)
	if err != nil {
		return "error"
	}
	value := fmt.Sprintf("%d", generation)
	state.mu.Lock()
	state.validationWrites, state.validation = writes, value
	state.mu.Unlock()
	return value
}

func (s *Server) configGeneration() string {
	if s.runtime != nil && s.runtime.Live != nil {
		if generation := s.runtime.Live.Snapshot().ConfigGeneration; generation != "" {
			return generation
		}
	}
	// Without a live runtime nothing reloads configuration, so the files'
	// hash at startup stays accurate.
	return s.generation.staticConfig
}

func configFilesHash(vaultPath string) string {
	hash := sha256.New()
	for _, rel := range []string{".rhizome/config.yml", ".rhizome/ignore", ".obsidianignore"} {
		data, _ := os.ReadFile(filepath.Join(vaultPath, filepath.FromSlash(rel)))
		fmt.Fprintf(hash, "%s %d\n", rel, len(data))
		hash.Write(data)
	}
	return hex.EncodeToString(hash.Sum(nil))[:12]
}

func shortHash(value string) string {
	if len(value) > 12 {
		return value[:12]
	}
	if value == "" {
		return "none"
	}
	return value
}
