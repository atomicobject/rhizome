// Package proctree kills every process a child starts.
//
// Agent CLIs move shell commands out of the child's process group, and
// Claude Code gives each shell a session of its own; once a parent exits,
// its children are reparented and can no longer be found through it. So:
//
//   - Configure makes the child a session leader. Processes that only change
//     their process group stay in that session, however they are reparented.
//   - A Tracker records the child's descendants, keyed by PID and start time,
//     every interval and whenever Record is called, while they are still
//     linked to the child.
//   - Kill terminates the child's group, its session, every recorded
//     descendant that is still the same process, and the sessions those
//     descendants lead.
//
// Matching start times keeps a reused PID from naming an unrelated process.
// What can still escape is a process that starts its own session and whose
// parent exits before the next record.
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

// Track starts recording the descendants of a child that Configure set up.
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

// Stop ends periodic recording; Kill still works afterward.
func (t *Tracker) Stop() { t.once.Do(func() { close(t.stop) }) }

// Record adds the child's current descendants now. Call it just before
// anything that may end the child, since nothing is found through it
// afterward; earlier records are kept.
func (t *Tracker) Record() {
	if procs, ok := snapshot(); ok {
		t.recordFrom(procs)
	}
}

func (t *Tracker) recordFrom(procs map[int]proc) {
	found := descendants(procs, t.process.Pid)
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, pid := range found {
		t.seen[pid] = procs[pid].start
	}
}

// Kill terminates the child's process group, then every other process in
// its session, every recorded descendant that is still the same process,
// and every process in a session such a descendant leads. It returns the
// error from killing the group, os.ErrProcessDone when the group is gone.
// One snapshot, taken before the group is killed, both records the
// current descendants and decides the rest, so killing the group cannot
// orphan anything unseen. The group goes first: macOS refuses to signal a
// group whose only member is an unreaped zombie.
func (t *Tracker) Kill() error {
	procs, ok := snapshot()
	if ok {
		t.recordFrom(procs)
	}
	err := killGroup(t.process)
	if !ok {
		return err
	}
	t.mu.Lock()
	recorded := make(map[int]string, len(t.seen))
	for pid, start := range t.seen {
		recorded[pid] = start
	}
	t.mu.Unlock()
	sessions := map[int]bool{t.process.Pid: true}
	var victims []int
	for pid, start := range recorded {
		if p, alive := procs[pid]; alive && p.start == start {
			victims = append(victims, pid)
			if p.sid == pid {
				sessions[pid] = true
			}
		}
	}
	self := os.Getpid()
	for pid, p := range procs {
		if pid != self && pid > 1 && sessions[p.sid] {
			victims = append(victims, pid)
		}
	}
	for _, pid := range victims {
		killPID(pid)
	}
	return err
}

type proc struct {
	ppid  int
	sid   int
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
