package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type DotEnvOptions struct {
	StrictKeys bool
}

var dotEnvStrict = DotEnvOptions{StrictKeys: true}

// LoadDotEnv reads a dotenv-style file and sets each KEY=VALUE in the process
// environment (via os.Setenv) only when the key is not already set.
//
// The file is optional: if it doesn't exist, LoadDotEnv returns (nil, nil).
//
// Parsing rules:
// - Blank lines and lines starting with # are ignored.
// - Leading "export " is allowed.
// - Keys must match [A-Za-z_][A-Za-z0-9_]*.
// - Values may be unquoted or quoted with single/double quotes.
func LoadDotEnv(path string) ([]string, error) {
	return LoadDotEnvWithOptions(path, dotEnvStrict)
}

func LoadDotEnvWithOptions(path string, opts DotEnvOptions) ([]string, error) {
	path = filepath.Clean(path)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var set []string
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		raw = strings.TrimSpace(strings.TrimPrefix(raw, "export "))
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}

		key, val, ok := strings.Cut(raw, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if !isEnvKey(key) {
			if opts.StrictKeys {
				return nil, fmt.Errorf("%s:%d: invalid env key", path, lineNo)
			}
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		val = strings.TrimSpace(val)
		val = stripInlineComment(val)
		val = strings.TrimSpace(val)
		if unq, err := unquoteEnvValue(val); err == nil {
			val = unq
		}
		if err := os.Setenv(key, val); err != nil {
			return nil, fmt.Errorf("%s:%d: setenv %q: %w", path, lineNo, key, err)
		}
		set = append(set, key)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return set, nil
}

// LoadDotEnvUpwards searches for dotenv files starting at startDir and walking up
// toward the filesystem root. At the first directory containing either `.env` or
// `.rhizome/.env`, it loads (in that order) any that exist and returns the keys
// it set.
//
// The files are optional and do not override already-set env vars.
func LoadDotEnvUpwards(startDir string) ([]string, error) {
	dir := filepath.Clean(startDir)
	var set []string
	for {
		candidates := []struct {
			path string
			opts DotEnvOptions
		}{
			{filepath.Join(dir, ".env"), DotEnvOptions{StrictKeys: false}},
			{filepath.Join(dir, ".rhizome", ".env"), dotEnvStrict},
		}
		foundAny := false
		for _, candidate := range candidates {
			if _, err := os.Stat(candidate.path); err == nil {
				foundAny = true
				keys, err := LoadDotEnvWithOptions(candidate.path, candidate.opts)
				if err != nil {
					return nil, err
				}
				set = append(set, keys...)
			}
		}
		if foundAny {
			return set, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, nil
		}
		dir = parent
	}
}

func isEnvKey(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case i == 0 && (r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r == '_'):
			continue
		case i > 0 && (r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_'):
			continue
		default:
			return false
		}
	}
	return true
}

func stripInlineComment(s string) string {
	// Only strip a comment when it starts at whitespace and we are not inside quotes.
	inSingle := false
	inDouble := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			if inDouble && i+1 < len(s) {
				i++ // Escaped characters cannot close the quoted value.
			}
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if inSingle || inDouble {
				continue
			}
			if i == 0 || (i > 0 && (s[i-1] == ' ' || s[i-1] == '\t')) {
				return strings.TrimSpace(s[:i])
			}
		}
	}
	return s
}

func unquoteEnvValue(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	if strings.HasPrefix(s, "'") && strings.HasSuffix(s, "'") && len(s) >= 2 {
		// Dotenv single-quoted strings are literal (no escapes).
		return s[1 : len(s)-1], nil
	}
	if strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"") {
		return strconv.Unquote(s)
	}
	return s, fmt.Errorf("not quoted")
}
