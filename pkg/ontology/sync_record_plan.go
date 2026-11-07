package ontology

import ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"

// Logical IDs and positional numbering span their declared source set. A
// changed record host rebinds that set, including previously unresolved refs.
func expandRecordBindingPlan(plan *syncPlan, schema *ontoschema.Schema, current, changed, deleted []string) {
	if plan == nil || schema == nil {
		return
	}
	var types []*ontoschema.NoteType
	targets := map[string]*ontoschema.NoteType{}
	for _, typ := range schema.Types {
		if typ.Record != nil {
			types = append(types, typ)
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
	}
	if len(types) == 0 {
		return
	}
	matches := func(path string) bool {
		for _, typ := range types {
			if sourceMatchersMatchNote(typ.SourceMatchers, &noteDoc{Path: path}) {
				return true
			}
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
