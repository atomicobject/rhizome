package oneshotruntime

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultRegistryIsValid(t *testing.T) {
	require.NoError(t, DefaultRegistry().Validate())
}

func TestRegistryRejectsDuplicateOperationAndFrontIDs(t *testing.T) {
	declaration := Declaration{
		ID:         "agent.surface",
		Fronts:     []FrontRef{{Path: "agent surface"}},
		Owner:      CompositionNone,
		PlanSource: PlanSourceStatic,
		Authority:  AuthorityReadOnly,
		StaticPlan: &Plan{},
	}

	err := NewRegistry(declaration, declaration).Validate()
	require.Error(t, err)
	assert.ErrorContains(t, err, "duplicate operation id")

	other := declaration
	other.ID = "agent.start"
	err = NewRegistry(declaration, other).Validate()
	require.Error(t, err)
	assert.ErrorContains(t, err, "duplicate front path")
}

func TestRegistryRequiresPlanForOneShotStaticOperation(t *testing.T) {
	err := NewRegistry(Declaration{
		ID:         "agent.code-symbol",
		Fronts:     []FrontRef{{Path: "agent code-symbol"}},
		Owner:      CompositionOneShotRuntime,
		PlanSource: PlanSourceStatic,
		Authority:  AuthorityReadOnly,
	}).Validate()

	require.Error(t, err)
	assert.ErrorContains(t, err, "static one-shot operation requires a plan")
}

func TestRegistryRequiresReachableRequestPlanForOneShotOperation(t *testing.T) {
	err := NewRegistry(Declaration{
		ID:         "agent.unknown",
		Fronts:     []FrontRef{{Path: "agent unknown"}},
		Owner:      CompositionOneShotRuntime,
		PlanSource: PlanSourceRequestDerived,
		Authority:  AuthorityReadOnly,
	}).Validate()

	require.ErrorContains(t, err, "request-derived one-shot operation requires a planner binding")
}

func TestRegistryReservesFullRuntimeForTypedExemptions(t *testing.T) {
	static := Declaration{
		ID: "agent.code-symbol-context", Fronts: []FrontRef{{Path: "agent code-symbol-context"}},
		Owner: CompositionOneShotRuntime, PlanSource: PlanSourceStatic,
		Authority: AuthorityReadOnly, StaticPlan: &Plan{FullRuntime: true},
	}
	require.ErrorContains(t, NewRegistry(static).Validate(), "cannot declare full runtime")

	exempt := static
	exempt.PlanSource = PlanSourceExempt
	exempt.StaticPlan = nil
	exempt.Exemption = RuntimeExemptionFullRuntimeParity
	exempt.ExemptionPlan = &Plan{}
	require.ErrorContains(t, NewRegistry(exempt).Validate(), "requires a full-runtime plan")

	exempt.Exemption = RuntimeExemption("unknown")
	exempt.ExemptionPlan = &Plan{FullRuntime: true}
	require.ErrorContains(t, NewRegistry(exempt).Validate(), "unknown exemption")
}

func TestDefaultRegistryExposesStableOperationIDsForFronts(t *testing.T) {
	registry := DefaultRegistry()
	id, ok := registry.OperationIDForFront("agent code-symbol")
	require.True(t, ok)
	assert.Equal(t, OperationID("agent.code-symbol"), id)
}

func TestExactIndexPlansDeclareNoSessionWhileFileContextKeepsExistingSession(t *testing.T) {
	registry := DefaultRegistry()
	for _, operationID := range []OperationID{
		"agent.code-symbol",
		"agent.code-references",
		"agent.code-rationale",
		"agent.graph-path",
	} {
		declaration, ok := registry.Declaration(operationID)
		require.True(t, ok)
		require.NotNil(t, declaration.StaticPlan)
		assert.Equal(t, SessionNone, declaration.StaticPlan.Session)
	}
	fileContext, ok := registry.Declaration("agent.file-context")
	require.True(t, ok)
	require.NotNil(t, fileContext.StaticPlan)
	assert.Equal(t, SessionExistingOnly, fileContext.StaticPlan.Session)
	assert.Contains(t, fileContext.StaticPlan.Readiness, ReadinessSession)
}

func TestDefaultRegistryIncludesRequiredAdjacentControls(t *testing.T) {
	registry := DefaultRegistry()
	for _, path := range []string{
		"search",
		"list",
		"note tags list",
		"note properties list",
		"note move",
		"note rename-heading",
		"graph path",
		"code rationale",
		"query-recipe show",
		"serve",
		"index",
	} {
		assert.Truef(t, registry.HasFront(path), "missing adjacent front %q", path)
	}
}

func TestMutationAuthorityKeepsAgentPlanningSeparateFromMCPAndRootApply(t *testing.T) {
	registry := DefaultRegistry()
	for _, operationID := range []OperationID{"agent.node-link", "agent.note-rename-heading"} {
		declaration, ok := registry.Declaration(operationID)
		require.True(t, ok)
		assert.Equal(t, AuthorityPlanOnly, declaration.Authority)
	}
	for _, operationID := range []OperationID{"root.note-rename-heading", "root.graph-file-context"} {
		declaration, ok := registry.Declaration(operationID)
		require.True(t, ok)
		assert.Equal(t, AuthorityWriteCapable, declaration.Authority)
		assert.Equal(t, CompositionBoundedService, declaration.Owner)
	}
}
