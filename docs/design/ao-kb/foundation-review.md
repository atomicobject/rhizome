# SPEC-0133 foundation review, round 1

Reviewed on 2026-11-06: `foundation-design.md`, `ownership-design.md` (Lane B, commit `ce8625104`), and `binding-compiler-design.md`. Both reviewers returned ACCEPT WITH CHANGES: Sol 7/10, Fable 8/10. The coordinator accepted every finding below. The Batch 1 implementation lane revises `foundation-design.md` (and the compiler design where noted) to match before building on it.

## Accepted changes to the structural and binding design

| ID | Change |
| --- | --- |
| Sol S1 | Add a narrow runtime composition contract for account and product records. AO filters account rows by product (`account.py:233`) and composes effective documents (`:266`); paths and selectors cannot exclude another product's rows inside a shared account file. Prefer a bounded declarative membership rule evaluated by core from each row's own values at index time (so ordinary refresh recomputes it and account changes invalidate affected projections), plus explicit source order for account-first positional numbering. Mixed-origin row detection stays in the starter check. Name the producer and consumer. Do not port AO merge semantics into Go. |
| Sol S2 | AO emits `container -> child` `made-of` edges independently (`metamodel.py:2146`). Add an optional explicit edge to RECORD containment, for example `@contains(shape: RECORD, select: "jobs", edge: {name: "made-of"})`, preserved through Lane M's intermediate. Ordinary containment stays edge-free. |
| Sol S4 | `pkg/ontology/noderead/provider_snapshot.go:56` assigns the persisted metadata hash to freshly read bytes. Derive the root hash from the captured source's `ContentHash()` and add a stale-index fixture that changes the host before indexing and verifies positional rejection. |
| Sol S5 | AO `type: any` reference fields (for example `transition.event`, `standard-metamodel.yaml:779`) accept a scalar, list elements, or mapping keys depending on the actual value (`_targets`). Add one bounded shape-dependent occurrence mode, explicit in SDL and the intermediate. |
| Sol S6 | `pkg/app/web/node_preview.go:118` reserves only `struct:` and `node:`. Use shared reserved-selector recognition for `record:` and `record-pos:`, keep their diagnostics, and add missing and stale record-link preview cases. |
| Fable F1 | Add `pkg/validate/identifier_repair_mapper_fields.go:44,58` and `pkg/validate/identifierreconcile/field_discovery_build.go:162,166,181` to the consumer inventory; route them through the Markdown identifier-repair adapter or neutral owner-scoped bindings before deleting generic snapshot APIs. |
| Fable F2 | Positional (`key: null`) NodeID must not derive from the whole source hash, or every host edit churns every positional node. Keep the source hash only in the `record-pos:` locator fragment. Derive positional NodeID from host path, binding identity, parent physical identity, and ordinal; put a row-content fingerprint in `Structural`, as unanchored Markdown sections do. Sol's "invalidate on any host-byte change" ruling applies to the locator. |
| Fable F3, coordinator | One owner-local path spelling. Drop the proposed `@value` directive. RECORD fields reuse Lane D's merged `@field(path: "provenance.speaker")` spelling (literal `source:` keys unchanged); express the mapping-key fallback within `@field` rather than a second directive. |
| Fable F4 | Binding publication: render the complete binding set to one SDL file plus one manifest, each written temp-and-rename. Existing schema loading needs no multi-generation loader. Keep the previous generation only as a diagnostic copy. |
| Fable F5 | Read-time staleness: the starter surfaces it. The Knowledge workspace and record views read the manifest status and `check` reports it. Core gains no seam this milestone. This is the agreed R4 interpretation. |
| Fable F6 | `groups{}[]` key rule: AO `_leaf_id` (`metamodel.py:1293`) uses the mapping key whenever the last selector part does not end in `{}[]`, so `a{}[].b` keys rows by the group. The compiler emits key policy from AO's rule on the last part; fix the selector table. |
| Fable F7 | Parity oracle: AO `edges()` emits edges to unresolved `ref`/`ref[]`/`parent`/`variants` targets. Subtract AO edges whose target is not in `rows_by_kind` and assert the matching Rhizome diagnostic. |
| Fable F8 | Add YAML 1.1 sexagesimal integers (`1:30`) and underscore digits (`1_000`) to the pinned scalar profile fixture, normalized or explicitly unsupported. |
| Fable F9 | `account._merge` (`account.py:283`) emits product keys first and appends account-only keys; `{}` expansions order product-first while lists concatenate account-first. Fix the stale `product.py:218` anchor (`mirror_config` is at `:193`). |
| Fable, generated overlays | `_overlay_generated_fields` runs inside every AO reader. An overlaid field is a compatibility note on the affected kind, not setup incomplete, unless a binding requires that field. Parity fixtures exclude the overlaid field by name. |

## Ownership findings (sent to Lane B)

- Sol S3 (blocker): `rzm note create --append/--overwrite` dispatches through an Obsidian URI and bypasses admission (`pkg/app/operations/create.go:18`). Admit the destination before dispatch; tests assert the URI never runs.
- Fable F10: load the ownership declaration once per operation or read scope instead of per path; stay fail-closed.

## Rulings on the open decisions

1. Explicit RECORD SDL: accepted with the changes above.
2. Vendored `gopkg.in/yaml.v3` in Node mode with a pinned, provider-local scalar profile: accepted. The pinned AO oracle must establish scalar compatibility before parity is claimed.
3. Positional invalidation: accepted for locators; node identity follows F2.
4. Typed-reference member qualifiers on relation occurrences and read-model edge rows: accepted. Preserve each occurrence's qualifier before edge deduplication.
5. Generated overlays presented separately, flattened rows diagnosed: accepted with Fable's adjustment above.
