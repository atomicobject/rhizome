## pkg/vault/health

Live link and fragment health checks, note read diagnostics, dead ends, stale notes, and merge suggestions. Shared syntax and heading slugs come from `pkg/markdownsyntax`; cached note and filesystem resolution come from `pkg/vault/obsidian`; dead-end analysis consumes `pkg/graph`.

Bounded workers honor cancellation and retain exact source/target read failures. Fragment checks read a canonical target once, including ignored Markdown fallback targets. Health policy never changes note admission or graph identity.
