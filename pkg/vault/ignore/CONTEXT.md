## pkg/vault/ignore

Unified gitignore-style pattern matching for vault file exclusion.

- **Entry point**: `LoadUnifiedMatcher(root, userPatterns)`
- **Key invariant**: all visibility filtering uses this; go-git matcher; last rule wins; ancestor dirs checked too. Exact-case `CONTEXT.md` is system context: callers omit ordinary note-selection excludes for it, while the resulting Git/Rhizome matcher and built-in infrastructure directories remain authoritative.

### Deep docs

- [[Ignore + exclude rules]]
