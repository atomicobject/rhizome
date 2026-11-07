package ontology

// The external AO KB convergence test also runs GraphQL reads, which this
// package cannot import, so it reuses the structured publication harness here.
var (
	SyncPathsForTest          = syncPathsForTest
	StructuredSnapshotForTest = structuredSnapshot
)
