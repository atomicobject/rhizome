package web

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDeletingEditSessionDuringCommitDoesNotBlockOtherSessions(t *testing.T) {
	fixture := prepareOntologyFixtureVault(t)
	srv := newFixtureServer(t, fixture, nil)
	committing, err := srv.createOntologyEditSession(time.Now(), "committing", nil, nil)
	require.NoError(t, err)
	_, err = srv.createOntologyEditSession(time.Now(), "other", nil, nil)
	require.NoError(t, err)

	// Simulate an in-flight commit holding the session's lock.
	committing.mu.Lock()
	deleted := make(chan bool, 1)
	go func() { deleted <- srv.deleteOntologyEditSession("committing") }()
	require.Eventually(t, committing.deleted.Load, time.Second, time.Millisecond)

	otherSessionsWork := make(chan error, 1)
	go func() {
		if _, ok := srv.ontologyEditSession("other"); !ok {
			otherSessionsWork <- errors.New("other session not found")
			return
		}
		_, err := srv.createOntologyEditSession(time.Now(), "fresh", nil, nil)
		otherSessionsWork <- err
	}()
	select {
	case err := <-otherSessionsWork:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		committing.mu.Unlock()
		t.Fatal("delete waiting on a committing session blocked other sessions' lookup and creation")
	}

	committing.mu.Unlock()
	require.True(t, <-deleted)
	_, ok := srv.ontologyEditSession("committing")
	require.False(t, ok)
	require.False(t, srv.deleteOntologyEditSession("committing"))
}
