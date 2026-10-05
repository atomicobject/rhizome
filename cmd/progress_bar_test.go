package cmd

import (
	"bytes"
	"context"
	"io"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/diagnostics"
	"github.com/atomicobject/rhizome/pkg/logging"
	"github.com/stretchr/testify/require"
)

func TestProgressLoggingPreservesDiagnosticBridgeAndConsoleWarning(t *testing.T) {
	root := t.TempDir()
	recorder, err := diagnostics.Open(root, diagnostics.Options{Stderr: io.Discard})
	require.NoError(t, err)
	defer func() { require.NoError(t, recorder.Close()) }()
	restoreStandard := logging.InstallStandard(diagnostics.WithRecorder(context.Background(), recorder))
	defer restoreStandard()
	var out bytes.Buffer
	bar := newProgressBar(&out)
	restoreProgress := withProgressLogOutput(bar, &out, false)
	defer restoreProgress()
	log.Print("Warning: background indexing unavailable; retry indexing")
	require.Contains(t, out.String(), "retry indexing")
	events, err := diagnostics.ReadEvents(root, diagnostics.Filter{Limit: 100})
	require.NoError(t, err)
	require.Len(t, events.Events, 1)
	require.Equal(t, "legacy.log", events.Events[0].Name)
}

func TestProgressBarUpdateNonTTYLogsMilestones(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	bar := newProgressBar(&out)
	bar.Update(0, 100)
	bar.Update(1, 100)
	bar.Update(5, 100)
	bar.Update(10, 100)
	bar.Update(100, 100)

	text := out.String()
	require.Contains(t, text, "Indexing 0/100 (0%)")
	require.Contains(t, text, "Indexing 5/100 (5%)")
	require.Contains(t, text, "Indexing 10/100 (10%)")
	require.Contains(t, text, "Indexing 100/100 (100%)")
}

func TestProgressBarUpdateNonTTYDoesNotSpamGrowingZeroProgressTotals(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	bar := newProgressBar(&out)
	bar.Update(0, 1)
	bar.Update(0, 2)
	bar.Update(0, 3)

	require.Equal(t, "Indexing 0/1 (0%)\n", out.String())
}

func TestProgressBarNormalizesLegacyOntologyLabel(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	bar := newProgressBar(&out)
	bar.SetLabel("Refreshing ontology")
	bar.Update(5, 100)

	require.Contains(t, out.String(), "Ontology graph refresh 5/100 (5%)")
	require.NotContains(t, out.String(), "Refreshing ontology")
}

type fakeTerminalWriter struct {
	io.Writer
}

func (fakeTerminalWriter) IsTerminal() bool { return true }

func TestPreferredProgressWriterPrefersFallbackTerminal(t *testing.T) {
	t.Parallel()

	var primary bytes.Buffer
	fallback := fakeTerminalWriter{Writer: &bytes.Buffer{}}
	require.Equal(t, fallback, preferredProgressWriter(&primary, fallback))
}

func TestNewCLIProgressBarUsesOpenedTTY(t *testing.T) {
	oldOpenTTY := openProgressTTY
	t.Cleanup(func() { openProgressTTY = oldOpenTTY })

	tty, err := os.CreateTemp(t.TempDir(), "tty-*")
	require.NoError(t, err)

	openProgressTTY = func() *os.File { return tty }
	bar := newCLIProgressBar(&bytes.Buffer{})
	require.NotNil(t, bar)
	require.Same(t, tty, bar.out)
	require.Same(t, tty, bar.ownedTTY)
	require.True(t, bar.tty)

	bar.Close()
	_, err = tty.WriteString("closed")
	require.Error(t, err)
}

func TestProgressAwareWriterRestoresBarAfterLogLine(t *testing.T) {
	t.Parallel()

	var raw bytes.Buffer
	tty := fakeTerminalWriter{Writer: &raw}
	bar := newProgressBarWithLabel(tty, "Indexing")
	bar.Update(5, 10)
	first := raw.String()
	bar.Update(6, 10)
	require.Equal(t, first, raw.String(), "intermediate TTY updates are throttled")

	_, err := bar.WrapWriter(tty).Write([]byte("log line\n"))
	require.NoError(t, err)

	text := raw.String()
	require.Contains(t, text, "log line\n")
	require.GreaterOrEqual(t, strings.Count(text, "Indexing ["), 2)
	bar.Update(10, 10)
	require.Contains(t, raw.String(), "100%")
}

func TestProgressBarUpdatePercentOnly(t *testing.T) {
	var out bytes.Buffer
	bar := newProgressBar(&out)
	bar.showCounts = false
	bar.Update(5, 10)

	require.Contains(t, out.String(), "50%")
	require.NotContains(t, out.String(), "5/10")
}

func TestProgressLogFilterWriterKeepsFailuresWhileDroppingNoise(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	w := &progressLogFilterWriter{out: &out}
	msg := "" +
		"[csharp index] file=foo.cs symbols=3\n" +
		"RebuildCallEdgesForPaths: processing 820 files\n" +
		"RebuildCallEdgesForPaths: 200/820 files processed\n" +
		"RebuildCallEdgesForPaths: failed for foo.go: boom\n" +
		"[index] intel-store writes=200 wait=71ms\n" +
		"Warning: something degraded\n"

	n, err := w.Write([]byte(msg))
	require.NoError(t, err)
	require.Equal(t, len(msg), n)
	text := out.String()
	require.NotContains(t, text, "[csharp index]")
	require.NotContains(t, text, "200/820 files processed")
	require.NotContains(t, text, "processing 820 files")
	require.NotContains(t, text, "writes=200")
	require.Contains(t, text, "failed for foo.go: boom")
	require.Contains(t, text, "Warning: something degraded")
}

type partialProgressWriter struct {
	bytes.Buffer
	limit int
	err   error
}

func (w *partialProgressWriter) Write(p []byte) (int, error) {
	n, _ := w.Buffer.Write(p[:min(w.limit, len(p))])
	return n, w.err
}

func TestProgressLogFilterWriterReportsPartialWrites(t *testing.T) {
	t.Parallel()
	noise := "[csharp index] details\n"
	for _, tc := range []struct {
		name, input string
		limit, want int
		err         error
	}{
		{name: "retained line", input: "Warning: keep\n", limit: 2, want: 2, err: io.ErrClosedPipe},
		{name: "suppressed prefix", input: noise + "Warning: keep\n", limit: 2, want: len(noise) + 2, err: io.ErrClosedPipe},
		{name: "suppressed middle", input: "first\n" + noise + "Warning: keep\n", limit: 8, want: 8 + len(noise), err: io.ErrClosedPipe},
		{name: "line boundary", input: "first\n" + noise + "Warning: keep\n", limit: 6, want: 6 + len(noise), err: io.ErrClosedPipe},
		{name: "no final newline", input: "Warning: keep", limit: 2, want: 2, err: io.ErrClosedPipe},
		{name: "short write without error", input: "Warning: keep\n", limit: 2, want: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := &partialProgressWriter{limit: tc.limit, err: tc.err}
			writer := &progressLogFilterWriter{out: out}
			n, err := writer.Write([]byte(tc.input))
			require.Equal(t, tc.want, n)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.ErrorIs(t, err, io.ErrShortWrite)
			}
			out.limit, out.err = len(tc.input), nil
			retried, err := writer.Write([]byte(tc.input[n:]))
			require.NoError(t, err)
			require.Equal(t, len(tc.input)-n, retried)
			require.Equal(t, strings.ReplaceAll(tc.input, noise, ""), out.String())
		})
	}
}

func TestProgressLogFilterWriterReportsFullyConsumedInput(t *testing.T) {
	t.Parallel()
	noise := "[csharp index] details\n"
	for _, tc := range []struct {
		name, input, output string
		err                 error
	}{
		{name: "all suppressed", input: noise + noise},
		{name: "full write with error", input: noise + "Warning: keep\n" + noise, output: "Warning: keep\n", err: io.ErrClosedPipe},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := &partialProgressWriter{limit: len(tc.input), err: io.ErrClosedPipe}
			writer := &progressLogFilterWriter{out: out}
			n, err := writer.Write([]byte(tc.input))
			require.Equal(t, len(tc.input), n)
			require.ErrorIs(t, err, tc.err)
			require.Equal(t, tc.output, out.String())
		})
	}
}
