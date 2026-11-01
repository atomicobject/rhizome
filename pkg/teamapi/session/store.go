package session

import (
	"errors"
	"fmt"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
)

// Store keeps one opaque record per origin. Keychain implements it on macOS;
// MemoryStore serves tests.
type Store interface {
	Load(origin string) ([]byte, error)
	Save(origin string, record []byte) error
	Delete(origin string) error
}

// DefaultLockPath is <user home>/.config/rhizome/team-session.lock for the
// real OS account. It never reads HOME, so a repository dotenv file cannot
// move the lock away from the user's other processes.
func DefaultLockPath() (string, error) {
	account, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("resolve operating-system user: %w", err)
	}
	home := strings.TrimSpace(account.HomeDir)
	if home == "" || !filepath.IsAbs(home) {
		return "", errors.New("operating-system user home is unavailable")
	}
	return filepath.Join(home, ".config", "rhizome", "team-session.lock"), nil
}

// MemoryStore is an in-process Store for tests.
type MemoryStore struct {
	mu      sync.Mutex
	records map[string][]byte
}

func (m *MemoryStore) Load(origin string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	record, ok := m.records[origin]
	if !ok {
		return nil, ErrNotFound
	}
	return append([]byte(nil), record...), nil
}

func (m *MemoryStore) Save(origin string, record []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.records == nil {
		m.records = map[string][]byte{}
	}
	m.records[origin] = append([]byte(nil), record...)
	return nil
}

func (m *MemoryStore) Delete(origin string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.records, origin)
	return nil
}
