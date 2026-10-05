## pkg/vault/coderefs

Extract and rewrite **code → note** references (coderefs) from comments/docstrings.

- **Entry points**: `ScanFile`, `RewriteBatch`
- **Key invariant**: coderefs are code→note; resolution uses canonical `NotePathCache` results; links use `ScanCommentLinks` spans, while mention rewrites retain stricter boundaries than scanning
- **Rename syntax**: collect parsed link and standalone mention edits from original spans once per mapping, matching all old-path alternatives and counting shared mention spans once; separate mappings remain sequential. Reuse the structured scan while a comment block is unchanged, and rescan after an edit before applying a later mapping. Parsed links own their labels and destinations, so embedded `@` text never adds a mention or receives a mention rewrite. Preserve scanner-supported mentions and representable wiki paths; otherwise emit a full canonical wiki or URL-encoded Markdown destination that rescans and can be renamed again. Unrepresentable wiki embeds become normal Markdown links; authored Markdown images are never rewritten
- **Comment examples**: explicit wiki and Markdown links inside extracted comment backticks/fences are coderefs. Normal note scans still protect code spans
- **Comment routing**: extension registry maps formats to lightweight comment families; web/template files stay coderef-only (no anchors)

### Deep docs

- [[Coderefs (Hub)]]
