package query

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"strings"
	"time"

	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology"
)

// Record facts are runtime fields on note-backed and embedded object types:
// updatedAt is the indexed modification time of the record's source note, and
// issueCount counts published validation issues whose membership is the note
// (or, for an embedded record, its node). An authored field with either name
// wins, so these names are only added where the schema leaves them free.
const (
	recordFieldUpdatedAt  = "updatedAt"
	recordFieldIssueCount = "issueCount"
)

// Every record type repeats these descriptions in query-schema output, so they
// stay short; the kit API reference carries the full guidance.
var recordFactFields = []struct{ name, description, sdl string }{
	{
		recordFieldUpdatedAt,
		"Indexed mtime of the source note file (RFC3339), or null without indexed metadata. Not the last commit: a fresh clone gives every file the same time. Interfaces declare it only when every implementor does.",
		"updatedAt: DateTime",
	},
	{
		recordFieldIssueCount,
		"Published validation issues on the record's note, or on its node for an embedded record; 0 before validation publishes. Interfaces declare it only when every implementor does.",
		"issueCount: Int!",
	},
}

func recordFactSDL(free func(name string) bool) []string {
	var out []string
	for _, field := range recordFactFields {
		if free(field.name) {
			out = append(out, `"""`+field.description+`"""`, field.sdl)
		}
	}
	return out
}

func isRecordFactField(name string) bool {
	return name == recordFieldUpdatedAt || name == recordFieldIssueCount
}

// recordFactFree reports whether a note or embedded type leaves name free for
// the runtime record fact: neither the type, an authored Note interface, nor a
// derived relation count claims it.
func recordFactFree(schema *ontology.Schema, noteType *ontology.NoteType, name string) bool {
	if noteType == nil || (noteType.Role != ontology.TypeRoleNote && noteType.Role != ontology.TypeRoleEmbeddedNode) {
		return false
	}
	if fieldsClaim(noteType.ByName, name) {
		return false
	}
	if noteType.Role == ontology.TypeRoleNote && schema.NoteInterfaceAuthored {
		if iface := schema.Interfaces["Note"]; iface != nil && fieldsClaim(iface.ByName, name) {
			return false
		}
	}
	return true
}

func fieldsClaim(byName map[string]*ontology.Field, name string) bool {
	if byName[name] != nil {
		return true
	}
	// writeRelationCountFields derives <list>Count for neighbor and reverse lists.
	if base, ok := strings.CutSuffix(name, "Count"); ok {
		if field := byName[base]; field != nil && field.List && (field.Kind == ontology.FieldKindNeighbor || field.Kind == ontology.FieldKindReverse) {
			return true
		}
	}
	return false
}

// interfaceRecordFactFree adds a record fact to an interface only when the
// interface and every interface extending it leave the name free and every
// concrete implementor carries it, so each generated implementation stays
// valid. An interface without implementors is vacuously free.
func interfaceRecordFactFree(schema *ontology.Schema, ifaceName string, implementors []*ontology.NoteType, name string) bool {
	for _, iface := range schema.Interfaces {
		if iface != nil && (iface.Name == ifaceName || interfaceExtends(schema, iface, ifaceName)) && fieldsClaim(iface.ByName, name) {
			return false
		}
	}
	for _, noteType := range implementors {
		if !recordFactFree(schema, noteType, name) {
			return false
		}
	}
	return true
}

func interfaceExtends(schema *ontology.Schema, iface *ontology.InterfaceType, target string) bool {
	for _, parent := range iface.Implements {
		if parent == target {
			return true
		}
		if next := schema.Interfaces[parent]; next != nil && interfaceExtends(schema, next, target) {
			return true
		}
	}
	return false
}

func recordIssueScope(ref ontology.NodeRef) semdb.ValidationScope {
	if ref.Kind == ontology.NodeKindNote {
		return semdb.ValidationScope{Kind: semdb.ValidationScopeNote, Key: ref.NotePath}
	}
	return semdb.ValidationScope{Kind: semdb.ValidationScopeNode, Key: ref.NodeID}
}

// pendingRecordFact holds a record fact's place in the response until
// fillRecordFacts replaces it.
type pendingRecordFact struct {
	notePath string
	scope    semdb.ValidationScope
	issues   bool
}

// recordUpdatedAt defers the source note's indexed modification time.
func (e *executor) recordUpdatedAt(notePath string) any {
	if notePath == "" {
		return nil
	}
	e.recordFactPaths[notePath] = struct{}{}
	return pendingRecordFact{notePath: notePath}
}

// recordIssueCount defers the record's published validation issue count.
func (e *executor) recordIssueCount(ref ontology.NodeRef) any {
	scope := recordIssueScope(ref)
	if scope.Key == "" {
		return 0
	}
	e.recordFactScopes[scope] = struct{}{}
	return pendingRecordFact{scope: scope, issues: true}
}

// fillRecordFacts resolves every record fact in the response after the whole
// selection tree has resolved, so records at any depth, under any number of
// parents, share one metadata read and one validation summary read per 200
// scopes. updatedAt is RFC3339 or null without indexed metadata; issueCount is
// 0 before validation has published a snapshot.
func (e *executor) fillRecordFacts(ctx context.Context, data map[string]any) {
	if len(e.recordFactPaths) == 0 && len(e.recordFactScopes) == 0 {
		return
	}
	updatedAt := e.loadRecordUpdatedAt(ctx, slices.Sorted(maps.Keys(e.recordFactPaths)))
	issueCounts := e.loadRecordIssueCounts(ctx, slices.SortedFunc(maps.Keys(e.recordFactScopes), compareValidationScopes))
	walkResponseValues(data, func(value any) any {
		fact, ok := value.(pendingRecordFact)
		switch {
		case !ok:
			return value
		case fact.issues:
			return issueCounts[fact.scope]
		case updatedAt[fact.notePath] > 0:
			return time.Unix(updatedAt[fact.notePath], 0).UTC().Format(time.RFC3339)
		default:
			return nil
		}
	})
}

// walkResponseValues replaces each leaf of a resolved response in place.
func walkResponseValues(value any, replace func(any) any) any {
	switch current := value.(type) {
	case map[string]any:
		for key, item := range current {
			current[key] = walkResponseValues(item, replace)
		}
		return current
	case []any:
		for index, item := range current {
			current[index] = walkResponseValues(item, replace)
		}
		return current
	default:
		return replace(value)
	}
}

func compareValidationScopes(a, b semdb.ValidationScope) int {
	return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Key, b.Key))
}

func (e *executor) loadRecordUpdatedAt(ctx context.Context, paths []string) map[string]int64 {
	if len(paths) == 0 || e.loaders == nil || e.loaders.scope == nil {
		return nil
	}
	loaded, err := e.loaders.scope.NoteUpdatedAt(ctx, paths)
	if err != nil {
		// Report once; the paths stay null for this execution.
		e.addError(nil, err.Error())
	}
	return loaded
}

func (e *executor) loadRecordIssueCounts(ctx context.Context, scopes []semdb.ValidationScope) map[semdb.ValidationScope]int {
	store, ok := e.deps.Store.(validationResultStore)
	if len(scopes) == 0 || !ok || store == nil {
		return nil
	}
	generation := e.validationState(ctx).PublishedGeneration
	if generation <= 0 {
		return nil
	}
	out := make(map[semdb.ValidationScope]int, len(scopes))
	for start := 0; start < len(scopes); start += semdb.ValidationScopeBatchMax {
		batch := scopes[start:min(start+semdb.ValidationScopeBatchMax, len(scopes))]
		response, err := store.GetValidationScopeSummaries(ctx, semdb.ValidationScopeSummaryRequest{Generation: generation, Scopes: batch})
		if err != nil {
			// Report once; the remaining scopes count as 0 for this execution.
			e.addError(nil, err.Error())
			return out
		}
		// Summaries come back in request order.
		for index, summary := range response.Summaries {
			out[batch[index]] = summary.IssueCount
		}
	}
	return out
}
