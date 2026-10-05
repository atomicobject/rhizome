# Notemeta test ownership

- Provider-projected metadata and persisted link behavior belong to the live Indexer delta/publication paths with a real store. Do not recreate retired snapshot or link-cache adapters for tests.
- Alias shape compatibility for raw filesystem consumers belongs to `BuildNoteSourceFacts`; provider projection overwrites aliases and cannot prove that converter. Establish source presence before asserting empty alias values.
- Alias removal tests start with a persisted edge and assert its removal while an unrelated edge survives. Canonical scalar normalization expectations use independent literal values.
