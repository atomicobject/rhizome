package ontology

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	ontologyDirName            = ".rhizome/ontology"
	typeFieldName              = "type"
	companionDocsDirectiveName = "companionDocs"
)

var (
	ErrNoOntologyFiles = fmt.Errorf("no ontology files found")
)

// OntologyDir returns the committed SDL directory inside a vault/repo root.
func OntologyDir(vaultPath string) string {
	return filepath.Join(vaultPath, ontologyDirName)
}

// TypeFieldName returns the frontmatter field used to declare a note's type.
func TypeFieldName() string {
	return typeFieldName
}

type authoredSchemaFile struct {
	path string
	data []byte
}

// SchemaSourceHash fingerprints authored SDL without compiling the schema.
func SchemaSourceHash(vaultPath string) (string, error) {
	_, hash, err := readAuthoredSchemaFiles(vaultPath)
	return hash, err
}

func readAuthoredSchemaFiles(vaultPath string) ([]authoredSchemaFile, string, error) {
	dir := OntologyDir(vaultPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", ErrNoOntologyFiles
		}
		return nil, "", err
	}
	hasher := sha256.New()
	files := make([]authoredSchemaFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".graphql" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, "", err
		}
		_, _ = hasher.Write([]byte(entry.Name()))
		_, _ = hasher.Write([]byte{0})
		_, _ = hasher.Write(data)
		_, _ = hasher.Write([]byte{0})
		files = append(files, authoredSchemaFile{path: path, data: data})
	}
	if len(files) == 0 {
		return nil, "", ErrNoOntologyFiles
	}
	return files, hex.EncodeToString(hasher.Sum(nil)), nil
}

// LoadSchema compiles all ontology SDL files into Rhizome's runtime model.
//
// The schema hash is based on authored files, not generated prelude text, so
// snapshot dirtiness tracks user-controlled ontology changes. Treat
// ErrNoOntologyFiles as "ontology disabled"; other errors mean committed config
// is invalid and should be surfaced to callers.
func LoadSchema(vaultPath string) (*Schema, error) {
	dir := OntologyDir(vaultPath)
	authoredFiles, sourceHash, err := readAuthoredSchemaFiles(vaultPath)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(authoredFiles))

	authoredSources := make([]schemaSourceInput, 0, len(authoredFiles))
	noteInterfaceDefined := false
	noteInterfaceExtended := false

	for _, file := range authoredFiles {
		files = append(files, file.path)
		raw := string(file.data)
		rel := filepath.ToSlash(filepath.Base(file.path))
		// IMPORTANT: inject generated Section fields after hashing authored bytes.
		// Compiler/prelude changes should not dirty every vault snapshot when the
		// repo's SDL did not change.
		input := injectSectionPreludeFields(raw)
		noteDecl, detectErr := detectAuthoredNoteDeclarations(rel, input)
		if detectErr != nil {
			return nil, detectErr
		}
		if noteDecl.concrete {
			return nil, fmt.Errorf("ontology compile error: `type Note` is reserved; declare shared general fields with `interface Note` and use a separate concrete @node(default: true) type for fallback notes")
		}
		if noteDecl.interfaceDefined {
			noteInterfaceDefined = true
		}
		if noteDecl.interfaceExtended {
			noteInterfaceExtended = true
		}
		authoredSources = append(authoredSources, schemaSourceInput{name: rel, input: input})
	}

	noteExtensionInput := ""
	if noteInterfaceDefined || noteInterfaceExtended {
		var extensionErr error
		noteExtensionInput, extensionErr = noteInterfaceImplementerExtensions(authoredSources)
		if extensionErr != nil {
			return nil, extensionErr
		}
	}

	sources := make([]*ast.Source, 0, len(files)+1)
	sources = append(sources, &ast.Source{
		Name:    "rhizome-ontology-prelude.graphql",
		BuiltIn: true,
		Input:   ontologyPreludeInput(!noteInterfaceDefined),
	})
	for _, source := range authoredSources {
		sources = append(sources, &ast.Source{Name: source.name, Input: source.input})
	}
	if noteExtensionInput != "" {
		sources = append(sources, &ast.Source{Name: "rhizome-note-interface-extensions.graphql", BuiltIn: true, Input: noteExtensionInput})
	}

	doc, err := gqlparser.LoadSchema(sources...)
	if err != nil {
		return nil, err
	}

	schema := &Schema{
		Hash:                  sourceHash,
		Dir:                   dir,
		Files:                 files,
		Enums:                 make(map[string]map[string]struct{}),
		EnumTypes:             make(map[string]*EnumType),
		Types:                 make(map[string]*NoteType),
		Interfaces:            make(map[string]*InterfaceType),
		Scalars:               defaultScalarSet(),
		NoteInterfaceAuthored: noteInterfaceDefined || noteInterfaceExtended,
	}

	names := make([]string, 0, len(doc.Types))
	for name := range doc.Types {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		def := doc.Types[name]
		switch {
		case def == nil, def.BuiltIn:
			continue
		case name == "Query" || name == "Mutation" || name == "Subscription":
			return nil, compileError(def.Position, "root operation types are not supported in ontology files")
		case def.Kind == ast.Union || def.Kind == ast.InputObject:
			return nil, compileError(def.Position, "unsupported GraphQL definition kind %s", def.Kind)
		case def.Kind == ast.Scalar:
			if name != "Date" && name != "DateTime" && name != "URL" {
				return nil, compileError(def.Position, "unsupported scalar %q", name)
			}
			schema.Scalars[name] = struct{}{}
		case def.Kind == ast.Enum:
			values := make(map[string]struct{}, len(def.EnumValues))
			enumType := &EnumType{
				Name:        name,
				Description: strings.TrimSpace(def.Description),
				Guidance:    guidanceFromDescriptionAndDirective(def.Description, def.Directives.ForName("guidance")),
				Values:      make([]*EnumValue, 0, len(def.EnumValues)),
				ByName:      make(map[string]*EnumValue, len(def.EnumValues)),
			}
			for _, value := range def.EnumValues {
				values[value.Name] = struct{}{}
				enumValue := &EnumValue{
					Name:        value.Name,
					Description: strings.TrimSpace(value.Description),
					Guidance:    guidanceFromDescriptionAndDirective(value.Description, value.Directives.ForName("guidance")),
				}
				enumValue.Policy = policyHintFromDirective(value.Directives.ForName("policy"))
				viewDirective := value.Directives.ForName("view")
				enumValue.View, err = enumValueViewFromDirective(viewDirective)
				if err != nil {
					return nil, compileError(viewDirective.Position, "invalid @view directive on %s.%s: %v", name, value.Name, err)
				}
				enumType.Values = append(enumType.Values, enumValue)
				enumType.ByName[enumValue.Name] = enumValue
			}
			if missing := enumType.valuesMissingStage(); len(missing) > 0 {
				return nil, compileError(def.Position, "enum %s declares @view(stage:) on some values but not on %s; declare a stage on every value or none", name, strings.Join(missing, ", "))
			}
			schema.Enums[name] = values
			schema.EnumTypes[name] = enumType
		}
	}

	for _, name := range names {
		def := doc.Types[name]
		switch {
		case def == nil:
			continue
		case def.BuiltIn && !(name == "Note" && noteInterfaceExtended):
			continue
		case name == "Query" || name == "Mutation" || name == "Subscription":
			return nil, compileError(def.Position, "root operation types are not supported in ontology files")
		case def.Kind == ast.Union || def.Kind == ast.InputObject:
			return nil, compileError(def.Position, "unsupported GraphQL definition kind %s", def.Kind)
		case def.Kind == ast.Scalar || def.Kind == ast.Enum:
			continue
		case def.Kind == ast.Interface:
			iface, compileErr := compileInterfaceType(def, doc, schema)
			if compileErr != nil {
				return nil, compileErr
			}
			if name == "Note" && !schema.NoteInterfaceAuthored {
				continue
			}
			schema.Interfaces[iface.Name] = iface
		case def.Kind == ast.Object:
			if name == "Note" {
				return nil, compileError(def.Position, "`type Note` is reserved; declare shared general fields with `interface Note` and use a separate concrete @node(default: true) type for fallback notes")
			}
			noteType, compileErr := compileNoteType(def, doc, schema)
			if compileErr != nil {
				return nil, compileErr
			}
			if noteType.Default {
				if schema.DefaultNoteType != "" {
					return nil, compileError(def.Position, "only one @node(default: true) note type is allowed; %s and %s both declare default", schema.DefaultNoteType, noteType.Name)
				}
				schema.DefaultNoteType = noteType.Name
			}
			schema.Types[noteType.Name] = noteType
		default:
			return nil, compileError(def.Position, "unsupported GraphQL definition kind %s", def.Kind)
		}
	}

	injectNoteInterfaceFields(schema)
	if err := applyInheritedFieldDisplay(schema); err != nil {
		return nil, err
	}
	if err := validateIdentifierNamespaces(schema); err != nil {
		return nil, err
	}

	for _, noteType := range schema.Types {
		if noteType == nil || effectiveTypeRole(noteType) != TypeRoleNote {
			continue
		}
		for _, field := range noteType.Fields {
			if field.Kind == FieldKindReverse {
				sources := interfaceImplementersOrConcrete(schema, field.TypeName)
				if len(sources) == 0 {
					return nil, fmt.Errorf("ontology type %s.%s reverse source type %s has no concrete note types", noteType.Name, field.Name, field.TypeName)
				}
				for _, source := range sources {
					// A plain Section has no authored @link source. Embedded nodes do,
					// and must retain their NodeRef identity in reverse reads.
					if role := effectiveTypeRole(source); role != TypeRoleNote && role != TypeRoleEmbeddedNode {
						return nil, fmt.Errorf("reverse field %s.%s source %s must be a file-backed note or embedded node", noteType.Name, field.Name, source.Name)
					}
					linked := source.ByName[field.ReverseField]
					if linked == nil || linked.Kind != FieldKindLink || !typeMatchesOrImplements(schema, noteType.Name, linked.TypeName) {
						return nil, fmt.Errorf("ontology type %s.%s reverse field %q requires %s.%s to be an authored @link targeting %s", noteType.Name, field.Name, field.ReverseField, source.Name, field.ReverseField, noteType.Name)
					}
				}
				continue
			}
			if field.Kind != FieldKindLink || field.Inverse == "" || field.TypeName == "Note" {
				continue
			}
			targetTypes := interfaceImplementersOrConcrete(schema, field.TypeName)
			if len(targetTypes) == 0 {
				return nil, fmt.Errorf("ontology type %s.%s references unknown target type %s", noteType.Name, field.Name, field.TypeName)
			}
			for _, targetType := range targetTypes {
				inverseField := targetType.ByName[field.Inverse]
				if inverseField == nil {
					return nil, fmt.Errorf("ontology type %s.%s inverse %q not found on target type %s", noteType.Name, field.Name, field.Inverse, targetType.Name)
				}
				if inverseField.Kind != FieldKindLink {
					return nil, fmt.Errorf("ontology type %s.%s inverse %q on target type %s must point to a @link field", noteType.Name, field.Name, field.Inverse, targetType.Name)
				}
				if !typeMatchesOrImplements(schema, noteType.Name, inverseField.TypeName) {
					return nil, fmt.Errorf("ontology type %s.%s inverse %q on target type %s must target %s, got %s", noteType.Name, field.Name, field.Inverse, targetType.Name, noteType.Name, inverseField.TypeName)
				}
			}
		}
	}
	// Compile-time validation does cross-type checks once so query, index, and
	// semantic-indexing paths can assume a coherent runtime Schema.
	for _, iface := range schema.Interfaces {
		if err := validateImplements(schema, iface.Name, iface.Role, iface.Fields, iface.Implements); err != nil {
			return nil, err
		}
	}
	for _, noteType := range schema.Types {
		if err := validateImplements(schema, noteType.Name, noteType.Role, noteType.Fields, noteType.Implements); err != nil {
			return nil, err
		}
	}

	return schema, nil
}

func (s *Schema) MatchDoc(typeName string, doc *noteDoc) bool {
	noteType := s.Types[typeName]
	if noteType == nil || effectiveTypeRole(noteType) != TypeRoleNote || doc == nil {
		return false
	}
	for _, matcher := range noteType.Matchers {
		if matcher.matches(doc) {
			return true
		}
	}
	return false
}

func intArg(v any, fallback int) int {
	if n, ok := coerceInt(v); ok {
		return n
	}
	return fallback
}

func unwrapType(t *ast.Type) (list bool, required bool, name string) {
	if t == nil {
		return false, false, ""
	}
	required = t.NonNull
	if t.NamedType != "" {
		return false, required, t.NamedType
	}
	if t.Elem == nil {
		return false, required, ""
	}
	return true, required, t.Elem.Name()
}

func defaultScalarSet() map[string]struct{} {
	return map[string]struct{}{
		"String":   {},
		"Int":      {},
		"Float":    {},
		"Boolean":  {},
		"ID":       {},
		"Date":     {},
		"DateTime": {},
		"URL":      {},
	}
}

func compileError(pos *ast.Position, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	if pos == nil || pos.Src == nil {
		return fmt.Errorf("ontology compile error: %s", msg)
	}
	return fmt.Errorf("%s:%d:%d: %s", pos.Src.Name, pos.Line, pos.Column, msg)
}

func stringArg(v any, fallback string) string {
	s, ok := v.(string)
	if !ok {
		return fallback
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return fallback
	}
	return s
}

func boolArg(v any, fallback bool) bool {
	b, ok := v.(bool)
	if !ok {
		return fallback
	}
	return b
}

func optionalIntArg(args map[string]any, key string) (int, bool) {
	if len(args) == 0 {
		return 0, false
	}
	raw, ok := args[key]
	if !ok || raw == nil {
		return 0, false
	}
	value, ok := coerceInt(raw)
	return value, ok
}

func stringListArg(v any) ([]string, error) {
	if v == nil {
		return nil, nil
	}
	items, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("expected list, got %T", v)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("expected string entry, got %T", item)
		}
		s = strings.TrimSpace(filepath.ToSlash(s))
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

func enumType(schema *Schema, typeName string) *EnumType {
	if schema == nil {
		return nil
	}
	return schema.EnumTypes[typeName]
}

func EnumValuesSet(schema *Schema, typeName string) map[string]struct{} {
	if schema == nil {
		return nil
	}
	if values := schema.Enums[typeName]; len(values) > 0 {
		return values
	}
	enumType := enumType(schema, typeName)
	if enumType == nil {
		return nil
	}
	values := make(map[string]struct{}, len(enumType.Values))
	for _, value := range enumType.Values {
		if value == nil {
			continue
		}
		values[value.Name] = struct{}{}
	}
	if len(values) == 0 {
		return nil
	}
	schema.Enums[typeName] = values
	return values
}
