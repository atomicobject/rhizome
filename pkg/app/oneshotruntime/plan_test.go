package oneshotruntime

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlanRuntimeRequirementsAreAlwaysExplicit(t *testing.T) {
	plan := Plan{}
	require.NoError(t, plan.Validate())

	requirements := plan.RuntimeRequirements()
	assert.False(t, requirements.Includes(bootstrap.RuntimeCapabilitySearch))
	assert.False(t, requirements.Includes(bootstrap.RuntimeCapabilitySemantic))
	assert.False(t, requirements.Includes(bootstrap.RuntimeCapabilityCodeIndex))
}

func TestExplicitFullRuntimePlanConstructsWithBootstrapZeroValue(t *testing.T) {
	plan := Plan{FullRuntime: true}
	factoryCalls := 0

	runtime, err := Build(context.Background(), plan, func(_ context.Context, requirements bootstrap.RuntimeRequirements) (Runtime, error) {
		factoryCalls++
		assert.True(t, requirements.Includes(bootstrap.RuntimeCapabilitySearch))
		assert.True(t, requirements.Includes(bootstrap.RuntimeCapabilitySemantic))
		assert.True(t, requirements.Includes(bootstrap.RuntimeCapabilityCodeIndex))
		return &fakeRuntime{}, nil
	})
	require.NoError(t, err)
	require.NotNil(t, runtime)
	require.Equal(t, 1, factoryCalls)
}

func TestPlanValidateRejectsUndeclaredWait(t *testing.T) {
	plan := Plan{Readiness: []Readiness{ReadinessSemantic}}

	err := plan.Validate()
	require.Error(t, err)
	assert.ErrorContains(t, err, "semantic readiness requires semantic capability")
}

func TestPlanValidateRejectsImpossiblePolicies(t *testing.T) {
	tests := []struct {
		name string
		plan Plan
		want string
	}{
		{
			name: "unknown capability",
			plan: Plan{Capabilities: []bootstrap.RuntimeCapability{"typo"}},
			want: "unknown runtime capability",
		},
		{
			name: "duplicate capability",
			plan: Plan{Capabilities: []bootstrap.RuntimeCapability{
				bootstrap.RuntimeCapabilityCodeIndex,
				bootstrap.RuntimeCapabilityCodeIndex,
			}},
			want: "duplicate runtime capability",
		},
		{
			name: "read only store with write authority",
			plan: Plan{StoreAccess: StoreExistingReadOnly},
			want: "write authority cannot use an existing read-only store",
		},
		{
			name: "existing session does not require an indexed reader",
			plan: Plan{Session: SessionExistingOnly},
			want: "",
		},
		{
			name: "current code index without capability",
			plan: Plan{StoreAccess: StoreExistingReadOnly, CodeIndexFreshness: CodeIndexFreshnessCurrent},
			want: "current code-index freshness requires code-index capability",
		},
		{
			name: "cache snapshot without search readiness",
			plan: Plan{
				Capabilities: []bootstrap.RuntimeCapability{bootstrap.RuntimeCapabilitySearch},
				NoteState:    NoteStateCacheSnapshot,
				Readiness:    []Readiness{ReadinessSearch},
			},
			want: "cache snapshot requires note-cache readiness",
		},
		{
			name: "current ontology without live store",
			plan: Plan{OntologyState: OntologyStateCurrentProjection},
			want: "current ontology projection requires live read-write store access",
		},
		{
			name: "persisted ontology without store",
			plan: Plan{OntologyState: OntologyStatePersisted},
			want: "persisted ontology requires store access",
		},
		{
			name: "duplicate readiness",
			plan: Plan{
				Capabilities: []bootstrap.RuntimeCapability{bootstrap.RuntimeCapabilityCodeIndex},
				Readiness:    []Readiness{ReadinessCodeIndex, ReadinessCodeIndex},
			},
			want: "duplicate readiness",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authority := AuthorityReadOnly
			if tt.name == "read only store with write authority" {
				authority = AuthorityWriteCapable
			}
			err := tt.plan.ValidateForAuthority(authority)
			if tt.want == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.want)
		})
	}
}

func TestPlanValidateRejectsUnknownDiagnosticsAndUnavailablePolicy(t *testing.T) {
	err := (Plan{Diagnostics: DiagnosticsPolicy("typo")}).Validate()
	require.ErrorContains(t, err, "unknown diagnostics policy")

	err = (Plan{Unavailable: UnavailablePolicy("typo")}).Validate()
	require.ErrorContains(t, err, "unknown unavailable policy")

	err = (Plan{Diagnostics: DiagnosticsEnabled}).Validate()
	require.ErrorContains(t, err, "enabled diagnostics require a namespace")
}

func TestBuildAndAwaitInvokesOnlyDeclaredReadiness(t *testing.T) {
	runtime := &fakeRuntime{
		searchBlock:   make(chan struct{}),
		semanticBlock: make(chan struct{}),
	}
	plan := Plan{
		Capabilities: []bootstrap.RuntimeCapability{bootstrap.RuntimeCapabilityCodeIndex},
		Readiness:    []Readiness{ReadinessCodeIndex},
	}

	// The blocked channels are deliberately never released. Await returns only
	// because the plan is the sole authority for readiness consumption.
	built, err := BuildAndAwait(context.Background(), plan, func(_ context.Context, requirements bootstrap.RuntimeRequirements) (Runtime, error) {
		assert.True(t, requirements.Includes(bootstrap.RuntimeCapabilityCodeIndex))
		assert.False(t, requirements.Includes(bootstrap.RuntimeCapabilitySearch))
		assert.False(t, requirements.Includes(bootstrap.RuntimeCapabilitySemantic))
		return runtime, nil
	})
	require.NoError(t, err)
	assert.Same(t, runtime, built)
	assert.Equal(t, map[Readiness]int{ReadinessCodeIndex: 1}, runtime.callCounts())
}

func TestBuildConstructsWithoutAwaiting(t *testing.T) {
	runtime := &fakeRuntime{}
	plan := Plan{
		Capabilities: []bootstrap.RuntimeCapability{bootstrap.RuntimeCapabilityCodeIndex},
		Readiness:    []Readiness{ReadinessCodeIndex},
	}

	built, err := Build(context.Background(), plan, func(context.Context, bootstrap.RuntimeRequirements) (Runtime, error) {
		return runtime, nil
	})
	require.NoError(t, err)
	require.Same(t, runtime, built)
	require.Empty(t, runtime.callCounts())
}

func TestBuildAndAwaitRuntimeFreePlanDoesNotConstructRuntime(t *testing.T) {
	built, err := BuildAndAwait(context.Background(), Plan{}, func(context.Context, bootstrap.RuntimeRequirements) (Runtime, error) {
		return nil, errors.New("runtime construction must not run")
	})
	require.NoError(t, err)
	require.Nil(t, built)
}

func TestBuildAndAwaitClosesRuntimeWhenAwaitFails(t *testing.T) {
	runtime := &fakeRuntime{codeErr: errors.New("code unavailable")}
	plan := Plan{
		Capabilities: []bootstrap.RuntimeCapability{bootstrap.RuntimeCapabilityCodeIndex},
		Readiness:    []Readiness{ReadinessCodeIndex},
	}

	_, err := BuildAndAwait(context.Background(), plan, func(context.Context, bootstrap.RuntimeRequirements) (Runtime, error) {
		return runtime, nil
	})
	require.ErrorContains(t, err, "code unavailable")
	assert.True(t, runtime.closed)
}

func TestBuildAndAwaitStructuredUnavailableReturnsCallerOwnedRuntime(t *testing.T) {
	runtime := &fakeRuntime{codeErr: errors.New("code unavailable")}
	plan := Plan{
		Capabilities: []bootstrap.RuntimeCapability{bootstrap.RuntimeCapabilityCodeIndex},
		Readiness:    []Readiness{ReadinessCodeIndex},
		Unavailable:  UnavailableStructured,
	}

	built, err := BuildAndAwait(context.Background(), plan, func(context.Context, bootstrap.RuntimeRequirements) (Runtime, error) {
		return runtime, nil
	})
	require.ErrorContains(t, err, "code unavailable")
	require.Same(t, runtime, built)
	require.False(t, runtime.closed)
	require.Equal(t, map[Readiness]int{ReadinessCodeIndex: 1}, runtime.callCounts())
	built.Close()
	require.True(t, runtime.closed)
}

func TestBuildAndAwaitRequiresRuntimeForBuildWithoutAwait(t *testing.T) {
	plan := Plan{Capabilities: []bootstrap.RuntimeCapability{bootstrap.RuntimeCapabilityCodeIndex}}

	_, err := BuildAndAwait(context.Background(), plan, nil)
	require.ErrorContains(t, err, "runtime factory is required")

	_, err = BuildAndAwait(context.Background(), plan, func(context.Context, bootstrap.RuntimeRequirements) (Runtime, error) {
		return nil, nil
	})
	require.ErrorContains(t, err, "runtime factory returned nil")
}

type fakeRuntime struct {
	mu            sync.Mutex
	calls         map[Readiness]int
	searchBlock   <-chan struct{}
	semanticBlock <-chan struct{}
	codeErr       error
	closed        bool
}

func (f *fakeRuntime) WaitForSearch(ctx context.Context) error {
	f.record(ReadinessSearch)
	if f.searchBlock == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-f.searchBlock:
		return nil
	}
}

func (f *fakeRuntime) WaitForSemantic(ctx context.Context) error {
	f.record(ReadinessSemantic)
	if f.semanticBlock == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-f.semanticBlock:
		return nil
	}
}

func (f *fakeRuntime) WaitForCodeIndex(context.Context) error {
	f.record(ReadinessCodeIndex)
	return f.codeErr
}

func (f *fakeRuntime) WaitForNoteCache(context.Context) error {
	f.record(ReadinessNoteCache)
	return nil
}

func (f *fakeRuntime) WaitForSession(context.Context) error {
	f.record(ReadinessSession)
	return nil
}

func (f *fakeRuntime) Close() {
	f.closed = true
}

func (f *fakeRuntime) record(readiness Readiness) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls == nil {
		f.calls = map[Readiness]int{}
	}
	f.calls[readiness]++
}

func (f *fakeRuntime) callCounts() map[Readiness]int {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make(map[Readiness]int, len(f.calls))
	for readiness, count := range f.calls {
		result[readiness] = count
	}
	return result
}
