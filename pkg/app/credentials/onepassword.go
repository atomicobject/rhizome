package credentials

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"unicode"

	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// OnePasswordReader reads a credential from a stable 1Password item reference.
// Implementations must not expose the resolved value in an error.
type OnePasswordReader interface {
	Read(context.Context, string, string) (string, error)
}

// OnePasswordCLI reads values through the installed 1Password CLI.
type OnePasswordCLI struct {
	Binary string
	Run    func(context.Context, string, ...string) ([]byte, error)
}

// Read resolves reference through `op read`. It intentionally discards command
// output when the command fails because it may contain a credential value.
func (c OnePasswordCLI) Read(ctx context.Context, account, reference string) (string, error) {
	binary := c.Binary
	if binary == "" {
		binary = "op"
	}
	run := c.Run
	if run == nil {
		run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).Output()
		}
	}
	output, err := run(ctx, binary, "read", reference, "--account", account)
	if err != nil {
		return "", fmt.Errorf("read 1Password credential")
	}
	value := strings.TrimSpace(string(output))
	if value == "" {
		return "", fmt.Errorf("1Password credential is empty")
	}
	return value, nil
}

// ImportFromOnePassword resolves mappings from 1Password and writes them into
// the user-level Rhizome config after every read succeeds.
func ImportFromOnePassword(ctx context.Context, account string, mappings []string) (int, error) {
	return Import(ctx, OnePasswordCLI{}, account, mappings, obsidian.LoadCliConfig, obsidian.SaveCliConfig)
}

// Import validates mappings before reading 1Password, then persists their
// values in one atomic config save. Callers can supply synthetic readers and
// config IO for tests.
func Import(
	ctx context.Context,
	reader OnePasswordReader,
	account string,
	mappings []string,
	load func(bool) (obsidian.CliConfig, error),
	save func(obsidian.CliConfig) error,
) (int, error) {
	account = strings.TrimSpace(account)
	if account == "" {
		return 0, fmt.Errorf("1Password account is required")
	}
	if reader == nil {
		return 0, fmt.Errorf("1Password reader is required")
	}
	if load == nil || save == nil {
		return 0, fmt.Errorf("credential config IO is required")
	}

	parsed, err := parseMappings(mappings)
	if err != nil {
		return 0, err
	}
	values := make(map[string]string, len(parsed))
	for _, mapping := range parsed {
		value, err := reader.Read(ctx, account, mapping.reference)
		if err != nil {
			return 0, fmt.Errorf("import %s from 1Password", mapping.key)
		}
		values[mapping.key] = value
	}

	cfg, err := load(true)
	if err != nil {
		return 0, fmt.Errorf("load CLI config: %w", err)
	}
	if cfg.Env == nil {
		cfg.Env = map[string]string{}
	}
	for key, value := range values {
		cfg.Env[key] = value
		delete(cfg.CredentialSkips, key)
	}
	if err := save(cfg); err != nil {
		return 0, fmt.Errorf("save CLI config: %w", err)
	}
	return len(values), nil
}

type credentialMapping struct {
	key       string
	reference string
}

func parseMappings(mappings []string) ([]credentialMapping, error) {
	if len(mappings) == 0 {
		return nil, fmt.Errorf("at least one --key mapping is required")
	}
	parsed := make([]credentialMapping, 0, len(mappings))
	seen := map[string]struct{}{}
	for _, mapping := range mappings {
		key, reference, found := strings.Cut(mapping, "=")
		key = strings.TrimSpace(key)
		reference = strings.TrimSpace(reference)
		if !found || key == "" || reference == "" {
			return nil, fmt.Errorf("credential mappings must use KEY=op://vault/item/field")
		}
		if !IsSupportedKey(key) {
			return nil, fmt.Errorf("unsupported credential key %q", key)
		}
		if !isOnePasswordReference(reference) {
			return nil, fmt.Errorf("invalid 1Password reference for %s", key)
		}
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("credential key %q was specified more than once", key)
		}
		seen[key] = struct{}{}
		parsed = append(parsed, credentialMapping{key: key, reference: reference})
	}
	return parsed, nil
}

func isOnePasswordReference(reference string) bool {
	if !strings.HasPrefix(reference, "op://") || strings.IndexFunc(reference, unicode.IsControl) >= 0 {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(reference, "op://"), "/")
	if len(parts) < 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
	}
	return true
}
