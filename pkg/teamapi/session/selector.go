package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultOrigin is the gateway origin internal release builds compile in:
// -X github.com/atomicobject/rhizome/pkg/teamapi/session.DefaultOrigin=https://...
// Public builds leave it empty.
var DefaultOrigin = ""

// DevelopmentOrigin is the default for development builds (version "dev").
const DevelopmentOrigin = "http://127.0.0.1:8787"

// OriginEnv lets development builds pick among origins that already have a
// stored session, so end-to-end fixtures can target a second local stack.
const OriginEnv = "RHIZOME_TEAM_API_ORIGIN"

// The selector names the origin the person last signed in to. It holds no
// secret, so it is a plain file next to the lock.
type selectorFile struct {
	V      int    `json:"v"`
	Origin string `json:"origin"`
}

// DefaultSelectorPath is team-session.json beside DefaultLockPath.
func DefaultSelectorPath() (string, error) {
	lock, err := DefaultLockPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(lock), "team-session.json"), nil
}

// CurrentOrigin reads the real OS user's selector. ok is false when no
// sign-in has selected an origin.
func CurrentOrigin() (origin string, ok bool, err error) {
	path, err := DefaultSelectorPath()
	if err != nil {
		return "", false, err
	}
	return ReadSelector(path)
}

// ReadSelector reads a selector file; see CurrentOrigin.
func ReadSelector(path string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	var sel selectorFile
	if err := json.Unmarshal(data, &sel); err != nil {
		return "", false, fmt.Errorf("team session selector %s is malformed", path)
	}
	if sel.V > RecordVersion {
		return "", false, ErrNewerRecord
	}
	origin, err := CanonicalOrigin(sel.Origin)
	if err != nil || sel.V != RecordVersion {
		return "", false, fmt.Errorf("team session selector %s is malformed", path)
	}
	return origin, true, nil
}

func writeSelector(path, origin string) error {
	data, err := json.Marshal(selectorFile{V: RecordVersion, Origin: origin})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".team-session-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// clearSelector removes the selector only if it still names origin.
func clearSelector(path, origin string) error {
	current, ok, err := ReadSelector(path)
	if err != nil || !ok || current != origin {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// ResolveOptions are the inputs to ResolveOrigin.
type ResolveOptions struct {
	// Server is an explicit --server value from an interactive rzm auth command.
	Server string
	// Development marks a development build (version.Development()).
	Development bool
	// Getenv reads OriginEnv. It is consulted only for development builds.
	Getenv func(string) string
	// Store proves an OriginEnv origin already has a stored session.
	Store Store
	// SelectorPath is normally DefaultSelectorPath.
	SelectorPath string
}

// ResolveOrigin picks the gateway origin: an explicit server; in development
// builds, OriginEnv if that origin already has a stored session; the
// selector; then the development or compiled-in default. It returns "" when
// nothing applies. Repository configuration never takes part, and OriginEnv
// cannot send a token anywhere the person has not signed in interactively.
func ResolveOrigin(opts ResolveOptions) (string, error) {
	if strings.TrimSpace(opts.Server) != "" {
		return CanonicalOrigin(opts.Server)
	}
	development := opts.Development
	if development && opts.Getenv != nil {
		if raw := strings.TrimSpace(opts.Getenv(OriginEnv)); raw != "" {
			origin, err := CanonicalOrigin(raw)
			if err != nil {
				return "", fmt.Errorf("%s: %w", OriginEnv, err)
			}
			if opts.Store == nil {
				return "", fmt.Errorf("%s: %w", OriginEnv, ErrStoreUnavailable)
			}
			if _, err := opts.Store.Load(origin); err != nil {
				return "", fmt.Errorf("%s names %s, which has no stored session: %w", OriginEnv, origin, err)
			}
			return origin, nil
		}
	}
	if opts.SelectorPath != "" {
		origin, ok, err := ReadSelector(opts.SelectorPath)
		if err != nil || ok {
			return origin, err
		}
	}
	if development {
		return DevelopmentOrigin, nil
	}
	if DefaultOrigin != "" {
		return CanonicalOrigin(DefaultOrigin)
	}
	return "", nil
}
