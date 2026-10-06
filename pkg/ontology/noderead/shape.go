package noderead

import (
	"context"
	"slices"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	"github.com/atomicobject/rhizome/pkg/vault/identity"
)

const UntypedShapeMember = "__untyped__"

type ShapeParts struct{ Members, Links, Folders bool }
type ShapeResponse struct {
	Rebuilding     bool          `json:"rebuilding"`
	TotalNotes     int           `json:"totalNotes"`
	TypedNotes     int           `json:"typedNotes"`
	UntypedNotes   int           `json:"untypedNotes"`
	AmbiguousNotes int           `json:"ambiguousNotes"`
	Members        *ShapeMembers `json:"members,omitempty"`
	Links          *ShapeLinks   `json:"links,omitempty"`
	Folders        *ShapeFolders `json:"folders,omitempty"`
}
type ShapeMembers struct {
	Types      []ShapeMember    `json:"types"`
	Interfaces []ShapeInterface `json:"interfaces"`
	Untyped    ShapeUntyped     `json:"untyped"`
}
type ShapeMember struct {
	Name        string           `json:"name"`
	Kind        string           `json:"kind"`
	Count       int              `json:"count"`
	IssueCount  int              `json:"issueCount"`
	LastChanged int64            `json:"lastChanged"`
	Lifecycle   *ShapeLifecycle  `json:"lifecycle"`
	Gaps        []ShapeGap       `json:"gaps"`
	TargetSets  []ShapeTargetSet `json:"targetSets"`
}
type ShapeInterface struct {
	Name         string          `json:"name"`
	Count        int             `json:"count"`
	IssueCount   int             `json:"issueCount"`
	LastChanged  int64           `json:"lastChanged"`
	Lifecycle    *ShapeLifecycle `json:"lifecycle"`
	Gaps         []ShapeGap      `json:"gaps"`
	Implementors []string        `json:"implementors"`
}
type ShapeUntyped struct {
	Count int `json:"count"`
	Links int `json:"links"`
}
type ShapeLifecycle struct {
	Field  string       `json:"field"`
	Values []ShapeValue `json:"values"`
}
type ShapeValue struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}
type ShapeGap struct {
	Field string `json:"field"`
	Empty int    `json:"empty"`
}
type ShapeTargetSet struct {
	Types   []string `json:"types"`
	Records int      `json:"records"`
}
type ShapeLinks struct {
	Pairs []ShapePair `json:"pairs"`
}
type ShapePair struct {
	A             string           `json:"a"`
	B             string           `json:"b"`
	Links         int              `json:"links"`
	RelationLinks int              `json:"relationLinks"`
	PlainLinks    int              `json:"plainLinks"`
	Fields        []ShapePairField `json:"fields"`
}
type ShapePairField struct {
	Type  string `json:"type"`
	Field string `json:"field"`
	Count int    `json:"count"`
}
type ShapeFolders struct {
	Rows []ShapeFolder `json:"rows"`
}
type ShapeFolder struct {
	Folder         string         `json:"folder"`
	Total          int            `json:"total"`
	Untyped        int            `json:"untyped"`
	Typed          map[string]int `json:"typed"`
	UntypedLinksTo map[string]int `json:"untypedLinksTo"`
}

// OntologyShape computes scope-independent counts from one committed snapshot.
// This aggregate has no edit-session overlay or source hydration.
func OntologyShape(ctx context.Context, store readmodel.ShapeStore, schema *ontology.Schema, parts ShapeParts) (ShapeResponse, error) {
	profiles := map[string]ontology.TypeProfile{}
	fields := []string{}
	if parts.Members && schema != nil {
		for name, t := range schema.Types {
			if shapeNoteType(t) {
				profiles[name], _ = ontology.DeriveTypeProfile(schema, name, identity.CurrentUserType)
			}
		}
		for name := range schema.Interfaces {
			profiles[name], _ = ontology.DeriveTypeProfile(schema, name, identity.CurrentUserType)
		}
		for _, p := range profiles {
			fields = append(fields, p.GapFields...)
			if p.LifecycleField != "" {
				fields = append(fields, p.LifecycleField)
			}
		}
		slices.Sort(fields)
		fields = slices.Compact(fields)
	}
	version := 0
	if schema != nil {
		version = ontology.OntologyMaterializationVersion
	}
	snapshot, err := store.OntologyShapeSnapshot(ctx, fields, parts.Members || parts.Links || parts.Folders, version)
	if err != nil {
		return ShapeResponse{}, err
	}
	if schema != nil && snapshot.SchemaHash != schema.Hash {
		snapshot.Rebuilding = true
	}
	for i := range snapshot.Notes {
		n := &snapshot.Notes[i]
		if n.AssessmentJSON != "" {
			assessment, err := ontology.AssessmentFromJSON(n.AssessmentJSON)
			if err != nil {
				return ShapeResponse{}, err
			}
			if assessment != nil {
				flags := ontology.FlagsForAssessment(assessment)
				n.HasIssues, n.Ambiguous = flags.HasIssues, flags.TypeAmbiguous
			}
		}
	}
	result := shapeFromSnapshot(schema, profiles, snapshot, parts)
	return result, ctx.Err()
}

func shapeNoteType(t *ontology.NoteType) bool {
	return t != nil && (t.Role == ontology.TypeRoleNote || t.Role == "")
}

type shapeRecordPair struct {
	a, b   string
	fields map[[2]string]bool
}

func shapeFromSnapshot(schema *ontology.Schema, profiles map[string]ontology.TypeProfile, snapshot readmodel.ShapeSnapshot, parts ShapeParts) ShapeResponse {
	out := ShapeResponse{Rebuilding: snapshot.Rebuilding}
	notes := map[string]readmodel.ShapeNote{}
	byType := map[string][]readmodel.ShapeNote{}
	folders := map[string]*ShapeFolder{}
	for _, n := range snapshot.Notes {
		if _, exists := notes[n.Path]; exists {
			continue
		}
		if schema == nil || !shapeNoteType(schema.Types[n.Type]) || n.Ambiguous {
			n.Type = UntypedShapeMember
		}
		notes[n.Path] = n
		byType[n.Type] = append(byType[n.Type], n)
		out.TotalNotes++
		if n.Type == UntypedShapeMember {
			out.UntypedNotes++
		} else {
			out.TypedNotes++
		}
		if n.Ambiguous {
			out.AmbiguousNotes++
		}
		if parts.Folders {
			folder := shapeFolder(n.Path)
			if folders[folder] == nil {
				folders[folder] = &ShapeFolder{Folder: folder, Typed: map[string]int{}, UntypedLinksTo: map[string]int{}}
			}
			f := folders[folder]
			f.Total++
			if n.Type == UntypedShapeMember {
				f.Untyped++
			} else {
				f.Typed[n.Type]++
			}
		}
	}
	values := map[string]map[string][]string{}
	for _, f := range snapshot.Fields {
		if values[f.Path] == nil {
			values[f.Path] = map[string][]string{}
		}
		values[f.Path][strings.ToLower(f.Field)] = append(values[f.Path][strings.ToLower(f.Field)], f.Value)
	}
	recordPairs := map[[2]string]*shapeRecordPair{}
	for _, e := range snapshot.Edges {
		src, ok := notes[e.Source]
		if !ok {
			continue
		}
		dst, ok := notes[e.Target]
		if !ok {
			continue
		}
		// Node-scoped relation evidence cannot become a host-note relation.
		// Independent document evidence may still connect those notes as plain links.
		if (e.SourceNode != "" && e.SourceNode != src.NodeID) || (e.TargetNode != "" && e.TargetNode != dst.NodeID) {
			continue
		}
		a, b := src.Path, dst.Path
		if a > b {
			a, b = b, a
		}
		key := [2]string{a, b}
		if recordPairs[key] == nil {
			recordPairs[key] = &shapeRecordPair{a: a, b: b, fields: map[[2]string]bool{}}
		}
		if e.Relation && shapeRelation(schema, src.Type, e.Field, dst.Type) {
			recordPairs[key].fields[[2]string{src.Type, shapeFieldName(schema, src.Type, e.Field)}] = true
		}
	}
	targets := map[string]map[string]bool{}
	pairs := map[[2]string]*ShapePair{}
	fieldCounts := map[[2]string]map[[2]string]int{}
	untypedLinks := 0
	for _, r := range recordPairs {
		a, b := notes[r.a], notes[r.b]
		if a.Type != b.Type {
			if targets[a.Path] == nil {
				targets[a.Path] = map[string]bool{}
			}
			targets[a.Path][b.Type] = true
			if targets[b.Path] == nil {
				targets[b.Path] = map[string]bool{}
			}
			targets[b.Path][a.Type] = true
		}
		if a.Type == UntypedShapeMember || b.Type == UntypedShapeMember {
			untypedLinks++
		}
		if parts.Folders && a.Type != b.Type {
			for _, ends := range [][2]readmodel.ShapeNote{{a, b}, {b, a}} {
				if ends[0].Type == UntypedShapeMember {
					folders[shapeFolder(ends[0].Path)].UntypedLinksTo[ends[1].Type]++
				}
			}
		}
		if !parts.Links {
			continue
		}
		x, y := a.Type, b.Type
		if x > y {
			x, y = y, x
		}
		key := [2]string{x, y}
		if pairs[key] == nil {
			pairs[key] = &ShapePair{A: x, B: y, Fields: []ShapePairField{}}
			fieldCounts[key] = map[[2]string]int{}
		}
		p := pairs[key]
		p.Links++
		if len(r.fields) > 0 {
			p.RelationLinks++
		} else {
			p.PlainLinks++
		}
		for field := range r.fields {
			fieldCounts[key][field]++
		}
	}
	if parts.Members {
		out.Members = &ShapeMembers{Types: []ShapeMember{}, Interfaces: []ShapeInterface{}, Untyped: ShapeUntyped{Count: out.UntypedNotes, Links: untypedLinks}}
		if schema != nil {
			for _, name := range shapeSortedKeys(schema.Types) {
				if !shapeNoteType(schema.Types[name]) {
					continue
				}
				m := shapeMember(name, byType[name], profiles[name], values)
				sets := map[string]*ShapeTargetSet{}
				for _, n := range byType[name] {
					ts := shapeSortedKeys(targets[n.Path])
					key := strings.Join(ts, "\x00")
					if sets[key] == nil {
						sets[key] = &ShapeTargetSet{Types: ts}
					}
					sets[key].Records++
				}
				for _, key := range shapeSortedKeys(sets) {
					m.TargetSets = append(m.TargetSets, *sets[key])
				}
				out.Members.Types = append(out.Members.Types, m)
			}
			for _, name := range shapeSortedKeys(schema.Interfaces) {
				implementors := []string{}
				records := []readmodel.ShapeNote{}
				for _, typ := range shapeSortedKeys(schema.Types) {
					t := schema.Types[typ]
					if shapeNoteType(t) && slices.Contains(t.Implements, name) {
						implementors = append(implementors, typ)
						records = append(records, byType[typ]...)
					}
				}
				m := shapeMember(name, records, profiles[name], values)
				out.Members.Interfaces = append(out.Members.Interfaces, ShapeInterface{Name: name, Count: m.Count, IssueCount: m.IssueCount, LastChanged: m.LastChanged, Lifecycle: m.Lifecycle, Gaps: m.Gaps, Implementors: implementors})
			}
		}
	}
	if parts.Links {
		out.Links = &ShapeLinks{Pairs: []ShapePair{}}
		for key, p := range pairs {
			for field, count := range fieldCounts[key] {
				p.Fields = append(p.Fields, ShapePairField{Type: field[0], Field: field[1], Count: count})
			}
			sort.Slice(p.Fields, func(i, j int) bool {
				if p.Fields[i].Type != p.Fields[j].Type {
					return p.Fields[i].Type < p.Fields[j].Type
				}
				return p.Fields[i].Field < p.Fields[j].Field
			})
			out.Links.Pairs = append(out.Links.Pairs, *p)
		}
		sort.Slice(out.Links.Pairs, func(i, j int) bool {
			a, b := out.Links.Pairs[i], out.Links.Pairs[j]
			if a.A != b.A {
				return a.A < b.A
			}
			return a.B < b.B
		})
	}
	if parts.Folders {
		out.Folders = &ShapeFolders{Rows: []ShapeFolder{}}
		for _, name := range shapeSortedKeys(folders) {
			out.Folders.Rows = append(out.Folders.Rows, *folders[name])
		}
	}
	return out
}

func shapeMember(name string, records []readmodel.ShapeNote, profile ontology.TypeProfile, values map[string]map[string][]string) ShapeMember {
	m := ShapeMember{Name: name, Kind: "type", Count: len(records), Gaps: []ShapeGap{}, TargetSets: []ShapeTargetSet{}}
	lifecycle := map[string]int{}
	for _, n := range records {
		m.LastChanged = max(m.LastChanged, n.Changed)
		seen := map[string]bool{}
		for _, value := range values[n.Path][strings.ToLower(profile.LifecycleField)] {
			if strings.TrimSpace(value) != "" && !seen[value] {
				lifecycle[value]++
				seen[value] = true
			}
		}
	}
	if profile.LifecycleField != "" {
		m.Lifecycle = &ShapeLifecycle{Field: profile.LifecycleField, Values: []ShapeValue{}}
		for _, v := range shapeSortedKeys(lifecycle) {
			m.Lifecycle.Values = append(m.Lifecycle.Values, ShapeValue{Name: v, Count: lifecycle[v]})
		}
	}
	for _, field := range profile.GapFields {
		gap := ShapeGap{Field: field}
		for _, n := range records {
			empty := true
			for _, value := range values[n.Path][strings.ToLower(field)] {
				if strings.TrimSpace(value) != "" {
					empty = false
					break
				}
			}
			if empty {
				gap.Empty++
			}
		}
		m.Gaps = append(m.Gaps, gap)
	}
	return m
}
func shapeSortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
func shapeFolder(path string) string {
	if folder, _, ok := strings.Cut(path, "/"); ok {
		return folder
	}
	return ""
}
func shapeFieldName(schema *ontology.Schema, source, name string) string {
	for _, f := range schema.Types[source].Fields {
		if f != nil && strings.EqualFold(f.Name, name) {
			return f.Name
		}
	}
	return name
}
func shapeRelation(schema *ontology.Schema, source, name, target string) bool {
	if schema == nil || source == UntypedShapeMember || target == UntypedShapeMember {
		return false
	}
	for _, f := range schema.Types[source].Fields {
		if f == nil || !strings.EqualFold(f.Name, name) || f.Kind != ontology.FieldKindLink || f.TypeName == "Note" {
			continue
		}
		return f.TypeName == target || slices.Contains(schema.Types[target].Implements, f.TypeName)
	}
	return false
}
