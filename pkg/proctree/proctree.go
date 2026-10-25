// Package proctree tracks and kills every process a child starts.
//
// Agent CLIs run shell commands in process groups of their own, and once a
// parent exits its children are reparented and can no longer be found
// through it. So a Tracker records the child's descendants while it runs,
// keyed by PID and start time, and Kill terminates the ones still alive
// with the same start time, so a reused PID never names an unrelated
// process.
package proctree

import (
	"os"
	"sync"
	"time"
)

// DefaultInterval is how often a Tracker records descendants.
const DefaultInterval = 2 * time.Second

// Tracker records one child's descendants until Stop.
type Tracker struct {
	process *os.Process
	mu      sync.Mutex
	seen    map[int]string // pid → start time
	stop    chan struct{}
	once    sync.Once
}

// Track starts recording the descendants of process every interval.
func Track(process *os.Process, interval time.Duration) *Tracker {
	t := &Tracker{process: process, seen: map[int]string{}, stop: make(chan struct{})}
	if !supported {
		return t
	}
	t.Record()
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-t.stop:
				return
			case <-ticker.C:
				t.Record()
			}
		}
	}()
	return t
}

// Stop ends recording; Kill still works afterward.
func (t *Tracker) Stop() { t.once.Do(func() { close(t.stop) }) }

// Record adds the child's current descendants now. Nothing is found once
// the child has exited, so call it just before anything that may end the
// child; earlier records are kept.
func (t *Tracker) Record() {
	procs, ok := snapshot()
	if !ok {
		return
	}
	found := descendants(procs, t.process.Pid)
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, pid := range found {
		t.seen[pid] = procs[pid].start
	}
}

// Kill terminates the child's process group, its current descendants, and
// every recorded descendant that is still the same process. It returns the
// error from killing the group, os.ErrProcessDone when the group is gone.
func (t *Tracker) Kill() error {
	t.Record()
	t.mu.Lock()
	recorded := make(map[int]string, len(t.seen))
	for pid, start := range t.seen {
		recorded[pid] = start
	}
	t.mu.Unlock()
	if procs, ok := snapshot(); ok {
		for pid, start := range recorded {
			if p, alive := procs[pid]; alive && p.start == start {
				killPID(pid)
			}
		}
	}
	return killGroup(t.process)
}

type proc struct {
	ppid  int
	start string
}

// descendants walks the parent links in procs below root.
func descendants(procs map[int]proc, root int) []int {
	children := map[int][]int{}
	for pid, p := range procs {
		children[p.ppid] = append(children[p.ppid], pid)
	}
	var out []int
	for queue := []int{root}; len(queue) > 0; queue = queue[1:] {
		for _, child := range children[queue[0]] {
			out = append(out, child)
			queue = append(queue, child)
		}
	}
	return out
}
