package session

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/stretchr/testify/require"
)

const refreshChildEnv = "TEAMAPI_REFRESH_CHILD"

// TestMain doubles as the child process for the multi-process refresh test.
func TestMain(m *testing.M) {
	if os.Getenv(refreshChildEnv) != "1" {
		os.Exit(m.Run())
	}
	os.Exit(refreshChild())
}

// refreshChild waits for the start file, then makes one authorized request.
func refreshChild() int {
	origin, dir := os.Getenv("TEAMAPI_ORIGIN"), os.Getenv("TEAMAPI_DIR")
	a, err := Open(Options{Origin: origin, Store: fileStore{dir: filepath.Join(dir, "store")}, LockPath: filepath.Join(dir, "lock")})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	for deadline := time.Now().Add(20 * time.Second); ; time.Sleep(time.Millisecond) {
		if _, err := os.Stat(filepath.Join(dir, "go")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			fmt.Fprintln(os.Stderr, "start file never appeared")
			return 2
		}
	}
	resp, err := a.HTTPClient().Get(origin + wire.RouteSession)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "status", resp.StatusCode)
		return 1
	}
	return 0
}

func TestTwentyProcessesRefreshOnce(t *testing.T) {
	g := newFakeGateway(t)
	dir := t.TempDir()
	store := fileStore{dir: filepath.Join(dir, "store")}
	require.NoError(t, os.MkdirAll(store.dir, 0o700))
	saveRecord(t, store, g.record(time.Now().Add(30*time.Second)))

	const processes = 20
	errs := make([]error, processes)
	outputs := make([][]byte, processes)
	var wg sync.WaitGroup
	for i := range processes {
		cmd := exec.Command(os.Args[0], "-test.run=^$")
		cmd.Env = append(os.Environ(), refreshChildEnv+"=1", "TEAMAPI_ORIGIN="+g.origin(), "TEAMAPI_DIR="+dir)
		wg.Add(1)
		go func() {
			defer wg.Done()
			outputs[i], errs[i] = cmd.CombinedOutput()
		}()
	}
	time.Sleep(500 * time.Millisecond) // let every child start and wait
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go"), nil, 0o600))
	wg.Wait()
	for i := range processes {
		require.NoError(t, errs[i], string(outputs[i]))
	}
	rotations, reused, revoked := g.state()
	require.Equal(t, 1, rotations)
	require.Zero(t, reused)
	require.False(t, revoked)
	rec, ok := loadRecord(t, store, g.origin())
	require.True(t, ok)
	require.Equal(t, "rzt_a_s1.a1", rec.AccessToken)
}

func TestUnwritableStoreNeverRotates(t *testing.T) {
	g := newFakeGateway(t)
	memory := &MemoryStore{}
	saveRecord(t, memory, g.record(time.Now().Add(30*time.Second)))
	store := &countingStore{Store: memory}
	a := openAccount(t, g.origin(), store, filepath.Join(t.TempDir(), "lock"))

	resp, err := a.HTTPClient().Get(g.origin() + wire.RouteSession)
	require.NoError(t, err, "the current token still works until it expires")
	resp.Body.Close()
	rotations, _, _ := g.state()
	require.Zero(t, rotations)

	a.now = func() time.Time { return time.Now().Add(time.Minute) }
	_, err = a.refresh(context.Background(), "")
	require.ErrorIs(t, err, ErrStoreUnavailable)
	rotations, _, _ = g.state()
	require.Zero(t, rotations)
}

func TestFailedSaveAfterRotationKeepsTheNewTokens(t *testing.T) {
	g := newFakeGateway(t)
	memory := &MemoryStore{}
	saveRecord(t, memory, g.record(time.Now().Add(30*time.Second)))
	// The probe write succeeds; the save after rotation and its retry fail.
	store := &countingStore{Store: memory, failAfter: 1}
	a := openAccount(t, g.origin(), store, filepath.Join(t.TempDir(), "lock"))

	rec, err := a.current(context.Background())
	require.NoError(t, err)
	require.Equal(t, "rzt_a_s1.a1", rec.AccessToken)
	stored, _ := loadRecord(t, memory, g.origin())
	require.Equal(t, "rzt_a_s1.a0", stored.AccessToken, "the failed save left the old record")
	again, err := a.current(context.Background())
	require.NoError(t, err)
	require.Equal(t, rec.AccessToken, again.AccessToken, "this process keeps the new tokens")

	// Once the store recovers, the next refresh writes the unsaved rotation
	// before rotating, instead of presenting the retired refresh token.
	store.failAfter = -1
	a.now = func() time.Time { return time.Now().Add(10 * time.Minute) }
	_, err = a.refresh(context.Background(), "")
	require.NoError(t, err)
	rotations, reused, _ := g.state()
	require.Equal(t, 2, rotations)
	require.Zero(t, reused)
}

// countingStore lets failAfter saves succeed and fails the rest; -1 never fails.
type countingStore struct {
	Store
	failAfter int
}

func (c *countingStore) Save(origin string, record []byte) error {
	if c.failAfter == 0 {
		return errors.New("synthetic keychain write failure")
	}
	if c.failAfter > 0 {
		c.failAfter--
	}
	return c.Store.Save(origin, record)
}
