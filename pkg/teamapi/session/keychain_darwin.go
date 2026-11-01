//go:build darwin

package session

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// securityTool and securityTimeout are variables so tests can substitute a
// slow fake.
var (
	securityTool = "/usr/bin/security"
	// A locked keychain in a GUI session shows an unlock dialog and blocks
	// security until someone answers it.
	securityTimeout = 10 * time.Second
)

const (
	keychainService = "rhizome-team-session"
	// security exits with the low byte of the OSStatus; errSecItemNotFound
	// (-25300) becomes 44.
	exitItemNotFound = 44
)

// Keychain stores one opaque record per origin as a generic password in the
// macOS keychain. Every access goes through /usr/bin/security, so the item's
// access list trusts Apple's tool rather than a Rhizome code signature: ad-hoc
// signed builds, pinned versions, and the desktop sidecar all read the same
// item without a prompt. Any process running as the user can do the same.
type Keychain struct {
	// Path selects a keychain file for tests. Empty means the user's default
	// keychain. security silently falls back to the default keychain for a
	// path that does not exist, so a missing Path is an error.
	Path string
}

// Save creates or replaces the record. The record travels to security on
// standard input, never in argv, and is read back to confirm the write
// because security -i exits zero when a command fails.
func (k Keychain) Save(origin string, record []byte) error {
	if err := k.checkPath(); err != nil {
		return err
	}
	if strings.ContainsAny(origin+k.Path, "\"\n") {
		return fmt.Errorf("%w: origin or path contains a quote or newline", ErrStoreUnavailable)
	}
	encoded := base64.RawURLEncoding.EncodeToString(record)
	command := fmt.Sprintf("add-generic-password -U -s %s -a %s -w %s", quote(keychainService), quote(origin), quote(encoded))
	if k.Path != "" {
		command += " " + quote(k.Path)
	}
	ctx, cancel := context.WithTimeout(context.Background(), securityTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, securityTool, "-i")
	cmd.Stdin = strings.NewReader(command + "\n")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil || stderr.Len() > 0 {
		if ctx.Err() != nil {
			return fmt.Errorf("%w: keychain did not answer within %s (is it locked?)", ErrStoreUnavailable, securityTimeout)
		}
		return fmt.Errorf("%w: save failed: %s", ErrStoreUnavailable, firstLine(stderr.String(), err))
	}
	saved, err := k.Load(origin)
	if err != nil {
		return err
	}
	if !bytes.Equal(saved, record) {
		return fmt.Errorf("%w: saved record did not read back", ErrStoreUnavailable)
	}
	return nil
}

// Load returns the record, ErrNotFound, or ErrStoreUnavailable.
func (k Keychain) Load(origin string) ([]byte, error) {
	out, err := k.run(origin, "find-generic-password", "-w")
	if err != nil {
		return nil, err
	}
	record, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(string(out)))
	if err != nil {
		return nil, fmt.Errorf("%w: stored record is malformed", ErrStoreUnavailable)
	}
	return record, nil
}

// Delete removes the record. Deleting a missing record is not an error.
func (k Keychain) Delete(origin string) error {
	_, err := k.run(origin, "delete-generic-password")
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

// run invokes security with secret-free argv: service, origin, and path only.
func (k Keychain) run(origin, verb string, flags ...string) ([]byte, error) {
	if err := k.checkPath(); err != nil {
		return nil, err
	}
	args := append([]string{verb, "-s", keychainService, "-a", origin}, flags...)
	if k.Path != "" {
		args = append(args, k.Path)
	}
	ctx, cancel := context.WithTimeout(context.Background(), securityTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, securityTool, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%w: keychain did not answer within %s (is it locked?)", ErrStoreUnavailable, securityTimeout)
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == exitItemNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("%w: %s", ErrStoreUnavailable, firstLine(stderr.String(), err))
	}
	return stdout.Bytes(), nil
}

func (k Keychain) checkPath() error {
	if k.Path == "" {
		return nil
	}
	if _, err := os.Stat(k.Path); err != nil {
		return fmt.Errorf("%w: keychain %s: %v", ErrStoreUnavailable, k.Path, err)
	}
	return nil
}

// quote wraps a value for security -i, which splits on whitespace. Save has
// already rejected quotes and newlines; base64url contains neither.
func quote(s string) string { return `"` + s + `"` }

func firstLine(stderr string, err error) string {
	if line, _, _ := strings.Cut(strings.TrimSpace(stderr), "\n"); line != "" {
		return line
	}
	if err != nil {
		return err.Error()
	}
	return "unknown error"
}
