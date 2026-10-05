package userstate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration"
	"github.com/atomicobject/rhizome/pkg/sqliteutil/migration/domains"
)

type sharedWriter struct {
	sync.Mutex
	references int
}

var writers = struct {
	sync.Mutex
	byPath map[string]*sharedWriter
}{byPath: make(map[string]*sharedWriter)}

// Store owns the personal-state pools. Its reader uses deferred transactions,
// while its writer takes an immediate SQLite lock before checking revisions.
type Store struct {
	vaultPath string
	dbPath    string
	db        *sql.DB
	reader    *sql.DB
	writeMu   *sharedWriter
	closeOnce sync.Once
	closeErr  error
}

func retainWriter(path string) *sharedWriter {
	writers.Lock()
	defer writers.Unlock()
	mu := writers.byPath[path]
	if mu == nil {
		mu = &sharedWriter{}
		writers.byPath[path] = mu
	}
	mu.references++
	return mu
}

func releaseWriter(path string, mu *sharedWriter) {
	writers.Lock()
	defer writers.Unlock()
	mu.references--
	if mu.references == 0 {
		delete(writers.byPath, path)
	}
}

func Open(ctx context.Context, vaultPath string) (*Store, error) {
	if vaultPath == "" {
		return nil, errors.New("user-state vault path is required")
	}
	dbPath := databasePath(vaultPath)
	release, err := sqliteutil.LockSchemaInit(ctx, dbPath)
	if err != nil {
		return nil, err
	}
	defer release()
	dbPath = paths.ResolveSymlinks(dbPath).String()
	mu := retainWriter(dbPath)
	ready := false
	defer func() {
		if !ready {
			releaseWriter(dbPath, mu)
		}
	}()
	db, err := sqliteutil.OpenDSN(sqliteutil.DSNWithOptions(dbPath, sqliteutil.DSNOptions{TxLockMode: sqliteutil.TxLockImmediate}), sqliteutil.Options{MaxOpenConns: 1, MaxIdleConns: 1})
	if err != nil {
		return nil, err
	}
	defer func() {
		if !ready {
			_ = db.Close()
		}
	}()
	mu.Lock()
	err = migration.EnsureDomain(ctx, db, domains.UserStatePlan(), migration.EnsureOptions{})
	mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("user-state schema: %w", err)
	}
	reader, err := sqliteutil.OpenDSN(sqliteutil.DSNWithOptions(dbPath, sqliteutil.DSNOptions{ReadOnly: true}), sqliteutil.Options{})
	if err != nil {
		return nil, err
	}
	ready = true
	return &Store{vaultPath: vaultPath, dbPath: dbPath, db: db, reader: reader, writeMu: mu}, nil
}

func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		s.closeErr = errors.Join(s.reader.Close(), s.db.Close())
		releaseWriter(s.dbPath, s.writeMu)
	})
	return s.closeErr
}

func (s *Store) withWrite(ctx context.Context, fn func(*sql.Tx) error) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return sqliteutil.ExecTxWithRetry(ctx, s.db, fn)
}
