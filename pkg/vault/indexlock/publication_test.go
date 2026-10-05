package indexlock

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/fileio"
)

func TestLockMetadataIsNeverPartiallyPublished(t *testing.T) {
	for _, stale := range []bool{false, true} {
		name := "fresh"
		if stale {
			name = "stale"
		}
		t.Run(name, func(t *testing.T) {
			lockPath := filepath.Join(t.TempDir(), "index.lock")
			if stale {
				identity, err := runtimeIdentity()
				if err != nil {
					t.Fatal(err)
				}
				old := LockData{PID: deadPID(t), Started: time.Now().UTC().Format(time.RFC3339Nano), Runtime: identity}
				raw, err := json.Marshal(old)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(lockPath, raw, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			role := strings.Repeat("x", 8<<20)
			ready := make(chan struct{})
			observed := make(chan error, 1)
			stop := make(chan struct{})
			readerDone := make(chan struct{})
			t.Cleanup(func() { close(stop); <-readerDone })
			var once sync.Once
			go func() {
				defer close(readerDone)
				deadline := time.Now().Add(20 * time.Second)
				for time.Now().Before(deadline) {
					select {
					case <-stop:
						return
					default:
					}
					raw, err := fileio.ReadFile(lockPath)
					once.Do(func() { close(ready) })
					if os.IsNotExist(err) {
						continue
					}
					if err != nil {
						observed <- err
						return
					}
					var data LockData
					if err := json.Unmarshal(raw, &data); err != nil {
						observed <- fmt.Errorf("observed partial final lock (%d bytes): %w", len(raw), err)
						return
					}
					if data.Role == role {
						observed <- nil
						return
					}
				}
				observed <- fmt.Errorf("never observed the new owner")
			}()
			<-ready
			release, acquired, err := TryAcquireWithOptions(lockPath, AcquireOptions{Role: role})
			if err != nil || !acquired {
				t.Fatalf("acquire: err=%v acquired=%v", err, acquired)
			}
			t.Cleanup(func() {
				if release != nil {
					_ = release()
				}
			})
			if err := <-observed; err != nil {
				t.Fatal(err)
			}
			if err := release(); err != nil {
				t.Fatal(err)
			}
			release = nil
		})
	}
}
