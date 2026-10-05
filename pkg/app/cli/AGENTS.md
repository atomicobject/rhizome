# Test ownership

- Context dedupe tests get their seen state from a real first delivery and assert the second delivery's visible output.
- Similarity tests use persisted source and candidate rows, including nonfunction sources and out-of-root candidates.
- List, info, tags, and properties tests supply projected facts explicitly. Do not derive facts or expected output from mock call declarations.
- Indexed context tests exercise the public builder with a supplied store. Prove read-only behavior with divergent persisted data and observable output or unchanged store state, not a mutable loader alias.
- Keep one owner test for each graph, backlink, and bootstrap contract; carry assertions into that test before retiring a lower-level replay.
- Note mutation keepers compare exact output bytes. A line-set comparison hid heading rename deleting the blank line after the heading.
- Edge dedupe is only observable when one request names both ends of an edge; single-endpoint fixtures cannot catch a duplicate neighbor.
