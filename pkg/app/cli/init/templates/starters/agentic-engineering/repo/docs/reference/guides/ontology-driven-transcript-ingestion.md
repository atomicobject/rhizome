---
type: ReferenceDoc
summary: "How transcript synthesis derives its output tracks from the ontology instead of a fixed lens catalog."
reference-kind: guide
last-verified: 2026-09-07
status: active
---

# Ontology-driven transcript ingestion

## Summary

Transcript ingestion in a Rhizome-powered repo should not start from a static lens list. Ask which existing durable note families could responsibly hold the source's useful evidence, then inspect only the likely destinations.

## Core idea

The ontology carries most of the semantics a transcript workflow needs:

- docstrings provide the concise summary of what each note family and field is for
- layered guidance carries deeper meaning, authoring discipline, and agent-preservation implications
- companion docs extend the schema with workflow and vocabulary guidance
- the executable query schema exposes query entry points and relation names when a typed query is needed

That means the "analysis lenses" are mostly implicit in the ontology:

- specs ask what behavior should be frozen
- decisions ask what rationale or tradeoff should be preserved
- reference docs ask what supporting context should become durable
- domain notes ask what vocabulary, state model, or mapping needs to be remembered
- process specs ask what repeatable workflow changed

## Practical routing heuristics

Use these as ontology-backed routing questions, not a replacement for the schema:

- If the transcript resolves intended user-visible behavior or scope, it likely belongs in a spec.
- If it records a durable rationale, policy, or architectural tradeoff, it likely belongs in a reference note.
- If it preserves supporting context, source mappings, personas, terminology, or comparative analysis, it likely belongs in a reference note.
- If it changes how the team should repeatedly operate, it may belong in a team policy document or a deliberately modeled process spec.

The ontology governs note shapes and available destinations. It does not establish whether a source claim is true or accepted. These heuristics only help frame the first survey.

## Working approach

Preserve the source and provenance. Use the core `rhizome` skill's discovery mechanics to inspect authoring guidance for likely target types and query existing notes that may already own the material. Deepen to schema inspection or broader discovery only when the destination or relationships remain unclear.

Separate extracted evidence, candidate interpretation, and accepted commitments. An explicit request to save or update the source authorizes in-scope durable writes; it does not turn uncertain claims into accepted requirements. Write the smallest useful update, validate it, and index affected context through the base workflow.

## Duplicate sources and reruns

Check for an existing copy before storing a source. Keep one canonical artifact per meeting or source event, as `docs/meetings/README.md` describes. Look for a stored artifact with the same date and subject, and compare content with a checksum such as `shasum -a 256` when a likely match exists. Then search for notes whose `derived-from` names that artifact.

- If the same source is already stored, reuse its path and do not store a second copy.
- If a longer or corrected version of a stored source arrives, replace the stored artifact in place and say what changed.
- If the source was already ingested, compare the new synthesis with the notes derived from it. Update only what changed, keep their `derived-from` links, and report claims that now conflict instead of silently overwriting them.

## Query-schema contract

`rzm agent ontology-query-schema` should be treated as the executable schema contract for transcript workflows.

It includes:

- generated root query entry points for concrete note types on `Query`
- generic `note` / `notes` escape hatches
- GraphQL descriptions derived from ontology type and field docstrings as the concise summary channel
- the actual relation names and field shapes agents can query

Use it before writing GraphQL when a typed query is needed. Never guess root names or relation names from memory.

## When to stop and revise the ontology

Load the `rhizome` ontology-authoring guidance and surface the modeling gap when:

- the transcript clearly wants to update a note family that does not exist
- the existing type contracts force the evidence into the wrong layer
- the authoring guide for the likely destination is missing critical workflow semantics

Do not force durable knowledge into an arbitrary note type. Changing the ontology or accepting a new commitment requires the authority appropriate to that change.
