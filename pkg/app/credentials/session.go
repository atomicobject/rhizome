package credentials

import (
	"bufio"
	"io"
	"os"
	"strings"

	"github.com/atomicobject/rhizome/pkg/teamkeys"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// Session owns per-run credential state. Create one Session per CLI run and
// route every credential check or prompt through it so a key or skip recorded
// at any point in the run is visible to every later prompt site.
//
// Resolution precedence: process env -> provided this run -> persisted CLI
// config env -> team-key coverage (for team-covered providers).
//
// Keys and skips are persisted to the global CLI config immediately when
// entered, never staged for an end-of-run save.
type Session struct {
	reader *bufio.Reader
	out    io.Writer

	teamCoverage func() bool
	teamBundled  func() bool
	teamUnlocks  func(string) bool
	provided     map[string]string
	skipped      map[string]bool
}

// Option configures a Session.
type Option func(*Session)

// WithPrompts enables interactive prompting on the session. Without it the
// session never prompts (EnsureNeeds becomes a no-op, CanPrompt is false).
func WithPrompts(reader *bufio.Reader, out io.Writer) Option {
	return func(s *Session) {
		s.reader = reader
		s.out = out
	}
}

// WithTeamCoverage overrides the team-key coverage check (defaults to: the
// Atomic Object key resolves and decrypts the embedded team keys).
func WithTeamCoverage(covered func() bool) Option {
	return func(s *Session) {
		s.teamCoverage = covered
	}
}

// WithTeamKeyBundle overrides whether this build carries the Atomic Object
// team-key bundle and which pasted values unlock it (defaults: pkg/teamkeys).
func WithTeamKeyBundle(bundled func() bool, unlocks func(string) bool) Option {
	return func(s *Session) {
		s.teamBundled = bundled
		s.teamUnlocks = unlocks
	}
}

// NewSession creates a Session with default team-key coverage.
func NewSession(opts ...Option) *Session {
	s := &Session{
		teamBundled: teamkeys.Bundled,
		teamUnlocks: teamkeys.Unlocks,
		provided:    map[string]string{},
		skipped:     map[string]bool{},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// CanPrompt reports whether the session may interact with the user.
func (s *Session) CanPrompt() bool {
	return s != nil && s.reader != nil && s.out != nil
}

// Resolve returns the effective value for key following the session
// precedence: process env, then keys provided this run, then the persisted
// CLI config env block.
func (s *Session) Resolve(key string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	if value := strings.TrimSpace(s.provided[key]); value != "" {
		return value
	}
	cfg, err := obsidian.LoadCliConfig(true)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cfg.Env[key])
}

// Satisfied reports whether the need resolves to a usable credential, either
// via its env keys or via team-key coverage. Skips do not satisfy a need.
func (s *Session) Satisfied(need Need) bool {
	for _, key := range need.keys() {
		if s.Resolve(key) != "" {
			return true
		}
	}
	return need.AllowTeamKey && s.teamCovered()
}

// Skipped reports whether the user declined this credential, either earlier
// in this run or persisted from a previous run.
func (s *Session) Skipped(key string) bool {
	if s.skipped[key] {
		return true
	}
	cfg, err := obsidian.LoadCliConfig(true)
	if err != nil {
		return false
	}
	return cfg.CredentialSkips[key]
}

// Provide persists the key to the global CLI config immediately, clears any
// skip for it, and exports it to the process env so every resolver in this
// run (and future commands) sees it. Empty values are ignored.
func (s *Session) Provide(key, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	cfg, err := obsidian.LoadCliConfig(true)
	if err != nil {
		return err
	}
	if cfg.Env == nil {
		cfg.Env = map[string]string{}
	}
	cfg.Env[key] = value
	delete(cfg.CredentialSkips, key)
	if err := obsidian.SaveCliConfig(cfg); err != nil {
		return err
	}
	s.provided[key] = value
	delete(s.skipped, key)
	if err := os.Setenv(key, value); err != nil {
		return err
	}
	if key == AtomicRhizomeKey {
		teamkeys.ResetCache()
	}
	return nil
}

// Skip records and persists a "skip for now" decision for key.
func (s *Session) Skip(key string) error {
	cfg, err := obsidian.LoadCliConfig(true)
	if err != nil {
		return err
	}
	if cfg.CredentialSkips == nil {
		cfg.CredentialSkips = map[string]bool{}
	}
	cfg.CredentialSkips[key] = true
	if err := obsidian.SaveCliConfig(cfg); err != nil {
		return err
	}
	s.skipped[key] = true
	return nil
}

func (s *Session) teamCovered() bool {
	if s.teamCoverage != nil {
		return s.teamCoverage()
	}
	if s.Resolve(AtomicRhizomeKey) == "" {
		return false
	}
	return teamkeys.Available()
}
