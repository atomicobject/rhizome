package validate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyRepairSessionReturnsPreparedPostcheckResult(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		"target.md": "# Target\n",
		"source.md": "bad extension: [[target.md]]\n",
	}))
	runCtx := linkHygieneRunContext(t, root)
	planned, _, err := RunSuiteOnce(context.Background(), Options{
		Checks: []string{CheckLinkHygiene}, RunContext: &runCtx,
	})
	require.NoError(t, err)
	require.Positive(t, planned.IssueCount)

	postcheck, execution, err := ApplyRepairSession(context.Background(), planned, runCtx, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: &repairPostApplyProbe{t: t},
	})
	require.NoError(t, err)
	require.NotNil(t, execution)
	assert.Zero(t, postcheck.IssueCount)
	assert.True(t, postcheck.OK)
	assert.Same(t, execution, postcheck.FixExecution)
	assert.Contains(t, string(mustReadFile(t, filepath.Join(root, "source.md"))), "[[target]]")
	require.NotEmpty(t, postcheck.Checks, "prepared postcheck must run against refreshed committed bytes despite its own journal")
	assert.Empty(t, postcheck.RepairJournals)
	if postcheck.NextActions != nil {
		for _, action := range postcheck.NextActions.Actions {
			assert.NotEqual(t, "repair_recovery", action.Category)
		}
	}
}

func TestApplyRepairSessionPostchecksOnlyCompletedAndRepairRequiredChecks(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		"target.md": "# Target\n",
		"source.md": "bad extension: [[target.md]]\n",
	}))
	runCtx := linkHygieneRunContext(t, root)
	planned, _, err := RunSuiteOnce(context.Background(), Options{
		Checks: []string{CheckLinkHygiene}, RunContext: &runCtx,
	})
	require.NoError(t, err)
	// Product selection also includes this blocked prerequisite, but no
	// CheckResult exists because it never executed.
	planned.SelectedChecks = append(planned.SelectedChecks, CheckCodeAnchors)

	postcheck, _, err := ApplyRepairSession(context.Background(), planned, runCtx, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: &repairPostApplyProbe{t: t},
	})
	require.NoError(t, err)
	require.Len(t, postcheck.Checks, 1)
	assert.Equal(t, CheckLinkHygiene, postcheck.Checks[0].Name)
	assert.Equal(t, []string{CheckLinkHygiene}, postcheck.SelectedChecks)
}

func TestApplyRepairSessionRejectsMissingOrchestrationBeforeMutation(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		"target.md": "# Target\n",
		"source.md": "bad extension: [[target.md]]\n",
	}))
	runCtx := linkHygieneRunContext(t, root)
	planned, _, err := RunSuiteOnce(context.Background(), Options{
		Checks: []string{CheckLinkHygiene}, RunContext: &runCtx,
	})
	require.NoError(t, err)
	before := mustReadFile(t, filepath.Join(root, "source.md"))

	postcheck, execution, err := ApplyRepairSession(context.Background(), planned, runCtx, Options{
		Fix: true, NonInteractive: true,
	})
	require.ErrorContains(t, err, "post-apply refresher")
	assert.Nil(t, execution)
	assert.Equal(t, planned.IssueCount, postcheck.IssueCount)
	assert.Equal(t, before, mustReadFile(t, filepath.Join(root, "source.md")))
	assert.NoDirExists(t, filepath.Join(root, ".rhizome", "repair-journal"))
}

func TestApplyRepairSessionRejectsDisagreeingRootsBeforeMutation(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		"target.md": "# Target\n",
		"source.md": "bad extension: [[target.md]]\n",
	}))
	runCtx := linkHygieneRunContext(t, root)
	planned, _, err := RunSuiteOnce(context.Background(), Options{
		Checks: []string{CheckLinkHygiene}, RunContext: &runCtx,
	})
	require.NoError(t, err)
	before := mustReadFile(t, filepath.Join(root, "source.md"))
	runCtx.VaultPath = t.TempDir()

	_, execution, err := ApplyRepairSession(context.Background(), planned, runCtx, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: &repairPostApplyProbe{t: t},
	})
	require.ErrorContains(t, err, "roots disagree")
	assert.Nil(t, execution)
	assert.Equal(t, before, mustReadFile(t, filepath.Join(root, "source.md")))
}

func TestApplyRepairSessionRecoversPendingJournalAndReturnsPostcheck(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		"target.md": "# Target\n",
		"source.md": "bad extension: [[target.md]]\n",
	}))
	runCtx := linkHygieneRunContext(t, root)
	planned, _, err := RunSuiteOnce(context.Background(), Options{
		Checks: []string{CheckLinkHygiene}, RunContext: &runCtx,
	})
	require.NoError(t, err)
	retain := errors.New("retain committed repair")
	_, err = ApplyFixPlan(context.Background(), runCtx, planned.FixPlan, Options{
		Fix: true, NonInteractive: true,
		PostApplyRefresher: &repairPostApplyProbe{t: t, refreshErr: retain},
	})
	require.ErrorIs(t, err, retain)

	blocked, _, err := RunSuiteOnce(context.Background(), Options{
		Checks: []string{CheckLinkHygiene}, RunContext: &runCtx,
	})
	require.NoError(t, err)
	require.NotEmpty(t, blocked.RepairJournals)

	postcheck, execution, err := ApplyRepairSession(context.Background(), blocked, runCtx, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: &repairPostApplyProbe{t: t},
	})
	require.NoError(t, err)
	require.NotNil(t, execution)
	assert.True(t, postcheck.OK)
	assert.Empty(t, postcheck.RepairJournals)
	assert.NotEmpty(t, execution.Transactions)
}

func TestApplyRepairSessionReturnsReviewedResultAndExecutionWhenRefreshFails(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		"target.md": "# Target\n",
		"source.md": "bad extension: [[target.md]]\n",
	}))
	runCtx := linkHygieneRunContext(t, root)
	planned, _, err := RunSuiteOnce(context.Background(), Options{
		Checks: []string{CheckLinkHygiene}, RunContext: &runCtx,
	})
	require.NoError(t, err)
	refreshErr := errors.New("refresh unavailable")

	result, execution, err := ApplyRepairSession(context.Background(), planned, runCtx, Options{
		Fix: true, NonInteractive: true,
		PostApplyRefresher: &repairPostApplyProbe{t: t, refreshErr: refreshErr},
	})
	require.ErrorIs(t, err, refreshErr)
	require.NotNil(t, execution)
	assert.Same(t, execution, result.FixExecution)
	assert.Equal(t, planned.IssueCount, result.IssueCount,
		"without a prepared postcheck the reviewed findings remain the meaningful result")
	assert.Nil(t, result.FixPlan, "a failed refresh cannot re-expose the stale reviewed plan")
	require.NotEmpty(t, result.RepairJournals)
	assert.Equal(t, "committed", result.RepairJournals[0].State)
}

func TestApplyRepairSessionRoutesMixedIdentifierPlanWithPrivatePayload(t *testing.T) {
	fixture := buildIdentifierRepairAdapterFixtureWithFiles(t, map[string]string{
		"notes/target.md":  "# Target\n",
		"notes/generic.md": "bad extension: [[notes/target.md]]\n",
		"notes/review.md":  "Review SPEC-0001 in prose before publishing.\n",
	})
	ctx := context.Background()
	scanned, _, err := RunSuiteOnce(ctx, Options{Checks: []string{CheckLinkHygiene}, RunContext: &fixture.runCtx})
	require.NoError(t, err)
	require.Len(t, scanned.Checks, 1)
	linkHygiene := scanned.Checks[0]
	require.Len(t, linkHygiene.Fixes, 1, "the fixture must yield exactly one supported generic repair")
	genericActionID := linkHygiene.Fixes[0].ID
	identifiers := CheckResult{
		Name:             CheckIdentifiers,
		identifierRepair: &identifierRepairPayload{Assembly: fixture.assembly, Bindings: fixture.bindings},
	}
	identifierPlan, err := BuildIdentifierRepairPlan(ctx, fixture.runCtx, fixture.assembly, fixture.bindings)
	require.NoError(t, err)
	require.NotEmpty(t, identifierPlan.AuthorityFingerprint)
	require.NotEmpty(t, identifierPlan.FollowUps, "prose mentions of the old identifier must become follow-ups")

	identifierOnly, err := BuildRepairPlan(ctx, fixture.runCtx, []CheckResult{identifiers})
	require.NoError(t, err)
	require.NotNil(t, identifierOnly)
	assert.Equal(t, identifierPlan.Fingerprint, identifierOnly.Fingerprint)
	assert.Equal(t, identifierPlan.AuthorityFingerprint, identifierOnly.AuthorityFingerprint)
	assert.Equal(t, identifierPlan.FollowUps, identifierOnly.FollowUps)
	assert.True(t, identifierOnly.RequiresLeaseHeldReplan)

	reviewed, err := BuildRepairPlan(ctx, fixture.runCtx, []CheckResult{linkHygiene, identifiers})
	require.NoError(t, err)
	require.NotNil(t, reviewed)
	reviewedActionIDs := make([]string, 0, len(reviewed.Actions))
	for _, action := range reviewed.Actions {
		reviewedActionIDs = append(reviewedActionIDs, action.ID)
	}
	require.ElementsMatch(t, []string{genericActionID, fixture.bindings[0].Action.ID}, reviewedActionIDs)
	assert.Equal(t, identifierPlan.AuthorityFingerprint, reviewed.AuthorityFingerprint)
	assert.True(t, reviewed.RequiresLeaseHeldReplan, "the mixed plan must keep the identifier lease-held replan marker")
	assert.Equal(t, identifierPlan.FollowUps, reviewed.FollowUps)
	planned := Result{
		SelectedChecks: []string{CheckIdentifiers, CheckLinkHygiene},
		Checks:         []CheckResult{identifiers, linkHygiene},
		FixPlan:        reviewed,
	}

	postcheck, execution, err := ApplyRepairSession(ctx, planned, fixture.runCtx, Options{
		Fix: true, NonInteractive: true,
		PostApplyRefresher: &identifierRepairRuntimeRefresher{t: t, runCtx: fixture.runCtx},
	})
	require.NoError(t, err)
	require.NotNil(t, execution)
	assert.ElementsMatch(t, []string{genericActionID, fixture.bindings[0].Action.ID}, execution.Applied)
	assert.Equal(t, "bad extension: [[notes/target]]\n", string(mustReadFile(t, filepath.Join(fixture.root, "notes", "generic.md"))))
	_, statErr := os.Stat(filepath.Join(fixture.root, filepath.FromSlash(fixture.oldPath)))
	require.ErrorIs(t, statErr, os.ErrNotExist)
	assert.Zero(t, postcheck.ErrorCount)
	assert.True(t, postcheck.OK, "the prepared postcheck must clear both the generic and identifier findings: %+v", postcheck.Checks)
	assert.Same(t, execution, postcheck.FixExecution)
}

func TestApplyRepairSessionIdentifierPostcheckPreservesEveryCompletedCheck(t *testing.T) {
	fixture := buildIdentifierRepairAdapterFixtureWithFiles(t, nil)
	identifierPlan, err := BuildIdentifierRepairPlan(context.Background(), fixture.runCtx, fixture.assembly, fixture.bindings)
	require.NoError(t, err)
	planned := Result{
		SelectedChecks: []string{CheckIdentifiers, CheckLinkHygiene},
		Checks: []CheckResult{
			{
				Name:             CheckIdentifiers,
				identifierRepair: &identifierRepairPayload{Assembly: fixture.assembly, Bindings: fixture.bindings},
			},
			{Name: CheckLinkHygiene, OK: true},
		},
		FixPlan: identifierPlan,
	}

	postcheck, execution, err := ApplyRepairSession(context.Background(), planned, fixture.runCtx, Options{
		Fix: true, NonInteractive: true,
		PostApplyRefresher: &identifierRepairRuntimeRefresher{t: t, runCtx: fixture.runCtx},
	})
	require.NoError(t, err)
	require.NotNil(t, execution)
	assert.Contains(t, postcheck.SelectedChecks, CheckIdentifiers)
	assert.Contains(t, postcheck.SelectedChecks, CheckLinkHygiene)
	checkNames := make([]string, 0, len(postcheck.Checks))
	for _, check := range postcheck.Checks {
		checkNames = append(checkNames, check.Name)
	}
	assert.Contains(t, checkNames, CheckIdentifiers)
	assert.Contains(t, checkNames, CheckLinkHygiene)
}

func TestApplyRepairSessionCleanRerunIsIdempotent(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, writeFixtureFiles(root, map[string]string{
		"target.md": "# Target\n",
		"source.md": "clean link: [[target]]\n",
	}))
	runCtx := linkHygieneRunContext(t, root)
	planned, _, err := RunSuiteOnce(context.Background(), Options{
		Checks: []string{CheckLinkHygiene}, RunContext: &runCtx,
	})
	require.NoError(t, err)
	before := mustReadFile(t, filepath.Join(root, "source.md"))

	postcheck, execution, err := ApplyRepairSession(context.Background(), planned, runCtx, Options{
		Fix: true, NonInteractive: true, PostApplyRefresher: &repairPostApplyProbe{t: t},
	})
	require.NoError(t, err)
	assert.Nil(t, execution)
	assert.Equal(t, planned, postcheck)
	assert.Equal(t, before, mustReadFile(t, filepath.Join(root, "source.md")))
}
