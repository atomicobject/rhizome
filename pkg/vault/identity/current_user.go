// Package identity owns ignored per-vault agent identity state.
package identity

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	AgentStateDir      = "agent"
	CurrentUserFile    = "user.yml"
	CurrentUserType    = "Person"
	currentUserVersion = 1
)

var ErrCurrentUserMissing = errors.New("current user is not configured")

type CurrentUser struct {
	Version int    `json:"version" yaml:"version"`
	Ref     string `json:"ref" yaml:"ref"`
}

func CurrentUserPath(vaultRoot string) string {
	return filepath.Join(vaultRoot, ".rhizome", AgentStateDir, CurrentUserFile)
}

func ReadCurrentUser(vaultRoot string) (CurrentUser, bool, error) {
	path := CurrentUserPath(vaultRoot)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return CurrentUser{}, false, nil
	}
	if err != nil {
		return CurrentUser{}, false, err
	}
	var cfg CurrentUser
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return CurrentUser{}, true, fmt.Errorf("read current user: %w", err)
	}
	cfg.Ref = strings.TrimSpace(cfg.Ref)
	if cfg.Version == 0 {
		cfg.Version = currentUserVersion
	}
	return cfg, true, nil
}

func WriteCurrentUser(vaultRoot, ref string) (CurrentUser, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return CurrentUser{}, fmt.Errorf("current user ref is required")
	}
	cfg := CurrentUser{Version: currentUserVersion, Ref: ref}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return CurrentUser{}, err
	}
	path := CurrentUserPath(vaultRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return CurrentUser{}, err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return CurrentUser{}, err
	}
	return cfg, nil
}

type Resolution struct {
	Configured  bool   `json:"configured"`
	Ref         string `json:"ref,omitempty"`
	Found       bool   `json:"found"`
	ResolvedRef string `json:"resolvedRef,omitempty"`
	Path        string `json:"path,omitempty"`
	Title       string `json:"title,omitempty"`
	TypeName    string `json:"typeName,omitempty"`
	ErrorCode   string `json:"errorCode,omitempty"`
	Error       string `json:"error,omitempty"`
}

func MissingResolution() Resolution {
	return Resolution{
		Configured: false,
		ErrorCode:  "missing_current_user",
		Error:      ErrCurrentUserMissing.Error(),
	}
}

func ResolutionForConfig(cfg CurrentUser, found bool, resolvedRef, path, title, typeName string) Resolution {
	out := Resolution{
		Configured:  true,
		Ref:         strings.TrimSpace(cfg.Ref),
		Found:       found,
		ResolvedRef: strings.TrimSpace(resolvedRef),
		Path:        strings.TrimSpace(path),
		Title:       strings.TrimSpace(title),
		TypeName:    strings.TrimSpace(typeName),
	}
	switch {
	case !found:
		out.ErrorCode = "unresolved_current_user"
		out.Error = fmt.Sprintf("current user ref %q did not resolve", cfg.Ref)
	case out.TypeName != CurrentUserType:
		out.ErrorCode = "current_user_not_person"
		out.Error = fmt.Sprintf("current user ref %q resolved to %q, not %s", cfg.Ref, out.TypeName, CurrentUserType)
	}
	return out
}
