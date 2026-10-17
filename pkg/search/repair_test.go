package search

import (
	"github.com/atomicobject/rhizome/pkg/search/knowledge"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRepairQuerySpec_InferPathSeedsForSubsystemOverview(t *testing.T) {
	spec, warnings := RepairQuerySpec("/tmp/vault", QuerySpec{
		Text:      "search subsystem overview for pkg/search/service.go",
		Intent:    IntentSubsystemOverview,
		PathKinds: map[string]PathKind{"pkg/search/service.go": PathKindCode},
	})

	if spec.Intent != IntentSubsystemOverview {
		t.Fatalf("expected subsystem_overview to stay applied, got %q", spec.Intent)
	}
	if !spec.HasExplicitSeeds {
		t.Fatalf("expected inferred explicit seeds")
	}
	if len(spec.ExplicitSeedPaths) == 0 || spec.ExplicitSeedPaths[0] != "pkg/search/service.go" {
		t.Fatalf("expected inferred path seed, got %#v", spec.ExplicitSeedPaths)
	}
	if len(spec.Seeds) == 0 || spec.Seeds[0].String() != "file:pkg/search/service.go" {
		t.Fatalf("expected inferred file handle, got %#v", spec.Seeds)
	}
	if spec.TargetStatus != TargetStatusInferredPath {
		t.Fatalf("expected inferred path target status, got %q", spec.TargetStatus)
	}
	if len(warnings) == 0 || warnings[0].Code != "inferred_seed_paths" {
		t.Fatalf("expected inferred_seed_paths warning, got %#v", warnings)
	}
}

func TestRepairQuerySpec_DoesNotDowngradeSubsystemOverviewBeforeTargetResolution(t *testing.T) {
	spec, warnings := RepairQuerySpec("/tmp/vault", QuerySpec{
		Text:   "search subsystem overview",
		Intent: IntentSubsystemOverview,
	})

	if spec.Intent != IntentSubsystemOverview {
		t.Fatalf("expected subsystem_overview to remain for later resolution, got %q", spec.Intent)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no early downgrade warnings, got %#v", warnings)
	}
}

func TestRepairQuerySpec_DoesNotInferBareTopLevelDirectoryFromProse(t *testing.T) {
	spec, warnings := RepairQuerySpec("/tmp/vault", QuerySpec{
		Text:   "onboarding retrieval command surface semantic query guidance docs",
		Intent: IntentSubsystemOverview,
	})

	if spec.HasExplicitSeeds {
		t.Fatalf("expected no inferred explicit seeds for prose-only generic roots, got %#v", spec.ExplicitSeedPaths)
	}
	if len(spec.Seeds) != 0 {
		t.Fatalf("expected no inferred handles for prose-only generic roots, got %#v", spec.Seeds)
	}
	if spec.TargetStatus != TargetStatusNone {
		t.Fatalf("expected target status to remain unresolved for later target resolution, got %q", spec.TargetStatus)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no inferred seed warnings, got %#v", warnings)
	}
}

func TestRepairQuerySpec_LeavesFetchInferenceToTargetResolution(t *testing.T) {
	spec, warnings := RepairQuerySpec("/tmp/vault", QuerySpec{Text: "find callers for the cache builder", Intent: IntentCallers})
	if spec.Intent != IntentCallers {
		t.Fatalf("expected callers intent to stay applied, got %q", spec.Intent)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings before target resolution, got %#v", warnings)
	}
}

func TestRepairQuerySpec_IsIdempotentAfterFirstPass(t *testing.T) {
	spec, warnings := RepairQuerySpec("/tmp/vault", QuerySpec{
		Text:      "search subsystem overview for pkg/search/service.go",
		Intent:    IntentSubsystemOverview,
		PathKinds: map[string]PathKind{"pkg/search/service.go": PathKindCode},
	})
	if len(warnings) == 0 {
		t.Fatalf("expected first-pass warnings")
	}

	again, warnings := RepairQuerySpec("/tmp/vault", spec)
	if len(warnings) != 0 {
		t.Fatalf("expected no second-pass warnings, got %#v", warnings)
	}
	if again.ExplicitSeedPaths[0] != "pkg/search/service.go" {
		t.Fatalf("expected inferred seed to remain stable, got %#v", again.ExplicitSeedPaths)
	}
}

func TestRepairQuerySpec_DoesNotInferUnownedCodePath(t *testing.T) {
	spec, warnings := RepairQuerySpec("/tmp/vault", QuerySpec{
		Text:   "search subsystem overview for pkg/search/service.go",
		Intent: IntentSubsystemOverview,
	})
	require.Empty(t, spec.Seeds)
	require.Empty(t, spec.ExplicitSeedPaths)
	require.Empty(t, warnings)
}

func TestRepairQuerySpecSeedOwnershipDoesNotMutateSharedCorpus(t *testing.T) {
	shared := map[string]PathKind{"docs/reference.html": PathKindNote}
	first, _ := RepairQuerySpec("", QuerySpec{Text: "source", Intent: IntentSearch, PathKinds: shared, Seeds: []knowledge.Handle{knowledge.FileHandle("pkg/new.go")}})
	second, _ := RepairQuerySpec("", QuerySpec{Text: "other facet", Intent: IntentSearch, PathKinds: shared})
	require.Equal(t, PathKindCode, first.PathKinds["pkg/new.go"])
	require.Equal(t, map[string]PathKind{"docs/reference.html": PathKindNote}, shared)
	require.Equal(t, map[string]PathKind{"docs/reference.html": PathKindNote}, second.PathKinds)
}
