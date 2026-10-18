## pkg/markdownsyntax

Pure Markdown grammar with no Rhizome package dependencies: frontmatter boundaries, code protection, wiki/Markdown/reference links, sealed source spans, identifier review scans, headings/block targets, tags, inline fields, and plain titles.

`ProtectedSpans` and `CodeMask` protect code only. Tags, inline fields, headings, and titles explicitly exclude frontmatter; link scans retain valid frontmatter property links. Structured snapshots seal exact source fingerprints and component spans, including empty results. `StructuredLink.ResolverInput` carries authored link syntax to the separate cached resolver without reparsing it here.

`EnumerateMarkdownTargets` is the heading/block grammar shared by projection and health. `HeadingSlug` supplies the Unicode letter/digit slug used in section identity and returns empty for headings without letters or digits. Parsers retain authored block IDs before generating a heading identity. `MarkdownHeadingSlug` retains the separate renderer-fragment spelling used by provider-fact validation. Parsing never reads a vault, opens a store, discovers config, or invokes an application service.

`NextLineStart`, `ByteRun`, and `FenceLine` are shared lexical scanning primitives. `FenceLine` recognizes delimiter candidates with up to three leading spaces; slot and template scanners own their opening/closing state. These helpers do not apply document-level code protection on their own.
