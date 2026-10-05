package sqlite

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// Two processes open a fresh vault database at once whenever a one-shot
// command auto-starts the vault runtime; every opener must find a finished
// schema rather than lose the WAL switch or replay a migration.
func TestOpenWithOptionsSerializesConcurrentFirstOpens(t *testing.T) {
	for round := 0; round < 5; round++ {
		path := filepath.Join(t.TempDir(), fmt.Sprintf("r%d", round), "db.sqlite")
		errs := make(chan error, 8)
		var wg sync.WaitGroup
		for i := 0; i < cap(errs); i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				store, err := OpenWithOptions(path, OpenOptions{})
				if err != nil {
					errs <- err
					return
				}
				_ = store.Close()
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			require.NoError(t, err, "round %d", round)
		}
	}
}
