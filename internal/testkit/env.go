package testkit

import (
	"github.com/atomicobject/rhizome/pkg/vault/config"
	"path/filepath"
	"testing"
)

// Env gives each test independent host paths and no inherited credentials.
func Env(t testing.TB) config.Env {
	t.Helper()
	root := t.TempDir()
	return config.Env{HomeDir: root, ConfigDir: filepath.Join(root, "config"), WorkingDir: root}
}
