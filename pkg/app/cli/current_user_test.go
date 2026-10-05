package actions

import (
	"context"
	"errors"
	"testing"

	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/vault/identity"
	"github.com/stretchr/testify/require"
)

func TestCurrentUserLookupFromQueryResult(t *testing.T) {
	lookup, err := CurrentUserLookupFromQueryResult(ontologyquery.Result{Data: map[string]any{
		"resolve": map[string]any{
			"found":        true,
			"path":         "people/drew.md",
			"title":        "Drew",
			"resolvedType": identity.CurrentUserType,
			"ref":          map[string]any{"ref": "people/drew.md"},
		},
	}})
	require.NoError(t, err)
	require.Equal(t, CurrentUserLookup{
		Found: true, ResolvedRef: "people/drew.md", Path: "people/drew.md", Title: "Drew", TypeName: identity.CurrentUserType,
	}, lookup)
}

func TestCurrentUserServiceShowAndSet(t *testing.T) {
	vaultRoot := t.TempDir()
	service := CurrentUserService{}

	missing, err := service.Show(vaultRoot)
	require.NoError(t, err)
	require.Equal(t, identity.MissingResolution(), missing)

	set, err := service.Set(vaultRoot, "people/drew.md")
	require.NoError(t, err)
	require.Equal(t, identity.Resolution{Configured: true, Ref: "people/drew.md"}, set)

	shown, err := service.Show(vaultRoot)
	require.NoError(t, err)
	require.Equal(t, set, shown)
}

func TestCurrentUserServiceValidate(t *testing.T) {
	vaultRoot := t.TempDir()
	_, err := identity.WriteCurrentUser(vaultRoot, "people/drew.md")
	require.NoError(t, err)

	service := CurrentUserService{Lookup: func(ctx context.Context, ref string) (CurrentUserLookup, error) {
		require.Equal(t, "people/drew.md", ref)
		return CurrentUserLookup{Found: true, ResolvedRef: ref, Path: ref, Title: "Drew", TypeName: identity.CurrentUserType}, nil
	}}
	result, err := service.Validate(context.Background(), vaultRoot)
	require.NoError(t, err)
	require.Empty(t, result.ErrorCode)
	require.Equal(t, "Drew", result.Title)
}

func TestCurrentUserServiceValidatePropagatesLookupError(t *testing.T) {
	vaultRoot := t.TempDir()
	_, err := identity.WriteCurrentUser(vaultRoot, "people/drew.md")
	require.NoError(t, err)

	service := CurrentUserService{Lookup: func(context.Context, string) (CurrentUserLookup, error) {
		return CurrentUserLookup{}, errors.New("lookup failed")
	}}
	_, err = service.Validate(context.Background(), vaultRoot)
	require.EqualError(t, err, "lookup failed")
}
