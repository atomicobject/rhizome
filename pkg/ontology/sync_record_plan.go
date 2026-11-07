package ontology

import ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"

func recordTypes(schema *ontoschema.Schema) []*ontoschema.NoteType {
	var types []*ontoschema.NoteType
	if schema == nil {
		return nil
	}
	for _, typ := range schema.Types {
		if typ.Record != nil {
			types = append(types, typ)
		}
	}
	return types
}

func matchesAnySource(types []*ontoschema.NoteType, path string) bool {
	for _, typ := range types {
		if sourceMatchersMatchNote(typ.SourceMatchers, &noteDoc{Path: path}) {
			return true
		}
	}
	return false
}

// RecordSourceSetPaths returns touched plus, when a touched path hosts a
// RECORD source, every current RECORD host: positional numbering and logical
// IDs span the declared source set, so semantic chunks refresh with the
// catalog's rebinding. current is read only when such a host is touched.
func RecordSourceSetPaths(schema *ontoschema.Schema, touched []string, current func() ([]string, error)) ([]string, error) {
	types := recordTypes(schema)
	hit := false
	for _, path := range touched {
		if matchesAnySource(types, path) {
			hit = true
			break
		}
	}
	out := append([]string(nil), touched...)
	if hit {
		paths, err := current()
		if err != nil {
			return nil, err
		}
		for _, path := range paths {
			if matchesAnySource(types, path) {
				out = append(out, path)
			}
		}
	}
	return normalizeStringSet(out), nil
}

// Logical IDs and positional numbering span their declared source set. A
// changed record host rebinds that set, including previously unresolved refs.
func expandRecordBindingPlan(plan *syncPlan, schema *ontoschema.Schema, current, changed, deleted []string) {
	if plan == nil || schema == nil {
		return
	}
	types := recordTypes(schema)
	targets := map[string]*ontoschema.NoteType{}
	for _, typ := range types {
		for _, field := range typ.Fields {
			if field.Record == nil || field.Record.References == nil {
				continue
			}
			for _, name := range field.Record.References.To {
				for _, target := range schema.Types {
					if target.Record == nil && ontoschema.TypeMatchesOrImplements(schema, target.Name, name) {
						targets[target.Name] = target
					}
				}
			}
		}
	}
	if len(types) == 0 {
		return
	}
	matches := func(path string) bool {
		if matchesAnySource(types, path) {
			return true
		}
		for _, typ := range targets {
			if len(typ.Paths) == 0 || sourceMatchersMatchNote(typ.Matchers, &noteDoc{Path: path}) {
				return true
			}
		}
		return false
	}
	touched := false
	for _, paths := range [][]string{changed, deleted} {
		for _, path := range paths {
			if matches(path) {
				touched = true
				break
			}
		}
	}
	if !touched {
		return
	}
	if plan.ForcePaths == nil {
		plan.ForcePaths = map[string]struct{}{}
	}
	for _, path := range current {
		if matches(path) {
			plan.LoadPaths = append(plan.LoadPaths, path)
			plan.ReplacePaths = append(plan.ReplacePaths, path)
			plan.ForcePaths[path] = struct{}{}
		}
	}
	plan.LoadPaths = normalizeStringSet(plan.LoadPaths)
	plan.ReplacePaths = normalizeStringSet(plan.ReplacePaths)
}
