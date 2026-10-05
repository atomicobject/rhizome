package oneshotruntime

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	"github.com/stretchr/testify/require"
)

func TestOntologyQueryPlanSelectsOnlyPreparedCapabilities(t *testing.T) {
	nonSemantic := OntologyQueryPlan(false, false)
	require.NoError(t, nonSemantic.Validate())
	require.Equal(t, []bootstrap.RuntimeCapability{bootstrap.RuntimeCapabilityCodeIndex}, nonSemantic.Capabilities)
	require.Equal(t, []Readiness{ReadinessCodeIndex}, nonSemantic.Readiness)
	require.Equal(t, StoreLiveReadWrite, nonSemantic.StoreAccess)
	require.Equal(t, OntologyStateCurrentProjection, nonSemantic.OntologyState)
	require.False(t, nonSemantic.RuntimeRequirements().Includes(bootstrap.RuntimeCapabilitySearch))
	require.False(t, nonSemantic.RuntimeRequirements().Includes(bootstrap.RuntimeCapabilitySemantic))
	require.False(t, nonSemantic.RuntimeRequirements().Includes(bootstrap.RuntimeCapabilityLeaderSyncers))

	searchOnly := OntologyQueryPlan(false, true)
	require.NoError(t, searchOnly.Validate())
	require.Equal(t, []bootstrap.RuntimeCapability{bootstrap.RuntimeCapabilityCodeIndex, bootstrap.RuntimeCapabilitySemantic}, searchOnly.Capabilities)
	require.Equal(t, []Readiness{ReadinessCodeIndex}, searchOnly.Readiness)
	require.True(t, searchOnly.RuntimeRequirements().Includes(bootstrap.RuntimeCapabilitySemantic))

	semantic := OntologyQueryPlan(true, true)
	require.NoError(t, semantic.Validate())
	require.Equal(t, []bootstrap.RuntimeCapability{bootstrap.RuntimeCapabilityCodeIndex, bootstrap.RuntimeCapabilitySemantic}, semantic.Capabilities)
	require.Equal(t, []Readiness{ReadinessCodeIndex, ReadinessSemantic}, semantic.Readiness)
}
