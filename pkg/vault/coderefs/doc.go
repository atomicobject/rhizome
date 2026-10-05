// Package coderefs extracts and rewrites code -> note links from comments and
// docstrings.
//
// Coderefs complement code anchors: anchors make notes appear for code, while
// coderefs make code point back to note/spec/decision context. The scanner is
// intentionally comment-only and resolver-backed, so ordinary strings and
// unresolved prose do not become retrieval-visible links.
//
// Scanning is permissive enough to find author intent, but rewriting is stricter
// because it mutates source files during note moves. Keep that asymmetry: broad
// discovery can tolerate false negatives/lookup misses, while rename rewriting
// must avoid partial replacements such as @Note inside @NoteExtra.
package coderefs
