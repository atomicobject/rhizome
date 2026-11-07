# Structured-record AO oracle

`expected.json` contains synthetic YAML bytes and the unmodified outputs of AO's `rows`, `duplicate_rows`, and `edges` readers at toolkit revision `e9dce951ab4cd6aa9ccc734df96590bde25ccde9`, using PyYAML SafeLoader 6.0.3. A read-only helper captured these in `/private/tmp/y2-ao-oracle`; the helper and its Python environment are disposable and are never run in CI. The Go test consumes only this checked-in expectation.

For valid input, Rhizome preserves the normalized rows and both repeated reference occurrences. AO emits an edge to `row:missing`; the test removes that unresolved target from the edge comparison and requires `record_reference_unresolved`, as required by foundation review F7.

The duplicate case captures AO's last-winner row and `duplicate_rows` count. Rhizome retains both source candidates in its diagnostic context, emits `record_key_duplicate`, and admits no typed edge to the ambiguous key. It does not reproduce AO's last-winner lookup. The broader compiler/provider oracle in `testdata/ao-kb-model` covers effective account/product documents, physical addresses, containment, positional numbering, typed qualifiers, and generated JSON.
