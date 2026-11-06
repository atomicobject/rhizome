package structuralfixture

import (
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"os"
	"path/filepath"
	"sync/atomic"
)

// Reader bypasses the production vault's extension configuration, which is
// deliberately outside this provider fixture's read contract.
type Reader struct {
	*obsidian.Note
	Reads atomic.Int64
	Paths []string
}

func (r *Reader) GetContents(vault obsidian.VaultDefinition, path string) (string, error) {
	r.Reads.Add(1)
	bytes, err := os.ReadFile(filepath.Join(vault.BasePath(), path))
	return string(bytes), err
}
func (r *Reader) GetNotesList(obsidian.VaultDefinition) ([]string, error) {
	return append([]string(nil), r.Paths...), nil
}
