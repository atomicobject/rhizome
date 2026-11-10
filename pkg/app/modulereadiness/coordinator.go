package modulereadiness

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/actions"
	"github.com/atomicobject/rhizome/pkg/vault/modulelifecycle"
)

type ActionRunner interface {
	Run(context.Context, string, map[string]any) (actions.Envelope, error)
}
type Options struct {
	Root     string
	Runner   ActionRunner
	Trust    func(context.Context) string
	Schema   func() error
	Include  IncludeSources
	Changed  func()
	Interval time.Duration
}
type entry struct {
	Key      string   `json:"key"`
	Snapshot Snapshot `json:"snapshot"`
}
type recoveryRequest struct {
	module string
	input  map[string]any
}
type Coordinator struct {
	opts     Options
	mu       sync.Mutex
	entries  map[string]entry
	requests chan recoveryRequest
	pending  map[string]bool
	done     chan struct{}
}

func New(opts Options) *Coordinator {
	c := &Coordinator{opts: opts, entries: map[string]entry{}, requests: make(chan recoveryRequest, 32), pending: map[string]bool{}, done: make(chan struct{})}
	data, _ := modulelifecycle.ReadRegularFile(opts.Root, ".rhizome/state/module-readiness.json", 1<<20)
	_ = json.Unmarshal(data, &c.entries)
	if c.entries == nil {
		c.entries = map[string]entry{}
	}
	return c
}
func (c *Coordinator) Snapshot() []Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]Snapshot, 0, len(c.entries))
	for _, e := range c.entries {
		snapshot := e.Snapshot
		snapshot.Recovery = slices.Clone(snapshot.Recovery)
		for i := range snapshot.Recovery {
			snapshot.Recovery[i].Choices = slices.Clone(snapshot.Recovery[i].Choices)
			snapshot.Recovery[i].Paths = slices.Clone(snapshot.Recovery[i].Paths)
		}
		result = append(result, snapshot)
	}
	slices.SortFunc(result, func(a, b Snapshot) int {
		if a.Module < b.Module {
			return -1
		}
		if a.Module > b.Module {
			return 1
		}
		return 0
	})
	return result
}
func (c *Coordinator) Recover(module string, r Request) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[module]
	if !ok {
		return fmt.Errorf("module readiness not found")
	}
	var input map[string]any
	allowed := false
	for _, a := range e.Snapshot.Recovery {
		if a.Kind != r.Kind {
			continue
		}
		if a.Kind == Retry && r.Value == "" {
			allowed = true
		}
		if a.Kind == Choice && a.Input != "" && slices.Contains(a.Choices, r.Value) {
			allowed = true
			input = map[string]any{a.Input: r.Value}
		}
	}
	if !allowed {
		return fmt.Errorf("recovery is not available for this state")
	}
	if c.pending[module] {
		return nil
	}
	select {
	case c.requests <- recoveryRequest{module, input}:
		c.pending[module] = true
		return nil
	default:
		return fmt.Errorf("readiness recovery queue is full")
	}
}

// Run is owned and joined by serve. Only this worker executes module actions.
func (c *Coordinator) Run(ctx context.Context) {
	defer close(c.done)
	interval := c.opts.Interval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	timer := time.NewTicker(interval)
	defer timer.Stop()
	c.scan(ctx, nil)
	for {
		select {
		case <-ctx.Done():
			return
		case r := <-c.requests:
			c.scan(ctx, &r)
			c.mu.Lock()
			delete(c.pending, r.module)
			c.mu.Unlock()
		case <-timer.C:
			c.scan(ctx, nil)
		}
	}
}
func (c *Coordinator) Wait() { <-c.done }
func (c *Coordinator) scan(ctx context.Context, request *recoveryRequest) {
	descriptors, err := modulelifecycle.Load(c.opts.Root)
	if err != nil {
		c.publish("", entry{Snapshot: Snapshot{State: Failed, Reason: err.Error()}})
		return
	}
	present := map[string]bool{}
	for _, d := range descriptors {
		if ctx.Err() != nil {
			return
		}
		present[d.Module] = true
		key, err := modulelifecycle.Fingerprint(c.opts.Root, d)
		trust := ""
		if c.opts.Trust != nil {
			trust = c.opts.Trust(ctx)
		}
		key += trust
		c.mu.Lock()
		old, exists := c.entries[d.Module]
		c.mu.Unlock()
		forced := request != nil && request.module == d.Module
		if exists && key == old.Key && !forced {
			continue
		}
		s := Snapshot{Module: d.Module, Title: d.Title, State: Checking}
		if s.Title == "" {
			s.Title = d.Module
		}
		c.publish(d.Module, entry{key, s})
		switch {
		case d.Problem != "":
			s.State = Blocked
			s.Reason = d.Problem
		case err != nil:
			s.State = Blocked
			s.Reason = err.Error()
		case trust != "":
			s.State = Blocked
			s.Reason = trust
			s.Recovery = []Recovery{{Kind: Trust, Label: "Trust this worktree in Settings"}, {Kind: Retry, Label: "Retry"}}
		default:
			var input map[string]any
			if forced {
				input = request.input
			}
			s = c.evaluate(ctx, d, &key, trust, s, input)
		}
		if ctx.Err() != nil {
			return
		}
		if s.State == Checking {
			c.publish(d.Module, entry{Snapshot: s})
			continue
		}
		if len(s.Recovery) == 0 && s.State != Ready {
			s.Recovery = []Recovery{{Kind: Retry, Label: "Retry"}}
		}
		c.publish(d.Module, entry{key, s})
	}
	c.mu.Lock()
	changed := false
	for id := range c.entries {
		if !present[id] {
			delete(c.entries, id)
			changed = true
		}
	}
	c.mu.Unlock()
	if changed {
		c.changed()
	}
}
func (c *Coordinator) publish(id string, e entry) {
	c.mu.Lock()
	c.entries[id] = e
	c.mu.Unlock()
	c.changed()
}
func (c *Coordinator) changed() {
	if c.opts.Changed != nil {
		c.opts.Changed()
	}
	c.save()
}
func (c *Coordinator) save() {
	c.mu.Lock()
	terminal := map[string]entry{}
	for id, e := range c.entries {
		if e.Snapshot.State != Checking && e.Snapshot.State != Working && e.Snapshot.State != Ready {
			terminal[id] = e
		}
	}
	data, err := json.Marshal(terminal)
	c.mu.Unlock()
	if err != nil {
		return
	}
	root, err := os.OpenRoot(c.opts.Root)
	if err != nil {
		return
	}
	defer root.Close()
	if root.MkdirAll(".rhizome/state", 0700) != nil {
		return
	}
	dir, err := root.OpenRoot(".rhizome/state")
	if err != nil {
		return
	}
	defer dir.Close()
	temp := "module-readiness." + rand.Text() + ".tmp"
	file, err := dir.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer dir.Remove(temp)
	_, writeErr := file.Write(data)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr == nil && closeErr == nil {
		_ = dir.Rename(temp, "module-readiness.json")
	}
}
