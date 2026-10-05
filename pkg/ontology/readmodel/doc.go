// Package readmodel defines the storage-neutral ontology graph rows that
// SQLite returns and noderead merges into endpoint-aware graph payloads.
//
// Keep this package as DTOs plus store interfaces. It must not learn browser,
// search, or query profile semantics; those live in noderead so all consumers
// share the same endpoint-scoping and fallback rules.
package readmodel
