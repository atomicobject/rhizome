package cache

import (
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/ignore"
)

// NoteAdmissionFunc decides whether a canonical vault-relative path belongs in
// this syntax projection cache. It runs before the cache reads file content.
type NoteAdmissionFunc func(paths.NotePath) bool

// SelectionPolicy defines the cache's selected projection inputs.
// Discovery may narrow the filesystem crawl. Admit remains the authorization
// boundary for every crawl and incremental refresh path.
type SelectionPolicy struct {
	DiscoverFiles FileDiscoveryFunc
	Admit         NoteAdmissionFunc
	UserExcludes  []string
}

func normalizeSelectionPolicy(policy SelectionPolicy) SelectionPolicy {
	if policy.Admit == nil {
		policy.Admit = MarkdownCompatibilityAdmission
	}
	policy.UserExcludes = append([]string(nil), policy.UserExcludes...)
	return policy
}

// MarkdownCompatibilityAdmission is the narrow legacy adapter for callers
// that use this cache only as a Markdown syntax projection. Live runtime
// composition must inject an ownership-derived admission function instead.
func MarkdownCompatibilityAdmission(path paths.NotePath) bool {
	return paths.NormalizeNote(path.String()) == path
}

func (s *Service) admitsNotePath(rel string) bool {
	notePath, err := paths.CleanNotePath(rel)
	if err != nil {
		return false
	}
	s.mu.RLock()
	admit := s.selection.Admit
	s.mu.RUnlock()
	return admit(notePath)
}

// AdmitsNotePath reports the active, injected selection decision for a
// vault-relative path. Event consumers use this instead of inferring note
// ownership from a filename extension.
func (s *Service) AdmitsNotePath(rel string) bool {
	return s.admitsNotePath(rel)
}

func (s *Service) discoveryFunc() FileDiscoveryFunc {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.selection.DiscoverFiles
}

// ReplaceSelectionPolicy atomically replaces discovery and admission rules.
// The following refresh performs a complete resync, evicting paths the new
// policy rejects and discovering newly selected paths.
func (s *Service) ReplaceSelectionPolicy(policy SelectionPolicy) {
	policy = normalizeSelectionPolicy(policy)
	matcher := ignore.LoadUnifiedMatcher(s.vaultPath, policy.UserExcludes)
	hardMatcher := ignore.LoadUnifiedMatcher(s.vaultPath, nil)
	s.mu.Lock()
	s.selection = policy
	s.selectionVersion++
	s.ignoreMatcher = matcher
	s.hardIgnoreMatcher = hardMatcher
	s.stale = true
	s.mu.Unlock()
	s.metrics.staleFlips.Add(1)
}
