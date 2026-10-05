package obsidian

import "github.com/atomicobject/rhizome/pkg/vault/ignore"

// DefaultIgnorePatterns returns a copy of the built-in ignore prefixes.
func DefaultIgnorePatterns() []string {
	return ignore.DefaultIgnorePatterns()
}

// DefaultIgnoreFile renders a .rhizome/ignore body using the default patterns.
func DefaultIgnoreFile() string {
	return ignore.DefaultIgnoreFile()
}
