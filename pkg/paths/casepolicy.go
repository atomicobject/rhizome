package paths

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/bmatcuk/doublestar/v4"
	"golang.org/x/text/unicode/norm"
)

// CasePolicy says how one vault root's filesystem compares relative path
// spellings. It decides membership and lookup only: stored paths keep the
// spelling their directory entries have, so folding never produces a key that
// two distinct files share as identity. The zero value is exact byte
// comparison, which is also the answer when the filesystem gives no evidence.
type CasePolicy struct{ fold bool }

var (
	// CaseExact compares spellings byte for byte.
	CaseExact = CasePolicy{}
	// CaseFold treats spellings that differ only in letter case as the same
	// file, the behavior of a case-insensitive directory.
	CaseFold = CasePolicy{fold: true}
)

// Key returns the comparison form of a relative path or glob pattern.
// Folding uses simple lowercase mapping, not full Unicode case folding, and
// never normalizes composition, so distinct Unicode spellings stay distinct.
func (p CasePolicy) Key(rel string) string {
	if p.fold {
		return strings.ToLower(rel)
	}
	return rel
}

// Equal reports whether two relative spellings name the same path under p.
func (p CasePolicy) Equal(a, b string) bool { return p.Key(a) == p.Key(b) }

// Match reports whether a doublestar pattern selects rel under p. A pattern
// doublestar rejects matches nothing.
func (p CasePolicy) Match(pattern, rel string) bool {
	ok, err := doublestar.Match(p.Key(pattern), p.Key(rel))
	return err == nil && ok
}

func (p CasePolicy) String() string {
	if p.fold {
		return "fold"
	}
	return "exact"
}

// Case returns the comparison policy of the vault root's filesystem.
func (v VaultPaths) Case() CasePolicy { return CasePolicyFor(v.Root()) }

var casePolicies sync.Map

// CasePolicyFor probes root read-only and reports how its filesystem compares
// letter case. A root with no ASCII letters in its entries, an unreadable
// root, or a missing root is inconclusive and
// compares exactly without caching, so the next call probes again once the
// root has content. The operating system is never consulted.
//
// ponytail: one policy per root. A mount or symlinked folder inside the root
// with the other sensitivity makes globs there over- or under-match; concrete
// references stay correct because ExistingSpelling asks the filesystem per
// component. Probe per directory if that case ever matters.
func CasePolicyFor(root string) CasePolicy {
	root = strings.TrimSpace(root)
	if root == "" {
		return CaseExact
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return CaseExact
	}
	if cached, ok := casePolicies.Load(root); ok {
		return cached.(CasePolicy)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return CaseExact
	}
	names := make(map[string]bool, len(entries))
	for _, entry := range entries {
		names[entry.Name()] = true
	}
	for _, entry := range entries {
		swapped := swapCase(entry.Name())
		if swapped != entry.Name() && names[swapped] {
			casePolicies.Store(root, CaseExact)
			return CaseExact
		}
		if policy, ok := probeCaseEntry(root, entry.Name()); ok {
			casePolicies.Store(root, policy)
			return policy
		}
	}
	return CaseExact
}

func probeCaseEntry(dir, name string) (CasePolicy, bool) {
	swapped := swapCase(name)
	if swapped == name {
		return CasePolicy{}, false
	}
	info, err := os.Lstat(filepath.Join(dir, name))
	if err != nil {
		return CasePolicy{}, false
	}
	other, err := os.Lstat(filepath.Join(dir, swapped))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return CaseExact, true
	case err != nil:
		return CasePolicy{}, false
	case os.SameFile(info, other):
		return CaseFold, true
	default:
		return CaseExact, true
	}
}

func swapCase(name string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		case r >= 'a' && r <= 'z':
			return r - ('a' - 'A')
		default:
			return r
		}
	}, name)
}

// ExistingSpelling returns rel spelled as its directory entries name it. Each
// component takes the exact entry when one exists; otherwise the one sibling
// that the filesystem reports as the same file as the requested spelling. It
// reports false when a component is missing or unreadable, or when two
// siblings both claim the spelling, so a reference never selects one of two
// distinct files. The lookup is read-only.
func (v VaultPaths) ExistingSpelling(rel RelPath) (RelPath, bool) {
	clean, err := CleanRelPath(rel.String())
	if err != nil || clean == "" || v.root == "" {
		return "", false
	}
	dir := filepath.FromSlash(v.root.String())
	parts := strings.Split(clean.String(), "/")
	spelled := make([]string, 0, len(parts))
	for _, part := range parts {
		name, ok := existingEntryName(dir, part)
		if !ok {
			return "", false
		}
		spelled = append(spelled, name)
		dir = filepath.Join(dir, name)
	}
	return RelPath(strings.Join(spelled, "/")), true
}

func existingEntryName(dir, part string) (string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	candidates := make([]string, 0, 1)
	want := norm.NFC.String(part)
	for _, entry := range entries {
		if entry.Name() == part {
			return part, true
		}
		if strings.EqualFold(norm.NFC.String(entry.Name()), want) {
			candidates = append(candidates, entry.Name())
		}
	}
	if len(candidates) == 0 {
		return "", false
	}
	requested, err := os.Lstat(filepath.Join(dir, part))
	if err != nil {
		return "", false
	}
	found := ""
	for _, candidate := range candidates {
		info, err := os.Lstat(filepath.Join(dir, candidate))
		if err != nil || !os.SameFile(requested, info) {
			continue
		}
		if found != "" {
			return "", false
		}
		found = candidate
	}
	return found, found != ""
}
