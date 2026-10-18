package validate

import (
	"context"
	"testing"

	ontologytest "github.com/atomicobject/rhizome/internal/testkit/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/stretchr/testify/require"
)

// preparedSuite makes fixture preparation explicit while production only
// consumes runtimes owned by its projection or indexing caller.
func preparedSuite(t *testing.T, ctx context.Context, options ScanOptions) (Result, RunContext, error) {
	t.Helper()
	if options.RunContext == nil || options.RunContext.NoteMetadata.Validate() != nil || ctx.Err() != nil {
		return RunSuiteOncePrepared(ctx, options, nil, nil)
	}
	checks, err := ResolveChecks(options.Checks)
	if err != nil {
		return Result{}, RunContext{}, err
	}
	journals, err := DetectPendingRepairJournals(*options.RunContext)
	require.NoError(t, err)
	if len(journals) > 0 {
		return RunSuiteOncePrepared(ctx, options, nil, nil)
	}
	var runtime *ontology.Runtime
	var runtimeErr error
	if registryNeedsOntologyRuntime(checks) {
		run := options.RunContext
		store := openTestStore(t, run.VaultPath)
		_, err := run.NoteMetadata.EnsureIndexed(ctx, run.VaultDef, run.NoteReader, store)
		require.NoError(t, err)
		runtime, runtimeErr = ontologytest.Publish(ctx, run.NoteMetadata, run.VaultDef, run.NoteReader, store)
	}
	return RunSuiteOncePrepared(ctx, options, runtime, runtimeErr)
}

func preparedCheck(t *testing.T, ctx context.Context, run RunContext, options ScanOptions, check string) CheckResult {
	t.Helper()
	options.RunContext = &run
	options.Checks = []string{check}
	result, _, err := preparedSuite(t, ctx, options)
	require.NoError(t, err)
	require.Len(t, result.Checks, 1)
	return result.Checks[0]
}
