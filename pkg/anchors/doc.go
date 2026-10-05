// Package codeanchor binds vault notes to code and materializes the code-intel
// facts that make those bindings retrievable.
//
// The package has two related responsibilities:
//
//   - Code anchors are note -> code selectors declared in note frontmatter.
//     They match definitions, call sites, annotation/decorator uses, path
//     prefixes, or globs, then make the owning notes surface from file_context.
//   - Code intel rows are the retrieval-visible spine: file/module anchors,
//     symbol anchors, typed edges, FTS rows, doc links, rationale comments, and
//     reverse-index rows used by search, graph, and agent context.
//
// Persisted code paths must be vault-root-relative and slash-normalized through
// pkg/paths. FQNs are language-indexer output, not Go import paths in general;
// changing an indexer's FQN shape changes anchor authoring and search handles, so
// bump IndexerVersion when it affects persisted output.
//
// Batch indexing intentionally defers expensive resolver-backed edges and scope
// recomputation until the post-write phase. Keep the hot ingest path parse ->
// normalize -> build work -> batch persist; per-file database lookups here are a
// common performance trap.
package codeanchor
