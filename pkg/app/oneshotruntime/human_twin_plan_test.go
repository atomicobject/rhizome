package oneshotruntime

import (
	"context"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestViewPlanKeepsOptionalPersistedReadsDegradable(t *testing.T) {
	plan := ViewPlan()

	require.NoError(t, plan.Validate())
	assert.Equal(t, []bootstrap.RuntimeCapability{bootstrap.RuntimeCapabilityCodeIndex}, plan.Capabilities)
	assert.Equal(t, StoreLiveReadWrite, plan.StoreAccess)
	assert.Equal(t, OntologyStatePersisted, plan.OntologyState)
	assert.Equal(t, UnavailableDegrade, plan.Unavailable)
}

func TestGraphContextPlansKeepHumanWriteAuthoritySeparateFromAgentReads(t *testing.T) {
	readPlan := GraphContextPlan(false)
	require.NoError(t, readPlan.ValidateForAuthority(AuthorityWriteCapable))
	assert.Equal(t, StoreNone, readPlan.StoreAccess)
	assert.Equal(t, SessionNone, readPlan.Session)
	built, err := Build(context.Background(), readPlan, func(context.Context, bootstrap.RuntimeRequirements) (Runtime, error) {
		t.Error("plain graph context must not construct a runtime")
		return &fakeRuntime{}, nil
	})
	require.NoError(t, err)
	assert.Nil(t, built)

	compressionPlan := GraphContextPlan(true)
	require.NoError(t, compressionPlan.ValidateForAuthority(AuthorityWriteCapable))
	assert.Equal(t, StoreNone, compressionPlan.StoreAccess)
	assert.Equal(t, SessionNone, compressionPlan.Session)
	assert.Contains(t, compressionPlan.Capabilities, bootstrap.RuntimeCapabilitySemantic)
	assert.Equal(t, NoteStateLive, compressionPlan.NoteState)
}
