package query

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/diag"
	"github.com/atomicobject/rhizome/pkg/ontology/pushdown"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
)

// shortcut: roots cover note-only interfaces; mixed node kinds need shared hydration before exposing a root.
func noteInterfaceRootTypes(schema *ontoschema.Schema, name string) []string {
	if schema.Interfaces[name] == nil {
		return nil
	}
	if _, builtin := runtimeTypeNames()[name]; builtin {
		return nil
	}
	var names []string
	for typeName, noteType := range schema.Types {
		if !ontoschema.TypeMatchesOrImplements(schema, typeName, name) {
			continue
		}
		if noteType == nil || noteType.Role != ontoschema.TypeRoleNote {
			return nil
		}
		names = append(names, typeName)
	}
	sort.Strings(names)
	return names
}

func (e *executor) validateInterfaceRootConstraints(ctx context.Context, typeName string, args map[string]any) error {
	iface := e.schema.Interfaces[typeName]
	check := func(name string, sorting bool) error {
		name = strings.TrimSpace(name)
		if iface.ByName[name] != nil || pushdown.IsBuiltinFieldKey(name) || name == "title" || name == "ref" {
			return nil
		}
		if sorting && name == recordFieldUpdatedAt && interfaceRecordFactFree(e.schema, typeName, interfaceImplementors(e.schema, noteInterfaceRootTypes(e.schema, typeName), typeName), name) {
			return nil
		}
		return fmt.Errorf("%s root field %q is not a shared interface field or supported runtime key", typeName, name)
	}
	for _, filter := range rootFilterInputs(args["filters"]) {
		if err := check(filter.Field, false); err != nil {
			return err
		}
	}
	for _, spec := range rootSortInputs(args["sort"]) {
		if err := check(spec.Field, true); err != nil {
			return err
		}
	}
	return e.validateLinkRootFilters(ctx, typeName, args["filters"])
}

func (e *executor) indexedInterfaceRootPlan(ctx context.Context, typeName string, args map[string]any) typedRootPlan {
	typeNames := noteInterfaceRootTypes(e.schema, typeName)
	if len(typeNames) == 0 {
		return typedRootPlan{indexed: readmodel.NodeQueryPlan{Limit: rootFirst(args, false)}}
	}
	plan := e.indexedOntologyNodeRootPlan(ctx, typeNames[0], args)
	for _, name := range typeNames[1:] {
		next := e.indexedOntologyNodeRootPlan(ctx, name, args)
		plan.warnings = append(plan.warnings, next.warnings...)
		if !reflect.DeepEqual(plan.indexed.Predicates, next.indexed.Predicates) || !reflect.DeepEqual(plan.indexed.Sort, next.indexed.Sort) || !reflect.DeepEqual(plan.residual, next.residual) || !reflect.DeepEqual(plan.sort, next.sort) {
			// Different concrete bindings must be evaluated before one global page.
			plan.indexed.Predicates = nil
			plan.indexed.Sort = nil
			plan.residual = rootFilterInputs(args["filters"])
			plan.sort = rootSortInputs(args["sort"])
			plan.indexed.Limit = publicRootFirstMax
		}
	}
	plan.indexed.TypeNames = typeNames
	return plan
}

// Resolve shared links against their interface target, even when concrete fields narrow it.
type interfaceLinkResolver struct {
	pushdown.LinkResolver
	fields map[string]*ontoschema.Field
}

func (r interfaceLinkResolver) ResolveLinkTarget(ctx context.Context, field *ontoschema.Field, raw string) (string, diag.Warning) {
	if shared := r.fields[field.Name]; shared != nil && shared.Kind == ontoschema.FieldKindLink {
		field = shared
	}
	return r.LinkResolver.ResolveLinkTarget(ctx, field, raw)
}

func (e *executor) withInterfaceLinkResolver(typeName string) *executor {
	if iface := e.schema.Interfaces[typeName]; iface != nil {
		root := *e
		root.linkTargetResolver = interfaceLinkResolver{LinkResolver: e.linkTargetResolver, fields: iface.ByName}
		return &root
	}
	return e
}
