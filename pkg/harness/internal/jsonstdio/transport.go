package jsonstdio

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/internal/processlog"
)

const forceKillAfter = 2 * time.Second

// killWaitFloor bounds the wait for a killed child to be reaped.
const killWaitFloor = time.Second

type recvResult[M any] struct {
	message M
	err     error
}

// Transport owns typed newline-delimited JSON and one session child process.
type Transport[M any] struct {
	stdin      io.Writer
	closer     io.Closer
	recv       chan recvResult[M]
	done       chan struct{}
	process    *os.Process
	stdout     io.Closer
	stderr     io.Closer
	vendor     string
	tail       stderrTail
	stderrDone chan struct{}
	exited     chan struct{}
	corr       string

	writeLock  chan struct{}
	inputOnce  sync.Once
	closeMu    sync.Mutex
	closing    bool
	closeDone  chan struct{}
	closeErr   error
	bindOnce   sync.Once
	identityMu sync.Mutex
	sessionID  string
}

// Start inherits the caller's environment and owns the child until Close.
func Start[M any](ctx context.Context, vendor, binary string, args []string, cwd string) (*Transport[M], error) {
	if err := ctx.Err(); err != nil {
		return nil, harness.Phase(harness.ErrSpawn, err)
	}
	cmd := exec.Command(binary, args...)
	cmd.Dir, cmd.Env = cwd, os.Environ()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, harness.Phase(harness.ErrSpawn, err)
	}
	stdout, stdoutWriter, err := os.Pipe()
	if err != nil {
		_ = stdin.Close()
		return nil, harness.Phase(harness.ErrSpawn, err)
	}
	defer stdoutWriter.Close()
	stderr, stderrWriter, err := os.Pipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, harness.Phase(harness.ErrSpawn, err)
	}
	defer stderrWriter.Close()
	// Wait must not close readers before buffered terminal frames are scanned.
	cmd.Stdout, cmd.Stderr = stdoutWriter, stderrWriter
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		return nil, harness.Phase(harness.ErrSpawn, err)
	}
	_ = stdoutWriter.Close()
	_ = stderrWriter.Close()
	t := newTransport[M](vendor, stdin, stdin)
	t.process, t.stdout, t.stderr = cmd.Process, stdout, stderr
	t.exited, t.stderrDone = make(chan struct{}), make(chan struct{})
	processlog.Spawn(vendor, binary, cwd, t.corr, cmd.Process.Pid)
	go t.scan(stdout)
	go func() {
		defer close(t.stderrDone)
		t.drainStderr(stderr)
	}()
	go func() {
		err := cmd.Wait()
		processlog.Exit(vendor, t.corr, cmd.Process.Pid, cmd.ProcessState, err)
		close(t.exited)
	}()
	return t, nil
}

// New connects a typed JSON stream without spawning a process.
func New[M any](vendor string, reader io.Reader, writer io.Writer, closer io.Closer) *Transport[M] {
	t := newTransport[M](vendor, writer, closer)
	go t.scan(reader)
	return t
}

func newTransport[M any](vendor string, writer io.Writer, closer io.Closer) *Transport[M] {
	t := &Transport[M]{
		stdin: writer, closer: closer, vendor: vendor, corr: processlog.CorrelationID(),
		recv: make(chan recvResult[M], 64), done: make(chan struct{}),
		writeLock: make(chan struct{}, 1), closeDone: make(chan struct{}),
	}
	t.writeLock <- struct{}{}
	return t
}

// Exited closes after the child has been reaped; it is nil for New streams.
func (t *Transport[M]) Exited() <-chan struct{} { return t.exited }

func (t *Transport[M]) Send(ctx context.Context, message M) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	encoded, err := json.Marshal(message)
	if err != nil {
		return harness.Phase(harness.ErrDecode, err)
	}
	encoded = append(encoded, '\n')
	select {
	case <-t.writeLock:
		defer func() { t.writeLock <- struct{}{} }()
	case <-ctx.Done():
		return ctx.Err()
	case <-t.done:
		return io.ErrClosedPipe
	}
	t.closeMu.Lock()
	closed := t.closing
	t.closeMu.Unlock()
	if closed {
		return io.ErrClosedPipe
	}
	written := make(chan error, 1)
	go func() {
		_, writeErr := t.stdin.Write(encoded)
		written <- writeErr
	}()
	select {
	case err := <-written:
		return err
	case <-ctx.Done():
		t.closeInput()
		return ctx.Err()
	case <-t.done:
		t.closeInput()
		return io.ErrClosedPipe
	}
}

func (t *Transport[M]) Recv(ctx context.Context) (M, error) {
	var zero M
	select {
	case result, ok := <-t.recv:
		if !ok {
			return zero, t.decorate(io.EOF)
		}
		// Session failure owns termination. Stderr may remain open until Close.
		return result.message, t.decorate(result.err)
	case <-ctx.Done():
		return zero, ctx.Err()
	}
}

func (t *Transport[M]) scan(reader io.Reader) {
	defer close(t.recv)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var message M
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			decodeErr := harness.Phase(harness.ErrDecode, fmt.Errorf("%w: %q", err, scanner.Text()))
			processlog.DecodeFailure(t.vendor, t.corr, t.boundSessionID(), decodeErr)
			select {
			case t.recv <- recvResult[M]{err: decodeErr}:
			case <-t.done:
			}
			return
		}
		select {
		case t.recv <- recvResult[M]{message: message}:
		case <-t.done:
			return
		}
	}
	if err := scanner.Err(); err != nil {
		select {
		case t.recv <- recvResult[M]{err: err}:
		case <-t.done:
		}
	}
}

func (t *Transport[M]) drainStderr(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 16*1024), 1024*1024)
	for scanner.Scan() {
		t.tail.add(scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.tail.add(err.Error())
	}
}

func (t *Transport[M]) decorate(err error) error {
	if err == nil {
		return nil
	}
	detail := t.tail.summary()
	if detail == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, detail)
}

func (t *Transport[M]) BindSessionID(sessionID string) {
	if sessionID == "" {
		return
	}
	t.identityMu.Lock()
	t.sessionID = sessionID
	t.identityMu.Unlock()
	if t.process != nil {
		t.bindOnce.Do(func() { processlog.Bind(t.vendor, t.corr, t.process.Pid, sessionID) })
	}
}

func (t *Transport[M]) boundSessionID() string {
	t.identityMu.Lock()
	defer t.identityMu.Unlock()
	return t.sessionID
}

func (t *Transport[M]) Close(ctx context.Context) error {
	t.closeMu.Lock()
	if t.closing {
		done := t.closeDone
		t.closeMu.Unlock()
		select {
		case <-done:
			t.closeMu.Lock()
			err := t.closeErr
			t.closeMu.Unlock()
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	t.closing = true
	close(t.done)
	t.closeMu.Unlock()

	err := t.closeProcess(ctx)
	t.closeMu.Lock()
	t.closeErr = err
	close(t.closeDone)
	t.closeMu.Unlock()
	return err
}

func (t *Transport[M]) closeProcess(ctx context.Context) error {
	defer t.closeOutput()
	t.closeInput()
	exited, process := t.exited, t.process
	if exited == nil {
		return nil
	}
	grace := forceKillAfter
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if half := remaining / 2; half < grace {
			grace = half
		}
	}
	if grace < 0 {
		grace = 0
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-exited:
		t.waitStderr(ctx)
		return nil
	case <-timer.C:
	case <-ctx.Done():
	}
	var killErr error
	if process != nil {
		killErr = process.Kill()
	}
	// After SIGKILL the child reaps promptly; wait a short floor for it, but
	// never past the caller's shutdown deadline.
	killWait := time.NewTimer(killWaitFloor)
	defer killWait.Stop()
	select {
	case <-exited:
		t.waitStderr(ctx)
		if killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
			return t.decorate(fmt.Errorf("kill process: %w", killErr))
		}
		return nil
	case <-killWait.C:
		return t.decorate(errors.Join(killErr, errors.New("process did not exit after kill")))
	case <-ctx.Done():
		return t.decorate(errors.Join(killErr, ctx.Err()))
	}
}

func (t *Transport[M]) closeInput() {
	t.inputOnce.Do(func() {
		if t.closer != nil {
			_ = t.closer.Close()
		}
	})
}

func (t *Transport[M]) closeOutput() {
	if t.stdout != nil {
		_ = t.stdout.Close()
	}
	if t.stderr != nil {
		_ = t.stderr.Close()
	}
}

func (t *Transport[M]) waitStderr(ctx context.Context) {
	if t.stderrDone == nil {
		return
	}
	select {
	case <-t.stderrDone:
	case <-ctx.Done():
	}
}

type stderrTail struct {
	mu    sync.Mutex
	lines []string
}

func (t *stderrTail) add(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(line) > 4096 {
		line = line[len(line)-4096:]
	}
	t.lines = append(t.lines, line)
	if len(t.lines) > 12 {
		t.lines = t.lines[len(t.lines)-12:]
	}
}

func (t *stderrTail) summary() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.Join(t.lines, "\n")
}
