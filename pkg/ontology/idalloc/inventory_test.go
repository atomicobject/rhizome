package idalloc

import (
	"context"
	"errors"
	"testing"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/stretchr/testify/require"
)

func TestAllocateNodeInventoryReadErrors(t *testing.T) {
	schema := loadTestSchema(t, `
type Story implements Section @node(locator: EMBEDDED) { id: ID! @field @identifier(preferred: true, derivable: false, prefix: "ITEM") }
type StoriesSection implements Section { stories: [Story!] @contains(level: H3) }
type Spec @node(paths: ["specs/*.md"]) {
 id: String! @field @identifier(preferred: true, prefix: "ITEM")
 stories: StoriesSection @contains(level: H2, heading: "Stories")
}`)
	readErr := errors.New("inventory unavailable")
	for _, requested := range []string{"Spec", "Story"} {
		result, err := Allocate(context.Background(), schema, &fakeStore{}, requested)
		require.NoError(t, err)
		require.Equal(t, "ITEM-0001", result.Next)
		require.Zero(t, result.OwnersScanned)
	}
	for _, tc := range []struct {
		name  string
		store *fakeStore
	}{
		{"nodes", &fakeStore{nodesErr: readErr}},
		{"fields", &fakeStore{nodesByType: map[string][]codeanchor.IntelOntologyNode{"Story": {{NodeID: "node-one", SourceLocator: "specs/one.md#^ITEM-0002"}}}, fieldsErr: readErr}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Allocate(context.Background(), schema, tc.store, "Spec")
			require.ErrorIs(t, err, readErr)
		})
	}
}

func TestAllocateExcludesDerivedSiblingOwners(t *testing.T) {
	schema := loadTestSchema(t, `
type Story implements Section @node(locator: EMBEDDED) { id: ID! @field @identifier(preferred: true, derivedSuffix: "US", prefix: "ITEM") }
type StoriesSection implements Section { stories: [Story!] @contains(level: H3) }
type Spec @node(paths: ["specs/*.md"]) {
 id: String! @field @identifier(preferred: true, prefix: "ITEM")
 stories: StoriesSection @contains(level: H2, heading: "Stories")
}`)
	result, err := Allocate(context.Background(), schema, &fakeStore{nodesErr: errors.New("derived owners must not be queried")}, "Spec")
	require.NoError(t, err)
	require.Equal(t, "ITEM-0001", result.Next)
	require.Empty(t, result.SharedWith)
	require.Zero(t, result.OwnersScanned)
}
