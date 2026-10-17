package runtimeproviders

import (
	"context"
	"fmt"
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	actions "github.com/atomicobject/rhizome/pkg/app/operations"
	"github.com/atomicobject/rhizome/pkg/app/queryengine"
	"github.com/atomicobject/rhizome/pkg/diag"
	"github.com/atomicobject/rhizome/pkg/ontology/idalloc"
	ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	schemadoc "github.com/atomicobject/rhizome/pkg/ontology/schemadoc"
	semdb "github.com/atomicobject/rhizome/pkg/store"
	"github.com/atomicobject/rhizome/pkg/vault/identity"
	"strings"
)

type Providers struct {
	schema  *ontoschema.Schema
	exec    *ontologyquery.ExecutableSchema
	deps    ontologyquery.Deps
	store   *semdb.Store
	anchors *codeanchor.Service
}

func New(schema *ontoschema.Schema, exec *ontologyquery.ExecutableSchema, deps ontologyquery.Deps, anchors *codeanchor.Service) *Providers {
	store, _ := deps.Store.(*semdb.Store)
	return &Providers{schema: schema, exec: exec, deps: deps, store: store, anchors: anchors}
}
func (p Providers) AuthoringGuide(ctx context.Context, req ontologyquery.OntologyAuthoringGuideRequest) (ontologyquery.OntologyAuthoringGuide, error) {
	typeName := strings.TrimSpace(req.Type)
	if typeName == "" {
		return ontologyquery.OntologyAuthoringGuide{Type: req.Type, Available: false, Warnings: []diag.Warning{{Code: "type_required", Message: "authoringGuide requires a non-empty type"}}}, nil
	}
	if p.schema == nil {
		return ontologyquery.OntologyAuthoringGuide{Type: req.Type, Available: false, Warnings: []diag.Warning{{Code: "ontology_runtime_unavailable", Message: "ontology schema is unavailable"}}}, nil
	}
	requested := []string{typeName}
	lookup := schemadoc.IDLookup(func(typeName string) (string, bool, error) {
		if p.store == nil {
			return "", false, nil
		}
		res, err := idalloc.Allocate(ctx, p.schema, p.store, typeName)
		if err != nil {
			return "", false, nil
		}
		return res.Next, true, nil
	})
	text, err := schemadoc.RenderGuideMarkdown(p.schema, schemadoc.GuideOptions{Types: requested, ResolveCompanion: CompanionResolver(p.deps.NoteReader), LookupID: lookup})
	if err != nil {
		return ontologyquery.OntologyAuthoringGuide{}, err
	}
	return ontologyquery.OntologyAuthoringGuide{Type: typeName, Markdown: text, Available: true}, nil
}
func (p Providers) NextID(ctx context.Context, request ontologyquery.OntologyNextIDRequest) (ontologyquery.OntologyNextID, error) {
	typeName := strings.TrimSpace(request.Type)
	if p.schema == nil || p.store == nil {
		return ontologyquery.OntologyNextID{Type: typeName, Available: false, ErrorCode: "runtime_unavailable", Error: "ontology runtime store is unavailable", Warnings: []diag.Warning{{Code: "runtime_unavailable", Message: "ontology runtime store is unavailable"}}}, nil
	}
	res, err := idalloc.AllocateRequest(ctx, p.schema, p.store, idalloc.Request{Type: typeName, Count: request.Count, Paths: request.Paths})
	if err != nil {
		code := idalloc.ErrorCode(err)
		next := ontologyquery.OntologyNextID{Type: typeName, Available: false, ErrorCode: code, Error: err.Error(), Warnings: []diag.Warning{{Code: code, Message: err.Error()}}}
		if res != nil {
			next.IdentifierField = res.IdentifierField
			next.Strategy = res.Strategy
			next.Source = res.Source
			next.Prefix = res.Prefix
			next.Separator = res.Separator
			next.Pad = res.Pad
			next.Count = res.Count
			next.Paths = res.Paths
		}
		return next, nil
	}
	return ontologyquery.OntologyNextID{Type: res.Type, IdentifierField: res.IdentifierField, Strategy: res.Strategy, Source: res.Source, Prefix: res.Prefix, Separator: res.Separator, Pad: res.Pad, Next: res.Next, IDs: res.IDs, Count: res.Count, Last: res.Last, CurrentMax: res.CurrentMax, CurrentMaxValue: res.CurrentMaxValue, CurrentMaxOwner: res.CurrentMaxOwner, OwnersScanned: res.OwnersScanned, SharedWith: res.SharedWith, OwnersMatched: res.OwnersMatched, OwnersSkipped: res.OwnersSkipped, Notes: res.Notes, Paths: res.Paths, Allocations: ontologyIDAllocations(res.Allocations), Available: true}, nil
}
func ontologyIDAllocations(values []idalloc.Allocation) []ontologyquery.OntologyIDAllocation {
	out := make([]ontologyquery.OntologyIDAllocation, 0, len(values))
	for _, value := range values {
		out = append(out, ontologyquery.OntologyIDAllocation{Path: value.Path, ID: value.ID, Base: value.Base, Disambiguator: value.Disambiguator})
	}
	return out
}
func (p Providers) CurrentUser(ctx context.Context) (ontologyquery.OntologyCurrentUser, error) {
	if p.deps.VaultDef.BasePath() == "" {
		return ontologyquery.OntologyCurrentUser{Configured: false, Found: false, ErrorCode: "runtime_unavailable", Error: "ontology runtime is unavailable", Warnings: []diag.Warning{{Code: "runtime_unavailable", Message: "ontology runtime is unavailable"}}}, nil
	}
	service := actions.CurrentUserService{Lookup: func(ctx context.Context, ref string) (actions.CurrentUserLookup, error) {
		prepared, errs := ontologyquery.PrepareWithVariables(p.exec, actions.CurrentUserLookupQuery, map[string]any{"ref": ref})
		if len(errs) > 0 {
			return actions.CurrentUserLookup{}, fmt.Errorf("current user query: %s", errs[0].Message)
		}
		engine, err := queryengine.New(p.schema, p.exec, p.deps)
		if err != nil {
			return actions.CurrentUserLookup{}, err
		}
		result, err := engine.Run(ctx, prepared)
		if err != nil {
			return actions.CurrentUserLookup{}, err
		}
		return actions.CurrentUserLookupFromQueryResult(result)
	}}
	result, err := service.Validate(ctx, p.deps.VaultDef.BasePath())
	if err != nil {
		return ontologyquery.OntologyCurrentUser{}, err
	}
	return ontologyCurrentUserFromIdentity(result), nil
}
func ontologyCurrentUserFromIdentity(result identity.Resolution) ontologyquery.OntologyCurrentUser {
	warnings := []diag.Warning{}
	if result.ErrorCode != "" {
		warnings = append(warnings, diag.Warning{Code: result.ErrorCode, Message: result.Error, Path: result.Path})
	}
	return ontologyquery.OntologyCurrentUser{Configured: result.Configured, Ref: result.Ref, Found: result.Found, ResolvedRef: result.ResolvedRef, Path: result.Path, Title: result.Title, TypeName: result.TypeName, ErrorCode: result.ErrorCode, Error: result.Error, Warnings: warnings}
}
