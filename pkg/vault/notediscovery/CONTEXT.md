# Note discovery

`notediscovery` is the format-neutral ownership boundary for a configured vault
path. It compiles note includes, a caller-provided ignore decision, and the
closed note-format registry into exactly one decision: ignored, note, code, or
unowned.

It does not walk the filesystem, load ignore files, read source bytes, parse
note syntax, select a code language, mutate caches, or persist rows. Application
composition owns `ignore.LoadUnifiedMatcher` and supplies an `IgnoreFunc`.
Filesystem adapters also retain legacy dotfile and hidden-directory traversal
exclusion before calling the plan; that filter is separate from unified ignore.
Callers provide a clean vault-relative `paths.RelPath` and whether their code
lane could own it. A configured accepted note wins over that code candidate.

Provider descriptors declare whether they own by default or only after an
explicit include. The built-in Markdown provider is default-owned; built-in
HTML is explicit-include-owned. An explicit provider enables only through a
terminal literal extension pattern whose entire extension set belongs to that
provider; broad globs, character classes, suffix wildcards, and mixed-provider
brace alternatives fail closed. Extension comparison is case-insensitive for
Markdown too (`.md`, `.MD`, and mixed case), while all other include path
matching retains existing doublestar semantics. HTML upgrade safety prevents
accidental `.html`/`.htm` activation; it does not preserve case-sensitive
Markdown extension behavior.

For a collection with no configured includes, the plan derives deterministic
`**/*<extension>` patterns from the sole default provider's claimed extensions;
the built-in result remains `**/*.md` without embedding that extension in this
package.
