package jsonstdio

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/stretchr/testify/require"
)

func TestProtocolFailureReturnsWhileChildKeepsStderrOpen(t *testing.T) {
	for _, mode := range []string{"malformed", "eof", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			connection, emitted := startChild(t, mode)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, err := connection.Recv(ctx)
			require.NoError(t, err)
			require.Eventually(t, func() bool { return strings.Contains(connection.tail.summary(), "synthetic diagnostic") }, time.Second, time.Millisecond)
			require.NoError(t, connection.Send(ctx, json.RawMessage(`{}`)))
			require.Eventually(t, func() bool { _, err := os.Stat(emitted); return err == nil }, time.Second, time.Millisecond)
			ctx, cancelRecv := context.WithTimeout(context.Background(), 350*time.Millisecond)
			defer cancelRecv()
			started := time.Now()
			_, err = connection.Recv(ctx)
			t.Logf("%s returned in %s", mode, time.Since(started))
			require.NoError(t, ctx.Err(), "protocol failure must not wait for stderr EOF")
			switch mode {
			case "malformed":
				require.ErrorIs(t, err, harness.ErrDecode)
			case "eof":
				require.ErrorIs(t, err, io.EOF)
			case "oversized":
				require.ErrorContains(t, err, "token too long")
			}
			require.ErrorContains(t, err, "synthetic diagnostic")
			select {
			case <-connection.Exited():
				t.Fatal("fixture exited before the protocol failure was returned")
			default:
			}
		})
	}
}

func TestCloseInterruptsBlockedWriteAndReapsChild(t *testing.T) {
	connection, _ := startChild(t, "blocked-write")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := connection.Recv(ctx)
	require.NoError(t, err)
	written := make(chan error, 1)
	go func() {
		written <- connection.Send(context.Background(), json.RawMessage(`"`+strings.Repeat("x", 1<<20)+`"`))
	}()
	select {
	case err := <-written:
		t.Fatalf("fixture accepted the oversized write: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	ctx, cancelClose := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancelClose()
	started := time.Now()
	require.NoError(t, connection.Close(ctx))
	require.Less(t, time.Since(started), 450*time.Millisecond)
	select {
	case err := <-written:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("blocked write survived Close")
	}
	select {
	case <-connection.Exited():
	default:
		t.Fatal("Close returned before reaping the child")
	}
	require.NoError(t, connection.Close(context.Background()))
}

func TestChildExitPreservesFinalFramesAndStderr(t *testing.T) {
	connection, _ := startChild(t, "frames")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for i := 0; i < 300; i++ {
		frame, err := connection.Recv(ctx)
		require.NoError(t, err)
		var value struct{ Index int }
		require.NoError(t, json.Unmarshal(frame, &value))
		require.Equal(t, i, value.Index)
		if i%32 == 0 {
			time.Sleep(time.Millisecond)
		}
	}
	_, err := connection.Recv(ctx)
	require.ErrorIs(t, err, io.EOF)
	require.NoError(t, connection.Close(ctx))
	require.Contains(t, connection.tail.summary(), "final diagnostic")
}

func TestImmediateDecodeFailureHasInitializedProcessIdentity(t *testing.T) {
	for range 10 {
		connection, _ := startChild(t, "immediate")
		connection.BindSessionID("synthetic-session")
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_, err := connection.Recv(ctx)
		cancel()
		require.ErrorIs(t, err, harness.ErrDecode)
		closeCtx, cancelClose := context.WithTimeout(context.Background(), 300*time.Millisecond)
		require.NoError(t, connection.Close(closeCtx))
		cancelClose()
	}
}

func TestSendCancellationClosesBlockedInput(t *testing.T) {
	reader, writer := io.Pipe()
	connection := New[json.RawMessage]("fixture", strings.NewReader(""), writer, writer)
	defer reader.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, connection.Send(ctx, json.RawMessage(`{}`)), context.DeadlineExceeded)
	require.NoError(t, connection.Close(context.Background()))
}

func startChild(t *testing.T, mode string) (*Transport[json.RawMessage], string) {
	t.Helper()
	binary, err := os.Executable()
	require.NoError(t, err)
	marker := filepath.Join(t.TempDir(), "emitted")
	connection, err := Start[json.RawMessage](context.Background(), "fixture", binary,
		[]string{"-test.run=^TestJSONStdioChildHelper$", "--", "jsonstdio-helper", mode, marker}, t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = connection.Close(ctx)
	})
	return connection, marker
}

func TestJSONStdioChildHelper(t *testing.T) {
	arg := slices.Index(os.Args, "jsonstdio-helper")
	if arg < 0 {
		return
	}
	mode, marker := os.Args[arg+1], os.Args[arg+2]
	if mode == "frames" {
		encoder := json.NewEncoder(os.Stdout)
		for i := 0; i < 300; i++ {
			_ = encoder.Encode(struct {
				Index   int
				Payload string
			}{i, strings.Repeat("x", 4096)})
		}
		_, _ = fmt.Fprintln(os.Stderr, "final diagnostic")
		os.Exit(0)
	}
	if mode != "immediate" {
		_, _ = fmt.Fprintln(os.Stderr, "synthetic diagnostic")
		_, _ = fmt.Fprintln(os.Stdout, `{ "ready": true }`)
		if mode != "blocked-write" {
			scanner := bufio.NewScanner(os.Stdin)
			if !scanner.Scan() {
				os.Exit(1)
			}
		}
	}
	switch mode {
	case "malformed", "immediate":
		_, _ = fmt.Fprintln(os.Stdout, "not-json")
	case "eof":
		_ = os.Stdout.Close()
	case "oversized":
		_, _ = fmt.Fprintln(os.Stdout, strings.Repeat("x", 4*1024*1024+1))
	}
	_ = os.WriteFile(marker, []byte("emitted"), 0o600)
	for {
		time.Sleep(time.Hour)
	}
}
