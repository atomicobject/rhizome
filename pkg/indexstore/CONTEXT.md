## pkg/indexstore

`Open` is the singular high-level configured index opener. Pass a preloaded code configuration plus explicit ReadOnly/ReadWrite authority and create/repair, optional vault, enablement, context, lock, and pool policy. A nil config resolves repository settings at this boundary.

Missing optional paths or absent existing indexes return nil. Config, access, schema, and repair errors propagate. Callers own cleanup. Embedding repair attempts preservation and never unlinks a live database or suppresses a failed repair.

Low-level schema/domain construction stays in the store packages. Keep this owner neutral: it must not import application orchestration.

ReadOnly uses OpenReadOnlyExisting and rejects create, repair, and write-lock selection. Missing existing indexes remain absent; incompatible schemas return indexing guidance without migration or recovery. ReadWrite is reserved for indexing, runtime writers, metadata/graph maintenance, and commands that explicitly index files.
