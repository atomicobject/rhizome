// Package unifiedsearch adapts CLI/MCP search requests into the shared search
// pipeline and answer packet.
//
// It owns provider/store bootstrap, seed normalization, query facet fan-out,
// target-resolution warnings, diagnostics wiring, and conversion from ranked
// search results into pkg/app/answer inputs. Retrieval and ranking remain in
// pkg/search; answer assembly remains pure role selection over already-ranked
// evidence.
package unifiedsearch
