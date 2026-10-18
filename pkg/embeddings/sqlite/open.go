package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/mattn/go-sqlite3"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
)

// Source specifies either an owned file connection or a borrowed database handle.
type Source struct {
	file string
	db   *sql.DB
}

func File(path string) Source    { return Source{file: path} }
func Borrowed(db *sql.DB) Source { return Source{db: db} }

// Access requires an explicit read or write authority at every open.
type Access uint8

const (
	ReadExisting Access = iota + 1
	WriteIndex
)

type Options struct {
	Access     Access
	Dimensions int
	Metadata   *embeddings.IndexMetadata
	TxLockMode string
	Pool       sqliteutil.Options
}

func OpenNotes(ctx context.Context, source Source, opts Options) (*NoteStore, error) {
	e, err := openDomain(ctx, source, Notes, opts)
	if err != nil {
		return nil, err
	}
	return &NoteStore{e}, nil
}
func OpenCode(ctx context.Context, source Source, opts Options) (*CodeStore, error) {
	e, err := openDomain(ctx, source, Code, opts)
	if err != nil {
		return nil, err
	}
	return &CodeStore{e}, nil
}

// ValidateExisting checks persisted metadata without schema repair or writes.
func ValidateExisting(ctx context.Context, source Source, domain Domain, expected embeddings.IndexMetadata) error {
	e, err := openDomain(ctx, source, domain, Options{Access: ReadExisting, Dimensions: expected.Dimensions, Metadata: &expected})
	if err != nil {
		return err
	}
	return e.Close()
}
func openDomain(ctx context.Context, source Source, domain Domain, opts Options) (*engine, error) {
	if opts.Access != ReadExisting && opts.Access != WriteIndex {
		return nil, errors.New("embedding index access must be ReadExisting or WriteIndex")
	}
	if (strings.TrimSpace(source.file) == "") == (source.db == nil) {
		return nil, errors.New("exactly one embedding index file or borrowed database is required")
	}
	spec, err := specification(domain)
	if err != nil {
		return nil, err
	}
	var release func()
	if opts.Access == WriteIndex {
		if source.db != nil {
			release, err = sqliteutil.LockSchemaInitForDB(ctx, source.db)
		} else {
			release, err = sqliteutil.LockSchemaInit(ctx, source.file)
		}
		if err != nil {
			return nil, err
		}
		defer release()
	}
	db := source.db
	closeFn := func() error { return nil }
	if db == nil {
		if opts.Access == ReadExisting {
			db, err = sqliteutil.OpenDSN(sqliteutil.DSNWithOptions(source.file, sqliteutil.DSNOptions{ReadOnly: true, NoBusyWait: true}), opts.Pool)
		} else {
			if mkdirErr := os.MkdirAll(filepath.Dir(source.file), 0o755); mkdirErr != nil {
				return nil, fmt.Errorf("create index directory: %w", mkdirErr)
			}
			db, err = sqliteutil.OpenDSN(sqliteutil.DSNWithOptions(source.file, sqliteutil.DSNOptions{TxLockMode: opts.TxLockMode}), opts.Pool)
		}
		if err != nil {
			return nil, err
		}
		closeFn = db.Close
	}
	e := &engine{db: db, runtime: newStoreRuntime(map[Domain]string{Notes: "note-embeddings", Code: "code-embeddings"}[domain], closeFn), spec: spec, access: opts.Access}
	if opts.Dimensions > 0 {
		e.runtime.Dimensions.Store(int64(opts.Dimensions))
	}
	if opts.Access == WriteIndex {
		err = e.ensureSchemaWithRecovery(ctx)
	} else if opts.Metadata == nil {
		var present int
		err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", spec.meta).Scan(&present)
		if err == nil && present != 1 {
			err = fmt.Errorf("embedding index metadata table %s is missing", spec.meta)
		}
	}
	if err == nil && opts.Metadata != nil {
		meta := *opts.Metadata
		if opts.Access == ReadExisting {
			if domain == Notes {
				err = validateNoteExisting(ctx, db, meta)
			} else {
				err = validateCodeExisting(ctx, db, meta)
			}
		} else {
			err = e.ValidateOrInitMetadata(ctx, meta)
			if err != nil {
				err = embeddings.MetadataError{Err: err}
			}
		}
	}
	if err != nil {
		_ = e.Close()
		return nil, err
	}
	return e, nil
}
