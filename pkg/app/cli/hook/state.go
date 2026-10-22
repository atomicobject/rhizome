package hook

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/fileio"
	"github.com/atomicobject/rhizome/pkg/vault/indexlock"
)

// Local dispatcher state lives in the worktree's ignored .rhizome folder.
const (
	stateDirRel     = ".rhizome/hooks"
	maxScopeItems   = 2000
	maxLogBytes     = 128 << 10
	stateMaxAge     = 14 * 24 * time.Hour
	lockPollEvery   = 5 * time.Millisecond
	maxLockWait     = time.Second
	scopeStart      = "start"
	scopeToolCall   = "tool"
	statusFileName  = "status.json"
	logFileName     = "diagnostics.log"
	stateFolderName = "state"
)

func stateDir(root string) string { return filepath.Join(root, filepath.FromSlash(stateDirRel)) }

// sessionState is the emitted items of one session's agent contexts, keyed
// by scopeKey.
type sessionState struct {
	Scopes map[string][]string `json:"scopes"`
}

func sessionStatePath(root string, id Identity) string {
	sum := sha256.Sum256([]byte(id.Worktree + "\x00" + id.Harness + "\x00" + id.Session))
	return filepath.Join(stateDir(root), stateFolderName, hex.EncodeToString(sum[:12])+".json")
}

// scopeKey separates session-start delivery from tool-call delivery and one
// agent context from another: per agent ID when the host gives one, which
// outlives its turns, and otherwise per turn, the narrowest scope a Codex
// subagent without an ID never shares with its parent. Claude Code's main
// agent has neither, so its scope is the session.
func scopeKey(scope string, id Identity) string {
	turn := id.Turn
	if id.AgentID != "" {
		turn = ""
	}
	return scope + "|" + id.AgentID + "|" + turn
}

func scopeFor(point string) string {
	if point == PostToolUse {
		return scopeToolCall
	}
	return scopeStart
}

// resetMarker names a reset that could not take the state lock. It sits
// beside the state file until a locked write applies it, and every read
// applies it first, so pre-reset items never suppress delivery meanwhile.
const resetMarker = ".reset-"

// readSessionState reads a session's state with its pending resets applied,
// and returns the marker files it applied.
func readSessionState(path string) (sessionState, []string) {
	st := sessionState{Scopes: map[string][]string{}}
	data, err := os.ReadFile(path)
	if err == nil && json.Unmarshal(data, &st) != nil {
		st = sessionState{}
	}
	if st.Scopes == nil {
		st.Scopes = map[string][]string{}
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	var markers []string
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), filepath.Base(path)+resetMarker) {
			continue
		}
		marker := filepath.Join(filepath.Dir(path), e.Name())
		var pending struct {
			AgentID string `json:"agentId"`
		}
		if data, err := os.ReadFile(marker); err == nil && json.Unmarshal(data, &pending) == nil {
			st.reset(pending.AgentID)
		}
		markers = append(markers, marker)
	}
	return st, markers
}

// writeResetMarker records a reset of agentID's context without the lock.
// Each marker has its own name, so concurrent resets never overwrite one
// another and a locked write removes only the markers it applied.
func writeResetMarker(path, agentID string) error {
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return err
	}
	return writeJSONAtomic(path+resetMarker+hex.EncodeToString(suffix[:]), map[string]string{"agentId": agentID})
}

// reset discards what an agent's context was given, in both scopes and every
// turn, so a compaction or clear delivers it again.
func (s *sessionState) reset(agentID string) {
	for key := range s.Scopes {
		parts := strings.SplitN(key, "|", 3)
		if len(parts) == 3 && parts[1] == agentID {
			delete(s.Scopes, key)
		}
	}
}

func (s *sessionState) record(key string, ids []string) {
	list := append(s.Scopes[key], ids...)
	if len(list) > maxScopeItems {
		list = list[len(list)-maxScopeItems:]
	}
	s.Scopes[key] = list
}

func writeJSONAtomic(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fileio.WriteFileAtomic(path, data, 0o644)
}

// lockState takes the session's exclusive state lock, waiting at most until
// ctx ends or maxLockWait passes. It returns nil when the lock is busy.
func lockState(ctx context.Context, path string) func() error {
	wait, cancel := context.WithTimeout(ctx, maxLockWait)
	defer cancel()
	for {
		release, ok, err := indexlock.TryAcquireWithOptions(path+".lock", indexlock.AcquireOptions{Role: "hook/state"})
		if err == nil && ok {
			return release
		}
		select {
		case <-wait.Done():
			return nil
		case <-time.After(lockPollEvery):
		}
	}
}

// pruneSessionStates removes session state nobody touched recently, so the
// folder stays bounded. It runs on session start only.
func pruneSessionStates(root string, now time.Time) {
	dir := filepath.Join(stateDir(root), stateFolderName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if info, err := e.Info(); err == nil && now.Sub(info.ModTime()) > stateMaxAge {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// Status is when each handler last returned context, per harness.
type Status map[string]map[string]time.Time

// ReadStatus reads the dispatcher's status for init's report; a missing or
// unreadable file is an empty status.
func ReadStatus(root string) Status {
	var st Status
	if data, err := os.ReadFile(filepath.Join(stateDir(root), statusFileName)); err == nil {
		if json.Unmarshal(data, &st) != nil {
			st = nil
		}
	}
	if st == nil {
		// A missing, unreadable, or null file.
		st = Status{}
	}
	return st
}

// recordDelivered notes that handlers returned context. Concurrent sessions
// may lose one another's update; the next delivery repairs it.
func recordDelivered(root, harness string, handlers []string, at time.Time) error {
	st := ReadStatus(root)
	if st[harness] == nil {
		st[harness] = map[string]time.Time{}
	}
	for _, key := range handlers {
		st[harness][key] = at.UTC().Truncate(time.Second)
	}
	return writeJSONAtomic(filepath.Join(stateDir(root), statusFileName), st)
}

// logDiagnostic appends one line to the bounded local diagnostic log. When
// the log is full, the previous one is kept as diagnostics.log.1.
func logDiagnostic(root string, at time.Time, ev Event, format string, args ...any) {
	if root == "" {
		return
	}
	path := filepath.Join(stateDir(root), logFileName)
	if info, err := os.Stat(path); err == nil && info.Size() > maxLogBytes {
		_ = os.Rename(path, path+".1")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	msg := fmt.Sprintf(format, args...)
	if len(msg) > 2000 {
		msg = msg[:2000]
	}
	line, _ := json.Marshal(map[string]string{"at": at.UTC().Format(time.RFC3339), "harness": ev.Harness, "event": ev.Event, "tool": ev.Tool, "message": msg})
	_, _ = f.Write(append(line, '\n'))
}
