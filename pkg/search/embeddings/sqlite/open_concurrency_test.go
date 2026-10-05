package sqlite

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenWithOptionsSerializesConcurrentFirstOpens(t *testing.T) {
	for round := 0; round < 5; round++ {
		path := filepath.Join(t.TempDir(), fmt.Sprintf("r%d", round), "db.sqlite")
		errs := make(chan error, 8)
		var wg sync.WaitGroup
		for i := 0; i < cap(errs); i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				store, err := OpenWithOptions(path, 4, OpenOptions{})
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
