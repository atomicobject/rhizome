// Package notediscovery compiles configured note ownership into one immutable
// decision point. It deliberately does not read files, parse note syntax, or
// classify code languages: callers provide a canonical relative path and say
// whether their own code lane could otherwise own it.
package notediscovery

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/bmatcuk/doublestar/v4"
)

// Mode defines the configured vault behavior relevant to note ownership.
type Mode uint8

const (
	// ClassicVault preserves the existing Markdown-only vault default.
	ClassicVault Mode = iota
	// CollectionVault applies configured includes relative to the vault root.
	CollectionVault
)

// Owner is the sole durable classification for a path at this boundary.
type Owner uint8

const (
	Ignored Owner = iota
	Note
	Code
	Unowned
)

// IgnoreFunc is a precomputed, caller-owned ignore decision. Keeping it
// outside this package lets application composition use unified filesystem
// ignore rules without making ownership classification perform I/O.
type IgnoreFunc func(paths.RelPath) bool

// Config contains the syntax-neutral inputs needed to compile a plan.
// Includes retain their existing doublestar syntax. Ignore is optional and is
// evaluated before provider selection when supplied.
type Config struct {
	Mode     Mode
	Includes []string
	Ignore   IgnoreFunc
	Registry noteformat.Registry
}

// Decision describes one exclusive ownership result. Provider is populated
// only for note-owned paths; authored path spelling is preserved.
type Decision struct {
	Path     paths.RelPath
	Owner    Owner
	Provider noteformat.FormatID
}

// Plan is an immutable configured ownership classifier.
type Plan struct {
	mode     Mode
	includes []string
	ignore   IgnoreFunc
	registry noteformat.Registry
}

// Compile validates collection include syntax and freezes the configured
// ownership inputs. It never walks the filesystem.
func Compile(cfg Config) (*Plan, error) {
	if cfg.Mode != ClassicVault && cfg.Mode != CollectionVault {
		return nil, fmt.Errorf("unknown note discovery mode %d", cfg.Mode)
	}
	if len(cfg.Registry.IDs()) == 0 {
		return nil, fmt.Errorf("note format registry must not be empty")
	}
	defaultProviders := 0
	var defaultDescriptor noteformat.Descriptor
	for _, id := range cfg.Registry.IDs() {
		provider, found := cfg.Registry.Provider(id)
		if found && provider.Descriptor().OwnershipPolicy == noteformat.OwnershipDefault {
			defaultProviders++
			defaultDescriptor = provider.Descriptor()
		}
	}
	if defaultProviders != 1 {
		return nil, fmt.Errorf("note format registry must declare exactly one default ownership provider")
	}

	includes := append([]string(nil), cfg.Includes...)
	if cfg.Mode == CollectionVault && len(includes) == 0 {
		includes = defaultIncludePatterns(defaultDescriptor.Extensions)
	}
	if cfg.Mode == CollectionVault {
		for _, pattern := range includes {
			pattern = filepath.ToSlash(strings.TrimSpace(pattern))
			if pattern == "" {
				return nil, fmt.Errorf("note include pattern is required")
			}
			if _, err := doublestar.Match(pattern, ""); err != nil {
				return nil, fmt.Errorf("invalid note include %q: %w", pattern, err)
			}
		}
	}

	return &Plan{
		mode:     cfg.Mode,
		includes: includes,
		ignore:   cfg.Ignore,
		registry: cfg.Registry,
	}, nil
}

func defaultIncludePatterns(extensions []string) []string {
	patterns := make([]string, 0, len(extensions))
	for _, extension := range extensions {
		extension = strings.ToLower(strings.TrimSpace(extension))
		if extension != "" {
			patterns = append(patterns, "**/*"+extension)
		}
	}
	sort.Strings(patterns)
	return patterns
}

// Classify returns exactly one owner. Accepted note ownership wins over the
// caller's codeCandidate result, so a configured note cannot enter both lanes.
func (p *Plan) Classify(rel paths.RelPath, codeCandidate bool) (Decision, error) {
	if p == nil {
		return Decision{}, fmt.Errorf("note ownership plan is required")
	}
	clean, err := paths.CleanRelPath(rel.String())
	if err != nil || clean == "" {
		if err == nil {
			err = paths.ErrOutsideVault
		}
		return Decision{}, err
	}

	decision := Decision{Path: clean}
	if p.ignore != nil && p.ignore(clean) {
		decision.Owner = Ignored
		return decision, nil
	}

	provider, found := p.registry.ProviderForPath(clean)
	if found && p.noteOwns(provider, clean) {
		decision.Owner = Note
		decision.Provider = provider.Descriptor().ID
		return decision, nil
	}
	if codeCandidate {
		decision.Owner = Code
		return decision, nil
	}
	decision.Owner = Unowned
	return decision, nil
}

func (p *Plan) noteOwns(provider noteformat.Provider, rel paths.RelPath) bool {
	descriptor := provider.Descriptor()
	switch descriptor.OwnershipPolicy {
	case noteformat.OwnershipDefault:
		return p.mode == ClassicVault || p.matchesAnyInclude(rel, descriptor.Extensions)
	case noteformat.OwnershipExplicitInclude:
		return p.mode == CollectionVault && p.matchesExplicitInclude(rel, descriptor.Extensions)
	default:
		return false
	}
}

func (p *Plan) matchesAnyInclude(rel paths.RelPath, providerExtensions []string) bool {
	for _, pattern := range p.includes {
		matched, _ := doublestar.Match(filepath.ToSlash(pattern), rel.String())
		if matched {
			return true
		}
		if matchesClaimedTerminalExtension(pattern, rel, providerExtensions, false) {
			return true
		}
	}
	return false
}

func (p *Plan) matchesExplicitInclude(rel paths.RelPath, providerExtensions []string) bool {
	for _, pattern := range p.includes {
		if MatchesExplicitInclude(pattern, rel, providerExtensions) {
			return true
		}
	}
	return false
}

// MatchesExplicitInclude reports whether a pattern explicitly authorizes a path
// using only extensions claimed by its provider. Broad globs do not authorize it.
func MatchesExplicitInclude(pattern string, rel paths.RelPath, providerExtensions []string) bool {
	return matchesClaimedTerminalExtension(pattern, rel, providerExtensions, true)
}

func matchesClaimedTerminalExtension(pattern string, rel paths.RelPath, providerExtensions []string, requireAllClaims bool) bool {
	path := rel.String()
	extension := strings.ToLower(filepath.Ext(path))
	claimed := make(map[string]struct{}, len(providerExtensions))
	for _, providerExtension := range providerExtensions {
		claimed[strings.ToLower(providerExtension)] = struct{}{}
	}
	if _, ok := claimed[extension]; !ok {
		return false
	}
	extensions, normalized, explicit := explicitTerminalExtensions(pattern)
	if !explicit {
		return false
	}
	if _, selected := extensions[extension]; !selected {
		return false
	}
	if requireAllClaims && !extensionsAreClaimed(extensions, claimed) {
		return false
	}
	normalizedPath := path[:len(path)-len(filepath.Ext(path))] + extension
	matched, _ := doublestar.Match(normalized, normalizedPath)
	return matched
}

func extensionsAreClaimed(extensions, claimed map[string]struct{}) bool {
	for extension := range extensions {
		if _, ok := claimed[extension]; !ok {
			return false
		}
	}
	return true
}
