package noteformat

import "testing"

func TestRegistryRejectsUnknownCapability(t *testing.T) {
	_, err := NewRegistry(testProviderInternal{descriptor: Descriptor{
		ID:                "html",
		Extensions:        []string{".html"},
		ProviderVersion:   "provider-v1",
		ProjectionVersion: "projection-v1",
		OwnershipPolicy:   OwnershipExplicitInclude,
		Capabilities:      Capabilities{values: []Capability{"unknown"}},
	}})
	if err == nil {
		t.Fatal("NewRegistry succeeded with unknown capability")
	}
}

type testProviderInternal struct {
	descriptor Descriptor
}

func (p testProviderInternal) Descriptor() Descriptor {
	return p.descriptor
}
