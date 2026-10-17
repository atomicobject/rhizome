package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	"github.com/atomicobject/rhizome/pkg/sqliteutil"
)

// Domain identifies one independently versioned embedding index.
type Domain uint8

const (
	Notes Domain = iota + 1
	Code
)

type domainSpec struct {
	domain                                                                  Domain
	meta, cache, items, chunks, itemVectors, chunkPrefix, itemPrefix, label string
	version                                                                 int
}

func specification(domain Domain) (domainSpec, error) {
	switch domain {
	case Notes:
		return domainSpec{domain: domain, meta: tableIndexMeta, cache: tableEmbeddingCache, items: tableNotes, chunks: tableChunkEmbeddings, chunkPrefix: vecChunkTablePrefix, label: "noteemb", version: noteSchemaVersion}, nil
	case Code:
		return domainSpec{domain: domain, meta: codeTableIndexMeta, cache: codeTableEmbeddingCache, items: tableItems, chunks: codeTableChunkEmbeddings, itemVectors: tableItemEmbeddings, chunkPrefix: codeVecChunkTablePrefix, itemPrefix: vecItemTablePrefix, label: "codeemb", version: codeSchemaVersion}, nil
	}
	return domainSpec{}, fmt.Errorf("invalid embedding domain: %d", domain)
}

type engine struct {
	db      *sql.DB
	runtime storeRuntime
	spec    domainSpec
	access  Access
}

// NoteStore implements the note embedding index using the shared domain engine.
type NoteStore struct{ *engine }

// CodeStore implements the code embedding index using the shared domain engine.
type CodeStore struct{ *engine }

var ErrReadOnly = errors.New("embedding index is read-only")

func (s *engine) requireWrite() error {
	if s == nil || s.access != WriteIndex {
		return ErrReadOnly
	}
	return nil
}
func (s *engine) SetWriteMu(mu *sync.Mutex) { s.runtime.SetWriteMu(mu) }
func (s *engine) withWrite(ctx context.Context, fn func(context.Context, *sql.DB) error) error {
	if err := s.requireWrite(); err != nil {
		return err
	}
	return s.runtime.WithWrite(ctx, s.db, fn)
}
func (s *engine) withWriteTx(ctx context.Context, fn func(*sql.Tx) error) error {
	if err := s.requireWrite(); err != nil {
		return err
	}
	return s.runtime.WithWriteTx(ctx, s.db, sqliteutil.ExecTxWithRetry, fn)
}
func (s *engine) Close() error {
	if s == nil {
		return nil
	}
	return s.runtime.Close(s.db)
}
func (s *engine) Dimensions() int { return int(s.runtime.Dimensions.Load()) }
