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
	"github.com/atomicobject/rhizome/pkg/vault/assetroots"
)

// GenerationHeader carries the index generation on view-execution, catalog,
// and invocation responses (SPEC-0136 R4).
const GenerationHeader = "Rhizome-Generation"

// indexGenerationState holds the parts of the index generation that do not
// change per request: the process epoch, the config component for servers
// without a live runtime, and the validation generation memoized per write.
type indexGenerationState struct {
	once         sync.Once
	epoch        string
	staticConfig string

	mu               sync.Mutex
	validationWrites uint64
	validation       string
	validationRead   bool
}

// IndexGeneration returns the opaque index generation
// `epoch.writes.schema.views.validation.config` (SPEC-0136 R4). Two values are
// compared for equality only. Every component is read before the caller
// executes its read, so data read afterwards is at least as new as the value.
func (s *Server) IndexGeneration(ctx context.Context) string {
	state := &s.generation
	state.once.Do(func() {
		var epoch [6]byte
		_, _ = rand.Read(epoch[:])
		state.epoch = hex.EncodeToString(epoch[:])
		state.staticConfig = configFilesHash(s.cfg.VaultPath)
	})
	writes := semdb.WriteCount()
	return fmt.Sprintf("%s.%d.%s.%s.%s.%s", state.epoch, writes, s.schemaGeneration(), s.viewsGeneration(), s.validationGeneration(ctx, writes), s.configGeneration())
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
// and modification time, so any definition or view file change shows.
// ponytail: stats every view file per call, like the views.changed poller;
// keep a watch-driven stamp if view folders grow large enough to matter.
func (s *Server) viewsGeneration() string {
	hash := fnv.New64a()
	roots, _ := assetroots.Roots(s.cfg.VaultPath, assetroots.Views)
	for _, r := range roots {
		root, err := viewRoot{module: r.Module}.open(s.cfg.VaultPath)
		if err != nil {
			continue
		}
		fmt.Fprintf(hash, "%s=%s\n", r.Rel, customViewFolderStamp(root.FS(), "."))
		_ = root.Close()
	}
	return fmt.Sprintf("%x", hash.Sum64())
}

// validationGeneration reads the validation run counter only when the store
// write count moved, since the counter changes only through store writes.
func (s *Server) validationGeneration(ctx context.Context, writes uint64) string {
	state := &s.generation
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.validationRead && state.validationWrites == writes {
		return state.validation
	}
	store := s.runtime.Intel()
	if store == nil {
		return "0"
	}
	generation, err := store.ValidationGeneration(ctx)
	if err != nil {
		return "error"
	}
	value := fmt.Sprintf("%d", generation)
	// Memoize only when no write landed during the read.
	if semdb.WriteCount() == writes {
		state.validationWrites, state.validation, state.validationRead = writes, value, true
	}
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
