package agentchat

import (
	"context"
	"database/sql"
	"sync"

	"github.com/atomicobject/rhizome/pkg/sqliteutil"
)

type chatWriteLock struct {
	sync.Mutex
	references int
}

var chatWriters = struct {
	sync.Mutex
	byPath map[string]*chatWriteLock
}{byPath: make(map[string]*chatWriteLock)}

func retainChatWriter(path string) *chatWriteLock {
	chatWriters.Lock()
	defer chatWriters.Unlock()
	mu := chatWriters.byPath[path]
	if mu == nil {
		mu = &chatWriteLock{}
		chatWriters.byPath[path] = mu
	}
	mu.references++
	return mu
}

func releaseChatWriter(path string, mu *chatWriteLock) {
	chatWriters.Lock()
	defer chatWriters.Unlock()
	mu.references--
	if mu.references == 0 {
		delete(chatWriters.byPath, path)
	}
}

func (s *Store) execWrite(ctx context.Context, query string, args ...any) (sql.Result, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var result sql.Result
	err := sqliteutil.ExecTxWithRetry(ctx, s.db, func(tx *sql.Tx) error {
		var err error
		result, err = tx.ExecContext(ctx, query, args...)
		return err
	})
	return result, err
}
