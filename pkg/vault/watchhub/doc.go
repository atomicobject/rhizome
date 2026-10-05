// Package watchhub turns noisy filesystem backends and follower hint logs into
// debounced vault-relative change events.
//
// The public boundary is intentionally vault-root-relative: subscribers should
// treat WatchEvent.RelPath as the stable key for cache/index invalidation and
// AbsPath as an I/O convenience only. The hub owns backend differences
// (recursive FSEvents vs per-directory fsnotify), ignore-file invalidation,
// stale/resync signaling, and event deduplication; downstream packages decide
// what a changed path means for their own state.
//
// Exact-case CONTEXT.md events bypass collection note excludes, but not
// Git/Rhizome ignore rules or built-in infrastructure-directory boundaries.
package watchhub
