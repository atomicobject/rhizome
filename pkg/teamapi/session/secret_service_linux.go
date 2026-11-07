//go:build linux

package session

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"
)

// SecretService shares one record per origin through the user's Linux keyring.
// secret-tool is supplied by libsecret. Secrets travel only on private pipes.
// The zero value uses the real OS account and a fixed system executable.
type SecretService struct {
	tool    string
	timeout time.Duration
}

const secretServiceTool = "/usr/bin/secret-tool"

// DefaultStore uses Secret Service; it never falls back to a plaintext file.
func DefaultStore() (Store, error) {
	if _, err := exec.LookPath(secretServiceTool); err != nil {
		return nil, fmt.Errorf("%w: install libsecret's /usr/bin/secret-tool", ErrStoreUnavailable)
	}
	return SecretService{}, nil
}

func (s SecretService) Load(origin string) ([]byte, error) {
	out, err := s.run("lookup", origin, nil)
	if errors.Is(err, ErrNotFound) {
		// A dismissed unlock prompt also looks like a missing password.
		// Search without --unlock still reports locked matching items.
		found, searchErr := s.run("search", origin, nil)
		if searchErr != nil {
			return nil, searchErr
		}
		if len(found) != 0 {
			return nil, fmt.Errorf("%w: matching session is locked or unreadable", ErrStoreUnavailable)
		}
	}
	if err != nil {
		return nil, err
	}
	record, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(string(out)))
	if err != nil || len(record) == 0 {
		return nil, fmt.Errorf("%w: stored record is malformed", ErrStoreUnavailable)
	}
	return record, nil
}

func (s SecretService) Save(origin string, record []byte) error {
	if _, err := s.Load(origin); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	encoded := base64.RawURLEncoding.EncodeToString(record)
	if _, err := s.run("store", origin, []byte(encoded)); err != nil {
		return err
	}
	saved, err := s.Load(origin)
	if err != nil {
		return err
	}
	if !bytes.Equal(saved, record) {
		return fmt.Errorf("%w: saved record did not read back", ErrStoreUnavailable)
	}
	return nil
}

func (s SecretService) Delete(origin string) error {
	_, err := s.run("clear", origin, nil)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	found, err := s.run("search", origin, nil)
	if err != nil {
		return err
	}
	if len(found) != 0 {
		return fmt.Errorf("%w: session could not be deleted (is the keyring locked?)", ErrStoreUnavailable)
	}
	return nil
}

func (s SecretService) run(verb, origin string, input []byte) ([]byte, error) {
	account, err := user.Current()
	if err != nil || account.Uid == "" || !filepath.IsAbs(account.HomeDir) {
		return nil, fmt.Errorf("%w: cannot resolve operating-system account", ErrStoreUnavailable)
	}
	runtime := filepath.Join("/run/user", account.Uid)
	tool, timeout := s.tool, s.timeout
	if tool == "" {
		tool = secretServiceTool
	}
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	args := []string{verb}
	if verb == "search" {
		args = append(args, "--all")
	}
	if verb == "store" {
		args = append(args, "--label=Rhizome team session")
	}
	args = append(args, "service", "rhizome-team-session", "account", origin)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, tool, args...)
	// Repository dotenv settings must not redirect credentials to another bus,
	// backend, collection or executable, or inject code into the helper.
	cmd.Env = []string{
		"PATH=/usr/bin:/bin", "HOME=" + account.HomeDir, "LANG=C",
		"XDG_RUNTIME_DIR=" + runtime,
		"DBUS_SESSION_BUS_ADDRESS=unix:path=" + filepath.Join(runtime, "bus"),
		"SECRET_BACKEND=service", "SECRET_SERVICE_BUS_NAME=org.freedesktop.secrets",
	}
	cmd.Dir = account.HomeDir
	cmd.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%w: Secret Service did not answer within %s (is the keyring locked?)", ErrStoreUnavailable, timeout)
		}
		var exit *exec.ExitError
		if (verb == "lookup" || verb == "clear") && errors.As(err, &exit) && exit.ExitCode() == 1 && stderr.Len() == 0 {
			return nil, ErrNotFound
		}
		// Never include helper output: it may contain a session record.
		return nil, fmt.Errorf("%w: Secret Service %s failed; ensure your login keyring is running and unlocked", ErrStoreUnavailable, verb)
	}
	return stdout.Bytes(), nil
}
