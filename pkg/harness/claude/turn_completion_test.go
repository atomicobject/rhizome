package claude

import (
	"context"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/harnesstest"
	"github.com/stretchr/testify/require"
)

// Turn completion must reach SendTurn and every concurrent Interrupt; a
// waiter that misses it would time out and stop the session.
func TestConcurrentInterruptsAllSeeTurnCompletion(t *testing.T) {
	script := harnesstest.NewScriptedIO(initResponse("init-1"))
	driver := testDriver(script)
	driver.interruptTimeout = 300 * time.Millisecond
	session, err := driver.StartSession(context.Background(), harness.SessionOptions{Cwd: "/tmp/project"})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- session.SendTurn(context.Background(), "wait") }()
	require.Eventually(t, func() bool { return script.WriteCount() >= 2 }, time.Second, time.Millisecond)
	script.Push(`{"type":"stream_event","event":{"type":"message_start"},"session_id":"sid"}`)
	awaitEvent(t, session.Events(), harness.EventTurnStarted)
	interrupted := make(chan error, 2)
	for range 2 {
		go func() { interrupted <- session.Interrupt(context.Background()) }()
	}
	require.Eventually(t, func() bool { return script.WriteCount() == 4 }, time.Second, time.Millisecond)
	script.Push(resultMessage("interrupted", false))
	require.NoError(t, <-done)
	require.NoError(t, <-interrupted)
	require.NoError(t, <-interrupted)
	require.False(t, script.Closed(), "a missed completion must not stop the session")
	require.NoError(t, session.Stop())
}
