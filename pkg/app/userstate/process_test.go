package userstate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

// Separate processes cannot share the in-memory write mutex. Their revision
// check must still happen after acquiring SQLite's immediate writer lock.
func TestUserStateWriterProcessesRejectOneStaleWrite(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	type outcome struct {
		err    error
		output []byte
	}
	results := make(chan outcome, 2)
	for _, key := range []string{"density", "sort"} {
		go func() {
			command := exec.Command(os.Args[0], "-test.run=^TestUserStateWriterProcessHelper$", "--", root, key)
			output, err := command.CombinedOutput()
			results <- outcome{err, output}
		}()
	}
	successes, conflicts := 0, 0
	for range 2 {
		result := <-results
		if result.err == nil {
			successes++
			continue
		}
		var exit *exec.ExitError
		require.ErrorAs(t, result.err, &exit, string(result.output))
		require.Equal(t, 3, exit.ExitCode(), string(result.output))
		conflicts++
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)
	store := openTestStore(t, root)
	snapshot, err := store.Read(context.Background(), testScope("work"))
	require.NoError(t, err)
	require.EqualValues(t, 1, snapshot.Revision)
	require.Len(t, snapshot.Values, 1)
}

func TestUserStateWriterProcessHelper(t *testing.T) {
	argument := -1
	for i, value := range os.Args {
		if value == "--" {
			argument = i
			break
		}
	}
	if argument < 0 || len(os.Args) != argument+3 {
		t.Skip("subprocess helper")
	}
	store, err := Open(context.Background(), os.Args[argument+1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_, err = store.Patch(context.Background(), testScope("work"), 0, map[string]json.RawMessage{os.Args[argument+2]: json.RawMessage(`true`)}, nil)
	closeErr := store.Close()
	if closeErr != nil {
		fmt.Fprintln(os.Stderr, closeErr)
		os.Exit(1)
	}
	var conflict *ConflictError
	if errors.As(err, &conflict) {
		os.Exit(3)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
