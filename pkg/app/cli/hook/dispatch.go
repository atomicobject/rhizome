package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxPayloadBytes       = 1 << 20
	maxHandlerOutputBytes = 256 << 10
	maxPrefixFileBytes    = 64 << 10
	// outputReserve is kept back from handlers for writing output and state.
	outputReserve = 300 * time.Millisecond
	itemSeparator = "\n\n"
)

// contextBudget is the most context text a host accepts from one hook:
// Claude Code caps it at 10,000 characters and Codex at about 2,500 tokens.
func contextBudget(harness string) int {
	if harness == HarnessCodex {
		return 7000
	}
	return 9000
}

// Options configures one dispatcher invocation.
type Options struct {
	Harness string
	Point   string
	Stdin   io.Reader
	Stdout  io.Writer
	// Cwd is the process working directory, used when the payload has none.
	Cwd string
	// Handlers returns the handlers of enabled starters for a repository
	// root, or none when hooks are off there.
	Handlers func(root string) ([]Handler, error)
	Now      func() time.Time
	// Deadline overrides the fixed Deadline in tests.
	Deadline time.Duration
}

// Item is one piece of context a handler returns.
type Item struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// handlerInput is what a handler reads on standard input.
type handlerInput struct {
	Version    int      `json:"version"`
	Event      Event    `json:"event"`
	Emitted    []string `json:"emitted"`
	Budget     int      `json:"budget"`
	DeadlineMS int64    `json:"deadlineMs"`
}

// Run dispatches one hook invocation. It never returns an error: failures
// end with the items gathered so far, or no output, and a diagnostic entry.
//
// Docs:
// - [[agent-surface-integration-modes#^SPEC-0045-US5-AC6]]
// - [[agent-surface-integration-modes#^SPEC-0045-US5-AC7]]
func Run(ctx context.Context, opts Options) {
	d := newDispatch(ctx, opts)
	defer d.cancel()
	defer func() {
		if p := recover(); p != nil {
			d.log("panic: %v", p)
		}
	}()
	if err := d.run(); err != nil {
		d.log("%v", err)
	}
}

// log writes one diagnostic entry in the event's worktree, or the process
// working directory's when the payload never named one.
func (d *dispatch) log(format string, args ...any) {
	root := d.ev.Root
	if root == "" {
		root, _ = worktreeRoot(d.opts.Cwd)
	}
	logDiagnostic(root, d.opts.Now(), d.ev, format, args...)
}

type dispatch struct {
	opts   Options
	ctx    context.Context
	cancel context.CancelFunc
	ev     Event
}

// newDispatch starts an invocation's deadline. Until the payload is parsed,
// its diagnostics name the harness and hook point it was started for.
func newDispatch(ctx context.Context, opts Options) *dispatch {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	deadline := opts.Deadline
	if deadline <= 0 {
		deadline = Deadline
	}
	d := &dispatch{opts: opts, ev: Event{Harness: opts.Harness, Event: opts.Point}}
	d.ctx, d.cancel = context.WithTimeout(ctx, deadline)
	return d
}

func (d *dispatch) run() error {
	if HostEvent(d.opts.Point) == "" || (d.opts.Harness != HarnessClaude && d.opts.Harness != HarnessCodex) {
		return fmt.Errorf("unsupported hook %s %s", d.opts.Harness, d.opts.Point)
	}
	raw, err := readWithin(d.ctx, d.opts.Stdin)
	if err != nil {
		return fmt.Errorf("read payload: %w", err)
	}
	if len(raw) > maxPayloadBytes {
		return errors.New("payload exceeds 1 MiB")
	}
	ev, err := normalize(d.opts.Harness, d.opts.Point, raw, d.opts.Cwd)
	if err != nil {
		if errors.Is(err, errNoWorktree) {
			return nil
		}
		return err
	}
	d.ev = ev
	handlers, err := d.opts.Handlers(ev.Root)
	if err != nil {
		return fmt.Errorf("load hook handlers: %w", err)
	}
	var applicable, starters []Handler
	for _, h := range handlers {
		if applies(h, ev) {
			applicable = append(applicable, h)
		}
	}
	statePath := sessionStatePath(ev.Root, ev.Agent)
	// A subagent compacted since its last tool call gets its start items
	// with this one.
	startEv := ev
	if ev.Event == PostToolUse && ev.Agent.AgentID != "" && startDue(statePath, ev.Agent.AgentID) {
		startEv = Event{Harness: ev.Harness, Event: SubagentStart, Root: ev.Root, Agent: ev.Agent, Reset: "compact"}
		for _, h := range handlers {
			if applies(h, startEv) {
				starters = append(starters, h)
			}
		}
	}
	// Nothing to do: leave before touching emitted-item state. A reset still
	// clears it, so later tool calls deliver again after a compaction.
	if len(applicable) == 0 && len(starters) == 0 && (ev.Reset == "" || len(handlers) == 0) {
		return nil
	}

	release := lockState(d.ctx, statePath)
	if release != nil {
		defer func() { _ = release() }()
	}
	var errs []error
	state, pending := readSessionState(statePath)
	if ev.Reset != "" {
		state.reset(ev.Agent.AgentID)
		if release == nil {
			if err := writeResetMarker(statePath, ev.Agent.AgentID); err != nil {
				errs = append(errs, fmt.Errorf("record reset: %w", err))
			}
		}
	}
	if ev.Event == PostCompact {
		if err := writeJSONAtomic(startDuePath(statePath, ev.Agent.AgentID), ev.Agent.AgentID); err != nil {
			errs = append(errs, fmt.Errorf("record start items due: %w", err))
		}
	}

	var texts, delivered, failures []string
	used := 0
	// deliver runs handlers for runEv and gathers their new items within the
	// output budget, returning the IDs to record under key.
	deliver := func(hs []Handler, runEv Event, key string) (ids []string, failed bool) {
		emitted := map[string]bool{}
		for _, id := range state.Scopes[key] {
			emitted[id] = true
		}
		for _, h := range hs {
			remaining := contextBudget(ev.Harness) - used
			if remaining <= 0 {
				break
			}
			items, stderr, err := d.runHandler(h, runEv, emitted, remaining)
			switch {
			case err != nil:
				// One handler's failure keeps the others' items, and its own
				// when it reported the failure itself.
				failures = append(failures, fmt.Sprintf("%s: %v", h.Key(), err))
				failed = true
			case stderr != "":
				// A handler that recovers from an error still reports it.
				failures = append(failures, fmt.Sprintf("%s stderr: %s", h.Key(), stderr))
			}
			gave := false
			for _, item := range items {
				text := strings.TrimSpace(item.Text)
				if item.ID == "" || text == "" || emitted[item.ID] {
					continue
				}
				cost := utf8.RuneCountInString(text)
				if len(texts) > 0 {
					cost += len(itemSeparator)
				}
				if used+cost > contextBudget(ev.Harness) {
					continue
				}
				used += cost
				texts = append(texts, text)
				ids = append(ids, item.ID)
				emitted[item.ID] = true
				gave = true
			}
			if gave && !slices.Contains(delivered, h.Key()) {
				delivered = append(delivered, h.Key())
			}
		}
		return ids, failed
	}
	startKey := scopeKey(scopeStart, ev.Agent)
	startIDs, startFailed := deliver(starters, startEv, startKey)
	key := scopeKey(scopeFor(ev.Event), ev.Agent)
	ids, _ := deliver(applicable, ev, key)

	if len(failures) > 0 {
		errs = append(errs, errors.New(strings.Join(failures, "; ")))
	}
	commit := func() {
		if err := writeJSONAtomic(statePath, state); err != nil {
			errs = append(errs, fmt.Errorf("record emitted items: %w", err))
			return
		}
		for _, marker := range pending {
			_ = os.Remove(marker)
		}
		pending = nil
	}
	// A reset is committed before output, so a host that stops reading
	// cannot leave pre-reset items suppressing later deliveries.
	if release != nil && (ev.Reset != "" || len(pending) > 0) {
		commit()
	}
	if len(texts) > 0 {
		if err := writeOutput(d.opts.Stdout, ev, strings.Join(texts, itemSeparator)); err != nil {
			return errors.Join(append(errs, err)...)
		}
	}
	// Start items that were due have been delivered; a failed starter
	// leaves them due for the next tool call.
	if len(starters) > 0 && !startFailed {
		_ = os.Remove(startDuePath(statePath, ev.Agent.AgentID))
	}
	// Record only after emitting: a crash in between repeats an item rather
	// than losing it.
	if release == nil {
		errs = append(errs, errors.New("state lock busy; emitted items not recorded"))
	} else if len(ids) > 0 || len(startIDs) > 0 {
		state.record(startKey, startIDs)
		state.record(key, ids)
		commit()
	}
	if len(delivered) > 0 {
		if err := recordDelivered(ev.Root, ev.Harness, delivered, d.opts.Now()); err != nil {
			errs = append(errs, fmt.Errorf("record status: %w", err))
		}
	}
	if ev.Event == SessionStart {
		pruneSessionStates(ev.Root, d.opts.Now())
	}
	return errors.Join(errs...)
}

// applies reports whether the event gives a handler something to act on.
func applies(h Handler, ev Event) bool {
	if !has(h.Events, ev.Event) {
		return false
	}
	if ev.Event != PostToolUse {
		return true
	}
	if ev.Tool == "" || !has(h.Tools, ev.Tool) {
		return false
	}
	if len(ev.Paths) > 0 {
		return true
	}
	if ev.Tool != ToolShell || h.CommandPrefixes == "" || strings.TrimSpace(ev.Command) == "" {
		return false
	}
	return matchesPrefix(ev.Command, readPrefixes(ev.Root, h.CommandPrefixes))
}

// readPrefixes reads a handler's published command prefixes; a missing or
// unreadable file lists none.
func readPrefixes(root, rel string) []string {
	f, err := os.Open(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return nil
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxPrefixFileBytes+1))
	if err != nil || len(data) > maxPrefixFileBytes {
		return nil
	}
	var prefixes []string
	if json.Unmarshal(data, &prefixes) != nil {
		return nil
	}
	return prefixes
}

// runHandler runs one handler under the remaining deadline and returns its
// items and what it wrote to standard error. A timeout kills the handler's
// whole process tree. A handler that reports an error keeps its items, and
// the error marks its run as failed.
func (d *dispatch) runHandler(h Handler, ev Event, emitted map[string]bool, budget int) ([]Item, string, error) {
	if len(h.Command) == 0 {
		return nil, "", errors.New("no command")
	}
	limit := time.Until(deadlineOf(d.ctx)) - outputReserve
	if h.Timeout > 0 && h.Timeout < limit {
		limit = h.Timeout
	}
	if limit <= 0 {
		return nil, "", errors.New("no time left before the deadline")
	}
	ctx, cancel := context.WithTimeout(d.ctx, limit)
	defer cancel()
	in := handlerInput{Version: ProtocolVersion, Event: ev, Emitted: []string{}, Budget: budget, DeadlineMS: limit.Milliseconds()}
	for id := range emitted {
		in.Emitted = append(in.Emitted, id)
	}
	input, err := json.Marshal(in)
	if err != nil {
		return nil, "", err
	}
	cmd := exec.CommandContext(ctx, h.Command[0], h.Command[1:]...)
	cmd.Dir = ev.Root
	cmd.Stdin = bytes.NewReader(input)
	stdout := &boundedBuffer{limit: maxHandlerOutputBytes}
	stderr := &boundedBuffer{limit: 2000}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	configureProcessTree(cmd)
	cmd.Cancel = func() error { return killProcessTree(cmd) }
	cmd.WaitDelay = 100 * time.Millisecond
	runErr := cmd.Run()
	errText := strings.TrimSpace(stderr.String())
	if ctx.Err() != nil {
		return nil, "", fmt.Errorf("timed out after %s", limit.Round(time.Millisecond))
	}
	if stdout.overflow {
		return nil, "", errors.New("output exceeds 256 KiB")
	}
	if runErr != nil {
		return nil, "", fmt.Errorf("%v: %s", runErr, errText)
	}
	if len(bytes.TrimSpace(stdout.Bytes())) == 0 {
		return nil, errText, nil
	}
	var out struct {
		Items []Item `json:"items"`
		// Error is a failure the handler recovered from: its items still
		// count, but the run did not complete.
		Error string `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return nil, "", fmt.Errorf("parse output: %w; stderr: %s", err, errText)
	}
	if out.Error != "" {
		return out.Items, "", fmt.Errorf("reported %q; stderr: %s", out.Error, errText)
	}
	return out.Items, errText, nil
}

// readWithin reads the payload, giving up when ctx ends: a host that never
// closes standard input must not hold the dispatcher past its deadline.
func readWithin(ctx context.Context, r io.Reader) ([]byte, error) {
	type read struct {
		data []byte
		err  error
	}
	done := make(chan read, 1)
	go func() {
		data, err := io.ReadAll(io.LimitReader(r, maxPayloadBytes+1))
		done <- read{data, err}
	}()
	select {
	case got := <-done:
		return got.data, got.err
	case <-ctx.Done():
		return nil, errors.New("standard input stayed open past the deadline")
	}
}

func deadlineOf(ctx context.Context) time.Time {
	if d, ok := ctx.Deadline(); ok {
		return d
	}
	return time.Now().Add(Deadline)
}

// writeOutput emits context in the host's event-specific JSON form. Claude
// Code and Codex both read hookSpecificOutput.additionalContext.
func writeOutput(w io.Writer, ev Event, text string) error {
	out := map[string]any{"hookSpecificOutput": map[string]string{
		"hookEventName":     HostEvent(ev.Event),
		"additionalContext": text,
	}}
	data, err := json.Marshal(out)
	if err != nil {
		return err
	}
	_, err = w.Write(append(data, '\n'))
	return err
}

// boundedBuffer keeps at most limit bytes and notes whether more arrived.
type boundedBuffer struct {
	bytes.Buffer
	limit    int
	overflow bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if room := b.limit - b.Len(); len(p) > room {
		b.overflow = true
		if room > 0 {
			b.Buffer.Write(p[:room])
		}
		return len(p), nil
	}
	return b.Buffer.Write(p)
}
