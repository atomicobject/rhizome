package actions

import (
	"context"
	"fmt"
	"strings"

	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/vault/identity"
)

// CurrentUserLookup is the resolved identity data supplied by the ontology
// query adapter.
type CurrentUserLookup struct {
	Found       bool
	ResolvedRef string
	Path        string
	Title       string
	TypeName    string
}

// CurrentUserService owns the current-user application flow. Lookup is the
// only runtime-dependent operation: showing and setting the ignored config do
// not need an ontology runtime.
type CurrentUserService struct {
	Lookup func(context.Context, string) (CurrentUserLookup, error)
}

const CurrentUserLookupQuery = `query CurrentUser($ref: String!) {
  resolve(ref: $ref) {
    found
    path
    title
    resolvedType
    ref { ref }
  }
}`

func CurrentUserLookupFromQueryResult(result ontologyquery.Result) (CurrentUserLookup, error) {
	if len(result.Errors) > 0 {
		return CurrentUserLookup{}, fmt.Errorf("%s", result.Errors[0].Message)
	}
	resolved, _ := result.Data["resolve"].(map[string]any)
	ref, _ := resolved["ref"].(map[string]any)
	return CurrentUserLookup{
		Found:       currentUserBool(resolved["found"]),
		ResolvedRef: currentUserText(ref["ref"]),
		Path:        currentUserText(resolved["path"]),
		Title:       currentUserText(resolved["title"]),
		TypeName:    currentUserText(resolved["resolvedType"]),
	}, nil
}

func (s CurrentUserService) Show(vaultRoot string) (identity.Resolution, error) {
	cfg, ok, err := identity.ReadCurrentUser(vaultRoot)
	if err != nil {
		return identity.Resolution{}, err
	}
	if !ok {
		return identity.MissingResolution(), nil
	}
	return identity.Resolution{Configured: true, Ref: cfg.Ref}, nil
}

func (s CurrentUserService) Set(vaultRoot, ref string) (identity.Resolution, error) {
	cfg, err := identity.WriteCurrentUser(vaultRoot, ref)
	if err != nil {
		return identity.Resolution{}, err
	}
	return identity.Resolution{Configured: true, Ref: cfg.Ref}, nil
}

func (s CurrentUserService) Validate(ctx context.Context, vaultRoot string) (identity.Resolution, error) {
	cfg, ok, err := identity.ReadCurrentUser(vaultRoot)
	if err != nil {
		return identity.Resolution{}, err
	}
	if !ok || strings.TrimSpace(cfg.Ref) == "" {
		return identity.MissingResolution(), nil
	}
	if s.Lookup == nil {
		return identity.Resolution{}, fmt.Errorf("current-user lookup is unavailable")
	}
	lookup, err := s.Lookup(ctx, cfg.Ref)
	if err != nil {
		return identity.Resolution{}, err
	}
	return identity.ResolutionForConfig(cfg, lookup.Found, lookup.ResolvedRef, lookup.Path, lookup.Title, lookup.TypeName), nil
}

func currentUserText(value any) string {
	text, _ := value.(string)
	return text
}

func currentUserBool(value any) bool {
	boolean, _ := value.(bool)
	return boolean
}
