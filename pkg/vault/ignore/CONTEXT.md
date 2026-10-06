## pkg/vault/ignore

Unified gitignore-style pattern matching for vault file exclusion.

- **Entry point**: `LoadUnifiedMatcher(root, userPatterns)`; `LoadMatcherWithRhizomeLines` takes `.rhizome/ignore` lines a caller has not written yet, such as a first run's plan.
- **Rule listing**: `Matcher.Rules` lists every rule by layer in evaluation order. Nested `.gitignore` files come from one walk that prunes hidden and ignored directories, as discovery does, so a `.gitignore` that never applies is not listed.
- **Key invariant**: all visibility filtering uses this; go-git matcher; last rule wins; ancestor dirs checked too. Exact-case `CONTEXT.md` is system context: callers omit ordinary note-selection excludes for it, while the resulting Git/Rhizome matcher and built-in infrastructure directories remain authoritative.

### Deep docs

- [[Ignore + exclude rules]]
