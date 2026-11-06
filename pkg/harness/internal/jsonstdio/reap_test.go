package jsonstdio

import (
	"context"
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCloseReapsChildWhileExitLogIsBlocked(t *testing.T) {
	connection, _ := startChild(t, "immediate")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := connection.Recv(ctx)
	require.Error(t, err)

	output := &blockedExitLog{writer: log.Writer(), correlation: connection.corr, entered: make(chan struct{}), release: make(chan struct{})}
	log.SetOutput(output)
	defer func() {
		close(output.release)
		log.SetOutput(output.writer)
	}()
	closed := make(chan error, 1)
	go func() {
		closeCtx, cancelClose := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancelClose()
		closed <- connection.Close(closeCtx)
	}()
	select {
	case <-output.entered:
	case <-time.After(time.Second):
		t.Fatal("child did not reach its exit log")
	}
	select {
	case err := <-closed:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Close waited for the exit log")
	}
	select {
	case <-connection.Exited():
	default:
		t.Fatal("Close returned before reporting the reaped child")
	}
}

type blockedExitLog struct {
	writer      io.Writer
	correlation string
	entered     chan struct{}
	release     chan struct{}
}

func (w *blockedExitLog) Write(data []byte) (int, error) {
	if strings.Contains(string(data), "harness fixture exit") && strings.Contains(string(data), w.correlation) {
		close(w.entered)
		<-w.release
	}
	return w.writer.Write(data)
}
