package guide

import (
	"fmt"
	"github.com/atomicobject/rhizome/pkg/ontology"
	"strings"
)

func RenderMarkdown(schema *ontology.Schema, requested []string) (string, error) {
	return RenderMarkdownWithResolver(schema, requested, nil)
}

type CompanionDocMeta struct {
	Title   string
	Summary string
}

type CompanionDocResolver func(path string) (CompanionDocMeta, bool)

// IDLookup returns the next available identifier for a typed note family. The
// renderer uses it to inject a "Suggested next id" line into the per-type
// authoring narrative and to seed the rendered skeleton's `id:` line. Lookups
// only fire for types that declare `@identifier(preferred: true, prefix: ...)`,
// so omitting IDLookup is always safe.
//
// IDLookup may return ok=false (with no error) when the lookup is plausible
// but currently unavailable -- for example, the underlying store hasn't been
// indexed yet. Errors are non-fatal: the renderer falls back to a blank
// skeleton id and skips the suggestion line.
type IDLookup func(typeName string) (next string, ok bool, err error)

func RenderMarkdownWithResolver(schema *ontology.Schema, requested []string, resolve CompanionDocResolver) (string, error) {
	return RenderMarkdownWithLookups(schema, requested, resolve, nil)
}

// RenderMarkdownWithLookups is the authoritative entrypoint. resolve and
// idLookup may be nil; the renderer degrades gracefully when either is
// unavailable.
func RenderMarkdownWithLookups(schema *ontology.Schema, requested []string, resolve CompanionDocResolver, idLookup IDLookup) (string, error) {
	if schema == nil {
		return "", fmt.Errorf("ontology schema is required")
	}
	primary, supporting, err := selectTypes(schema, requested)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("# Ontology Authoring Guide\n\n")
	b.WriteString("Write note markdown to satisfy the ontology selectors and field contracts below. ")
	b.WriteString("Use `type: <TypeName>` as author intent and disambiguation, but selectors (`paths` / `matches`) still control whether a note belongs to a type.\n")

	support := supportingSet(supporting)
	for _, typeName := range primary {
		renderFullType(&b, schema, guideTypeFromSchema(schema, typeName), support, resolve, idLookup)
	}

	if len(supporting) > 0 {
		b.WriteString("\n## Supporting Types\n")
		b.WriteString("\nThese related types are included because they are authored links, contained sections, or shared interfaces needed while drafting this type.\n")
		for _, typeName := range supporting {
			renderSupportingType(&b, schema, guideTypeFromSchema(schema, typeName), resolve)
		}
	}
	return b.String(), nil
}

// resolveSuggestedID asks the IDLookup for the next id when noteType declares
// a preferred-identifier field with a known format. Returns "" for types that
// don't declare a format or when the lookup fails (errors are best-effort).
func resolveSuggestedID(noteType *guideType, idLookup IDLookup) string {
	if noteType == nil || idLookup == nil {
		return ""
	}
	field := preferredIdentifierField(noteType)
	if field == nil || field.IdentifierFormat == nil || field.IsDerivableIdentifier || strings.TrimSpace(field.DerivedSuffix) != "" {
		return ""
	}
	next, ok, err := idLookup(noteType.Name)
	if err != nil || !ok {
		return ""
	}
	return next
}

func preferredIdentifierField(noteType *guideType) *ontology.Field {
	if noteType == nil {
		return nil
	}
	for _, f := range noteType.Fields {
		if f != nil && f.IsPreferredIdentifier {
			return f
		}
	}
	return nil
}

func renderFullType(b *strings.Builder, schema *ontology.Schema, noteType *guideType, support map[string]struct{}, resolve CompanionDocResolver, idLookup IDLookup) {
	if noteType == nil {
		return
	}
	b.WriteString("\n## ")
	b.WriteString(noteType.Name)
	b.WriteByte('\n')
	if noteType.Summary != "" {
		b.WriteByte('\n')
		b.WriteString(noteType.Summary)
		b.WriteByte('\n')
	}
	renderGuideGuidance(b, noteType.Meaning, noteType.Authoring, noteType.AgentImplications, "")

	b.WriteString("\n### Resolution\n")
	switch noteType.Role {
	case ontology.TypeRoleNote:
		if len(noteType.Paths) > 0 {
			b.WriteString("\n- paths: ")
			b.WriteString(strings.Join(noteType.Paths, ", "))
		}
		if len(noteType.Matches) > 0 {
			b.WriteString("\n- matches: ")
			b.WriteString(strings.Join(noteType.Matches, ", "))
		}
		b.WriteString("\n- declared type: set `type: ")
		b.WriteString(noteType.Name)
		b.WriteString("` in frontmatter when authoring or disambiguating this note type")
		if noteType.PropertyCase != "" {
			b.WriteString("\n- default property case: `")
			b.WriteString(strings.ToLower(strings.ReplaceAll(string(noteType.PropertyCase), "_", "-")))
			b.WriteString("`")
		}
	case ontology.TypeRoleEmbeddedNode:
		b.WriteString("\n- embedded node type: persisted inside a parent note body but treated as a first-class node")
		switch noteType.SourceShape {
		case ontology.EmbeddedSourceShapeCheckboxItem:
			b.WriteString("\n- shape: `CHECKBOX_ITEM` — each instance is a markdown checkbox list item (`- [ ] ...` or `- [x] ...`)")
			if noteType.SourceMarker != "" {
				b.WriteString("\n- marker: every instance must include the literal tag `")
				b.WriteString(noteType.SourceMarker)
				b.WriteString("` somewhere on the checkbox line; lines without the marker are not parsed as this type")
			}
			if len(noteType.SourcePaths) > 0 && !sourcePathsMatchAll(noteType.SourcePaths) {
				b.WriteString("\n- source paths: ")
				b.WriteString(strings.Join(noteType.SourcePaths, ", "))
			}
			b.WriteString("\n- the checkbox text after the bullet is the body/title of the node; the leading `[ ]` / `[x]` token is the `done` state and must not be omitted")
			b.WriteString("\n- do not add a `type:` frontmatter key; the type is inferred from the marker tag, not from frontmatter")
			b.WriteString("\n- author inline properties on indented continuation lines directly under the checkbox item, one `key:: value` per line")
		case ontology.EmbeddedSourceShapeListItem:
			b.WriteString("\n- shape: `LIST_ITEM` — each instance is a plain markdown list item (`- ...`) without a checkbox token")
			if noteType.SourceMarker != "" {
				b.WriteString("\n- marker: every instance must include the literal tag `")
				b.WriteString(noteType.SourceMarker)
				b.WriteString("` somewhere on the list line")
			}
			if len(noteType.SourcePaths) > 0 && !sourcePathsMatchAll(noteType.SourcePaths) {
				b.WriteString("\n- source paths: ")
				b.WriteString(strings.Join(noteType.SourcePaths, ", "))
			}
			b.WriteString("\n- the list-item text is the body/title of the node")
			b.WriteString("\n- do not add a `type:` frontmatter key; the type is inferred from the marker tag, not from frontmatter")
			b.WriteString("\n- author inline properties on indented continuation lines directly under the list item, one `key:: value` per line")
		default:
			b.WriteString("\n- shape: `SECTION` — instances derive from matching markdown headings inside an owning note")
			b.WriteString("\n- author embedded-node properties as metadata bullets by default, one `- key:: value` line per field")
			b.WriteString("\n- packed `key:: value` paragraphs are tolerated for old notes, but metadata bullets are preferred for readability")
		}
	case ontology.TypeRoleSection:
		b.WriteString("\n- section type: derived from matching markdown headings; not a standalone note type")
	default:
		b.WriteString("\n- shared interface contract: implemented by concrete note or section types; not a standalone note type")
		if len(noteType.Implements) > 0 {
			b.WriteString("\n- extends interfaces: ")
			b.WriteString(strings.Join(noteType.Implements, ", "))
		}
	}

	authored := authoredGuideTypeFields(noteType)
	derived := derivedFields(noteType)
	if len(authored.required) > 0 || len(authored.optional) > 0 {
		b.WriteString("\n\n### Authored Fields\n")
		for _, field := range authored.required {
			renderFieldGuide(b, schema, noteType, field, true, resolve)
		}
		for _, field := range authored.optional {
			renderFieldGuide(b, schema, noteType, field, false, resolve)
		}
	}
	if len(derived) > 0 {
		b.WriteString("\n\n### Derived Fields\n")
		for _, field := range derived {
			b.WriteString("\n- `")
			b.WriteString(field.Name)
			b.WriteString("`: query-only ")
			if field.Kind == ontology.FieldKindNeighbor {
				b.WriteString("neighbor set")
			} else if field.Kind == ontology.FieldKindReverse {
				b.WriteString("reverse of authored link `")
				b.WriteString(field.TypeName + "." + field.ReverseField)
				b.WriteString("`")
			} else {
				b.WriteString("derived field")
			}
			b.WriteString("; do not write it into note markdown")
			if field.TypeName != "" {
				b.WriteString("; target type `")
				b.WriteString(field.TypeName)
				if field.List {
					b.WriteString("[]")
				}
				b.WriteString("`")
			}
			if field.Direction != "" {
				b.WriteString("; direction `")
				b.WriteString(string(field.Direction))
				b.WriteString("`")
			}
			if field.Kind == ontology.FieldKindReverse {
				b.WriteString("; excludes prose mentions")
			}
		}
	}

	if notes := toolingNotes(noteType); len(notes) > 0 {
		b.WriteString("\n\n### Tooling Semantics\n")
		for _, note := range notes {
			b.WriteString("\n- ")
			b.WriteString(note)
		}
	}

	if noteType.Role == ontology.TypeRoleEmbeddedNode && preferredIdentifierField(noteType) == nil {
		b.WriteString("\n\n### Link Targets\n")
		b.WriteString("\n- this embedded node has no authored identifier field; do not invent an `id::` property for it")
		b.WriteString("\n- use `rzm agent node-link --target <note#fragment> --ensure plan` before citing it from another artifact")
		b.WriteString("\n- Rhizome can mint a plain standalone `^block-id` locator on demand when a durable link target is needed")
		b.WriteString("\n- leave uncited nodes without authored locators")
	}

	if len(noteType.CompanionDocs) > 0 {
		b.WriteString("\n\n### Companion Docs\n")
		b.WriteString("\nRead these when you need workflow, examples, or vocabulary that would be too bulky for the schema docstring.\n")
		for _, doc := range noteType.CompanionDocs {
			b.WriteString("\n- ")
			b.WriteString(formatCompanionDoc(doc, resolve))
		}
	}

	b.WriteString("\n\n### Validation Traps\n")
	for _, issue := range validationGuideNotes(schema, noteType.Role, noteType.Fields) {
		b.WriteString("\n- ")
		b.WriteString(issue)
	}

	related := relatedTypes(schema, noteType.Name)
	if len(related) > 0 {
		b.WriteString("\n\n### Related Types\n")
		for _, typeName := range related {
			b.WriteString("\n- `")
			b.WriteString(typeName)
			b.WriteString("`")
			if _, ok := support[typeName]; ok {
				b.WriteString(" appears below in the supporting appendix")
			}
			b.WriteString(": ")
			b.WriteString(relatedReason(schema, noteType.Name, typeName))
			if guidance := relatedTypeGuidance(schema, typeName); guidance != "" {
				b.WriteString(". ")
				b.WriteString(guidance)
			}
		}
	}

	suggestedID := resolveSuggestedID(noteType, idLookup)
	idField := preferredIdentifierField(noteType)
	if shouldRenderIDAllocation(noteType, idField, suggestedID) {
		b.WriteString("\n\n### ID Allocation\n")
		if idField.IdentifierFormat != nil {
			b.WriteString("\n- allocation strategy: `")
			b.WriteString(string(idField.IdentifierFormat.Strategy))
			b.WriteString("`")
		}
		if noteType.Role == ontology.TypeRoleEmbeddedNode {
			if idField != nil && idField.IdentifierPopulate == ontology.IdentifierPopulateOnCreate {
				b.WriteString("\n- author the identifier on creation as a block-safe inline property like `")
				b.WriteString(authoredSource(idField, noteType))
				b.WriteString(":: ^<id>`")
				if idField.IsDerivableIdentifier {
					b.WriteString("\n- derive the id from the parent identifier plus the schema `derivedSuffix`; keep existing authored ids stable and never renumber them")
				}
				b.WriteString("\n- use the identifier-backed block target for durable links; do not add a duplicate standalone block-id line")
			} else {
				b.WriteString("\n- this embedded identifier is generated from structure and authored only when a durable external link target is needed")
				b.WriteString("\n- use `rzm agent node-link --target <note#fragment> --ensure plan` before citing the node so Rhizome can plan the locator")
				b.WriteString("\n- when the preferred identifier field can carry the anchor, use its block-safe inline form instead of a standalone block-id line")
			}
		} else {
			if suggestedID != "" {
				b.WriteString("\n- next available id: `")
				b.WriteString(suggestedID)
				b.WriteString("` (use this when authoring one new note of this type)")
			}
			if idField.IdentifierFormat != nil && idField.IdentifierFormat.Strategy == ontology.IdentifierStrategyDateTime {
				b.WriteString("\n- choose the prospective vault-relative path first; its basename must start with a valid local `YYYY-MM-DD-HH-MM` calendar-minute stamp")
				b.WriteString("\n- allocate the id with `rzm agent next-id --type ")
				b.WriteString(noteType.Name)
				b.WriteString(" --path <prospective-vault-relative-path>`; use repeatable `--path` flags when allocating several notes before re-indexing")
			} else {
				b.WriteString("\n- allocate ids with `rzm agent next-id --type ")
				b.WriteString(noteType.Name)
				b.WriteString("`; if you are creating multiple notes before re-indexing, call `rzm agent next-id --type ")
				b.WriteString(noteType.Name)
				b.WriteString(" --count <N>` once and use the returned `ids` in order")
			}
			b.WriteString("\n- write the id into frontmatter `")
			b.WriteString(authoredSource(idField, noteType))
			b.WriteString(":`")
			if hasAuthoredAliasesField(noteType) {
				b.WriteString(" and mirror the same value into the note's `aliases:` list")
				if suggestedID != "" {
					b.WriteString(" so wikilinks like `[[")
					b.WriteString(suggestedID)
					b.WriteString("]]` resolve")
				}
			}
			b.WriteString("\n- validate after writing with `rzm agent validate all` (includes identifiers and ontology)")
		}
		if idField.IdentifierFormat != nil && idField.IdentifierFormat.Strategy == ontology.IdentifierStrategyDateTime {
			b.WriteString("\n- the filename seeds the id only at creation; later file moves do not rederive or rekey the stable id")
		} else {
			b.WriteString("\n- ids are gap-tolerant: never reuse an old number even if a note was deleted or archived")
		}
	}

	if noteType.Role != ontology.TypeRoleInterface {
		b.WriteString("\n\n### Skeleton\n\n```md\n")
		b.WriteString(skeletonWithID(schema, noteType, suggestedID))
		b.WriteString("\n```\n")
	}
}

func shouldRenderIDAllocation(noteType *guideType, field *ontology.Field, suggestedID string) bool {
	if noteType == nil || field == nil {
		return false
	}
	if suggestedID != "" {
		return true
	}
	return field.Required && (field.IdentifierFormat != nil || noteType.Role == ontology.TypeRoleEmbeddedNode)
}

func hasAuthoredAliasesField(noteType *guideType) bool {
	if noteType == nil {
		return false
	}
	for _, field := range noteType.Fields {
		if field == nil || field.Kind == ontology.FieldKindNeighbor || field.Kind == ontology.FieldKindReverse || field.Kind == ontology.FieldKindSection {
			continue
		}
		if authoredSource(field, noteType) == "aliases" {
			return true
		}
	}
	return false
}

func renderSupportingType(b *strings.Builder, schema *ontology.Schema, noteType *guideType, resolve CompanionDocResolver) {
	if noteType == nil {
		return
	}
	b.WriteString("\n### ")
	b.WriteString(noteType.Name)
	b.WriteByte('\n')
	if noteType.Summary != "" {
		b.WriteString("\n")
		b.WriteString(noteType.Summary)
		b.WriteByte('\n')
	}
	renderGuideGuidance(b, noteType.Meaning, noteType.Authoring, noteType.AgentImplications, "")
	if noteType.Role == ontology.TypeRoleNote && len(noteType.Paths) > 0 {
		b.WriteString("\n- paths: ")
		b.WriteString(strings.Join(noteType.Paths, ", "))
	}
	if noteType.Role == ontology.TypeRoleNote && len(noteType.Matches) > 0 {
		b.WriteString("\n- matches: ")
		b.WriteString(strings.Join(noteType.Matches, ", "))
	}
	if noteType.Role == ontology.TypeRoleNote {
		b.WriteString("\n- declared type: `type: ")
		b.WriteString(noteType.Name)
		b.WriteString("`")
	} else if noteType.Role == ontology.TypeRoleSection {
		b.WriteString("\n- section type: heading-derived structure used inside parent note bodies")
	} else if noteType.Role == ontology.TypeRoleEmbeddedNode {
		b.WriteString("\n- embedded node type: authored inside a parent note body but treated as a first-class node")
		b.WriteString("\n- declared type: `type: ")
		b.WriteString(noteType.Name)
		b.WriteString("`")
	} else {
		b.WriteString("\n- shared interface contract used by implementing types")
	}
	if len(noteType.CompanionDocs) > 0 {
		b.WriteString("\n- companion docs: ")
		b.WriteString(strings.Join(formatCompanionDocs(noteType.CompanionDocs, resolve), "; "))
	}
	fields := authoredGuideTypeFields(noteType)
	for _, field := range append(fields.required, fields.optional...) {
		renderFieldGuide(b, schema, noteType, field, field.Required, resolve)
	}
}
