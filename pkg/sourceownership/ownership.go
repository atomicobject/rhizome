// Package sourceownership enforces configured authoring ownership independently
// of note formats, schemas, projections, and index freshness.
package sourceownership

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"
)

const DeclarationPath = ".rhizome/source-ownership.yml"

// Generation follows the declaration independently of index writes or configuration reloads.
func Generation(root string) string {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(DeclarationPath)))
	return fmt.Sprintf("%x", sha256.Sum256(append([]byte(fmt.Sprint(err)), data...)))[:12]
}

type Source struct {
	Owner  string   `yaml:"owner"`
	Reason string   `yaml:"reason"`
	Paths  []string `yaml:"paths"`
}

type Declaration struct {
	Sources []Source `yaml:"sources"`
}

// Refusal is shared by native writers and capability reporting.
type Refusal struct{ Path, Owner, Reason string }

func (e *Refusal) Error() string {
	return fmt.Sprintf("%s is read-only (%s): %s", e.Path, e.Owner, e.Reason)
}

// Load reads only the ownership document. Invalid declarations fail closed;
// projection refreshes never write or clear this independent configuration.
func loadDeclaration(root string) (*Declaration, error) {
	vault, err := paths.NewVaultPaths(root)
	if err != nil {
		return nil, err
	}
	if vault.Root() == "" {
		return &Declaration{}, nil
	}
	data, err := os.ReadFile(filepath.Join(vault.Root(), filepath.FromSlash(DeclarationPath)))
	if os.IsNotExist(err) {
		return &Declaration{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read source ownership: %w", err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var declaration Declaration
	if err := decoder.Decode(&declaration); err != nil {
		return nil, fmt.Errorf("invalid source ownership: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("source ownership requires one YAML document")
	}
	for i, source := range declaration.Sources {
		if strings.TrimSpace(source.Owner) == "" || strings.TrimSpace(source.Reason) == "" || len(source.Paths) == 0 {
			return nil, fmt.Errorf("source ownership entry %d requires owner, reason, and paths", i+1)
		}
		for _, pattern := range source.Paths {
			clean, err := paths.CleanRelPath(pattern)
			if err != nil || strings.TrimSpace(pattern) == "" || clean.String() != pattern || strings.Contains(pattern, "\\") || !doublestar.ValidatePattern(pattern) {
				return nil, fmt.Errorf("invalid source ownership path %q", pattern)
			}
		}
	}
	return &declaration, nil
}

// Policy captures one declaration, including load failures, for a read scope
// or admission boundary. A new commit must load its own current policy.
type Policy struct {
	vault       paths.VaultPaths
	declaration *Declaration
	err         error
	links       *paths.SymlinkCache
}

func Load(root string) *Policy {
	policy := &Policy{}
	policy.vault, policy.err = paths.NewVaultPaths(root)
	if policy.err == nil {
		policy.declaration, policy.err = loadDeclaration(root)
	}
	return policy
}

// ForReads returns a copy for one read request that shares links' symlink
// resolutions across every path it checks. Write admission keeps the original
// policy, which resolves fresh, because a link can be retargeted without any
// indexed or configuration change.
func (p *Policy) ForReads(links *paths.SymlinkCache) *Policy {
	read := *p
	read.links = links
	return &read
}

// Check loads current ownership for one complete write-admission boundary.
func Check(root string, affected []string) error { return Load(root).Check(affected) }

// Check admits the entire affected file set against this captured declaration.
func (p *Policy) Check(affected []string) error {
	if len(affected) == 0 {
		return nil
	}
	if p.err != nil {
		return p.err
	}
	if p.vault.Root() == "" {
		return nil
	}
	for _, input := range affected {
		if input == "" {
			continue
		}
		path, err := p.vault.RelStrict(input)
		if err != nil {
			return err
		}
		// Resolve parent aliases even when the destination does not exist yet.
		physical, err := p.vault.RelPhysicalStrictCached(paths.ResolveAgainst(p.vault.Root(), input), p.links)
		if err != nil {
			return err
		}
		for _, candidate := range []string{path.String(), physical.String()} {
			if paths.CaseKey(candidate) == paths.CaseKey(DeclarationPath) {
				return &Refusal{Path: input, Owner: "source ownership configuration", Reason: "Change source ownership explicitly in repository configuration."}
			}
			for _, source := range p.declaration.Sources {
				for _, pattern := range source.Paths {
					if matched, _ := doublestar.Match(paths.CaseKey(pattern), paths.CaseKey(candidate)); matched {
						return &Refusal{Path: input, Owner: source.Owner, Reason: source.Reason}
					}
				}
			}
		}
	}
	return nil
}

// ReadOnlyReason gives clients the same refusal reason as server admission.
func ReadOnlyReason(root, path string) string {
	return Load(root).ReadOnlyReason(path)
}

func (p *Policy) ReadOnlyReason(path string) string {
	err := p.Check([]string{path})
	if refusal, ok := err.(*Refusal); ok {
		return refusal.Reason
	}
	if err != nil {
		return err.Error()
	}
	return ""
}
