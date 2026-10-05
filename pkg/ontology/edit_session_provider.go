package ontology

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/notemeta"
	"github.com/atomicobject/rhizome/pkg/paths"
)

func (s *EditSession) loadEditDocumentSnapshot(ctx context.Context, notePath string) (*DocumentSnapshot, error) {
	if s.formats == nil {
		return LoadDocumentSnapshot(ctx, s.vaultDef, s.noteMgr, notePath)
	}
	clean, err := paths.CleanNotePath(notePath)
	if err != nil {
		return nil, err
	}
	provider, selected := s.formats.ProviderForPath(paths.RelPath(clean))
	if !selected || provider.Descriptor().Capabilities.Has(noteformat.CapabilityStructuralContentMutation) {
		return LoadDocumentSnapshot(ctx, s.vaultDef, s.noteMgr, notePath)
	}
	vaultPaths, err := paths.NewVaultPaths(s.vaultDef.BasePath())
	if err != nil {
		return nil, err
	}
	abs, err := vaultPaths.AbsNotePath(clean)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(abs.String())
	if err != nil {
		return nil, err
	}
	info, statErr := os.Stat(abs.String())
	var modTime time.Time
	if statErr == nil {
		modTime = info.ModTime()
	}
	content := string(raw)
	return &DocumentSnapshot{
		NotePath: notePath, Content: content, ModTime: modTime,
		ContentFingerprint: hashText(content),
	}, nil
}

func (s *documentState) providerRootOnly() (bool, error) {
	if s == nil || s.formats == nil {
		return false, nil
	}
	clean, err := paths.CleanNotePath(s.notePath)
	if err != nil {
		return false, err
	}
	provider, selected := s.formats.ProviderForPath(paths.RelPath(clean))
	if !selected {
		return false, nil
	}
	descriptor := provider.Descriptor()
	return !descriptor.Capabilities.Has(noteformat.CapabilityStructuralContentMutation), nil
}

func (s *documentState) projectProviderRoot(content string) (noteformat.AuthoredSource, noteformat.Projection, *NodeProjection, error) {
	if s == nil || s.formats == nil {
		return noteformat.AuthoredSource{}, noteformat.Projection{}, nil, fmt.Errorf("note format runtime is required")
	}
	path, err := paths.CleanNotePath(s.notePath)
	if err != nil {
		return noteformat.AuthoredSource{}, noteformat.Projection{}, nil, err
	}
	provider, selected := s.formats.ProviderForPath(paths.RelPath(path))
	if !selected {
		return noteformat.AuthoredSource{}, noteformat.Projection{}, nil, fmt.Errorf("no note format provider claims %s", s.notePath)
	}
	mtime := int64(0)
	if s.baseSnapshot != nil {
		mtime = s.baseSnapshot.ModTime.Unix()
	}
	source, err := noteformat.NewAuthoredSource(path, provider.Descriptor(), []byte(content), mtime)
	if err != nil {
		return noteformat.AuthoredSource{}, noteformat.Projection{}, nil, err
	}
	projection, err := s.formats.Project(source)
	if err != nil {
		return noteformat.AuthoredSource{}, noteformat.Projection{}, nil, err
	}
	metadata := make(map[string]any, len(projection.Facts.RootMetadata))
	for _, fact := range projection.Facts.RootMetadata {
		metadata[fact.Key] = fact.Value.Export()
	}
	title := ""
	if projection.Facts.Title != nil {
		title = projection.Facts.Title.Value
	}
	root, err := BuildRootDocumentSnapshot(notemeta.NoteSourceSnapshot{
		Path: path, Format: source.Format(), RawSource: source.Bytes(), ContentHash: source.ContentHash(),
		Mtime: mtime, Projection: projection, Frontmatter: metadata, Title: title,
	})
	if err != nil {
		return noteformat.AuthoredSource{}, noteformat.Projection{}, nil, err
	}
	node, err := ProjectRootDocumentSnapshot(root, s.schema)
	if err != nil {
		return noteformat.AuthoredSource{}, noteformat.Projection{}, nil, err
	}
	return source, projection, node, nil
}

func applyProviderRootFieldOps(state *documentState, ops []setFieldOp) (editOp, error) {
	if len(ops) == 0 {
		return nil, nil
	}
	if state.content != state.baseContent {
		if err := providerRootTargetsUnchanged(state, ops); err != nil {
			return ops[0], err
		}
	}
	source, projection, node, err := state.projectProviderRoot(state.content)
	if err != nil {
		return ops[0], err
	}
	changes := make([]noteformat.RootMetadataChange, 0, len(ops))
	for _, op := range ops {
		if !op.RawRootMetadata && !strings.EqualFold(strings.TrimSpace(op.Field), typeFieldName) {
			if _, err := validateSetFieldOperation(node, op, state.schema, state.allowSelectorRecovery); err != nil {
				return op, err
			}
		}
		change, err := providerRootMetadataChange(state.schema, node, projection, op)
		if err != nil {
			return op, err
		}
		if change.Delete {
			_, present, presenceErr := metadataFactForField(state.schema, projection, op)
			if presenceErr != nil {
				return op, presenceErr
			}
			if !present {
				continue
			}
		}
		changes = append(changes, change)
	}
	if len(changes) == 0 {
		return nil, nil
	}
	plan, err := state.formats.PlanRootMetadataPatch(source, projection, changes)
	if err != nil {
		return ops[0], err
	}
	updated := source.Bytes()
	for index := len(plan.Patches) - 1; index >= 0; index-- {
		patch := plan.Patches[index]
		updated = append(append(append([]byte(nil), updated[:patch.Range.StartByte]...), patch.Replacement...), updated[patch.Range.EndByte:]...)
	}
	state.setContent(string(updated))
	return nil, nil
}

func providerRootTargetsUnchanged(state *documentState, ops []setFieldOp) error {
	_, baseProjection, _, err := state.projectProviderRoot(state.baseContent)
	if err != nil {
		return err
	}
	_, currentProjection, _, err := state.projectProviderRoot(state.content)
	if err != nil {
		return err
	}
	for _, op := range ops {
		if !strings.EqualFold(op.Field, typeFieldName) {
			baseType, baseTypePresent, typeErr := metadataFactForField(state.schema, baseProjection, setFieldOp{Ref: op.Ref, Field: typeFieldName})
			if typeErr != nil {
				return typeErr
			}
			currentType, currentTypePresent, typeErr := metadataFactForField(state.schema, currentProjection, setFieldOp{Ref: op.Ref, Field: typeFieldName})
			if typeErr != nil {
				return typeErr
			}
			if baseTypePresent != currentTypePresent || (baseTypePresent && string(baseType.Value.CanonicalJSON()) != string(currentType.Value.CanonicalJSON())) {
				return fmt.Errorf("%w: root metadata type changed since the edit session started", errCollectionDrift)
			}
		}
		// Explicit field witnesses are checked against this exact current
		// provider projection by fieldChangedConflict before mutations run. Once
		// the user accepts that witness (Keep mine), comparing the same field to
		// the original base would reject the accepted conflict a second time.
		if op.ExpectedCaptured {
			continue
		}
		base, basePresent, err := metadataFactForField(state.schema, baseProjection, op)
		if err != nil {
			return err
		}
		current, currentPresent, err := metadataFactForField(state.schema, currentProjection, op)
		if err != nil {
			return err
		}
		if basePresent != currentPresent || (basePresent && string(base.Value.CanonicalJSON()) != string(current.Value.CanonicalJSON())) || (basePresent && base.Key != current.Key) {
			return fmt.Errorf("%w: root metadata field %s changed since the edit session started", errCollectionDrift, op.Field)
		}
	}
	return nil
}

func providerRootMetadataChange(schema *Schema, node *NodeProjection, projection noteformat.Projection, op setFieldOp) (noteformat.RootMetadataChange, error) {
	if op.Ref.Kind != NodeKindNote || strings.TrimSpace(op.Ref.Fragment) != "" || strings.TrimSpace(op.Ref.NodeID) != "" {
		return noteformat.RootMetadataChange{}, fmt.Errorf("%w: provider metadata edits require a note root", errUnsupportedTarget)
	}
	field, key, err := providerRootField(schema, node, projection, op)
	if err != nil {
		return noteformat.RootMetadataChange{}, err
	}
	if op.Unset {
		return noteformat.RootMetadataChange{Key: key, Delete: true}, nil
	}
	raw, err := providerMetadataValue(schema, field, op)
	if err != nil {
		return noteformat.RootMetadataChange{}, err
	}
	value, err := noteformat.NewMetadataValue(raw)
	if err != nil {
		return noteformat.RootMetadataChange{}, err
	}
	return noteformat.RootMetadataChange{Key: key, Value: value}, nil
}

func providerRootField(schema *Schema, node *NodeProjection, projection noteformat.Projection, op setFieldOp) (*Field, string, error) {
	if strings.EqualFold(strings.TrimSpace(op.Field), "title") {
		key, keyErr := metadataAuthoredKey(projection, []string{"title"}, "title")
		return &Field{Name: "title", Source: "title", SourceKind: FieldSourceFrontmatter, Kind: FieldKindScalar, TypeName: "String"}, key, keyErr
	}
	if strings.EqualFold(strings.TrimSpace(op.Field), typeFieldName) {
		if len(op.Values) != 1 || schema == nil || schema.Types[strings.TrimSpace(op.Values[0])] == nil {
			return nil, "", fmt.Errorf("%w: type must name a configured note type", errUnsupportedTarget)
		}
		key, keyErr := metadataAuthoredKey(projection, []string{typeFieldName}, typeFieldName)
		return &Field{Name: typeFieldName, Source: typeFieldName, SourceKind: FieldSourceFrontmatter, Kind: FieldKindScalar, TypeName: "String"}, key, keyErr
	}
	if op.RawRootMetadata {
		name := strings.ToLower(strings.TrimSpace(op.Field))
		if name != "tags" && name != "aliases" {
			return nil, "", fmt.Errorf("%w: raw root metadata field %s is not supported", errUnsupportedTarget, op.Field)
		}
		key, keyErr := metadataAuthoredKey(projection, []string{name}, name)
		return &Field{Name: name, Source: name, SourceKind: FieldSourceFrontmatter, Kind: FieldKindScalar, TypeName: "String", List: true}, key, keyErr
	}
	var field *Field
	if node != nil && node.Type != nil {
		field = node.Type.ByName[op.Field]
	}
	if field == nil && schema != nil {
		if requestedType := schema.Types[strings.TrimSpace(op.Ref.TypeName)]; requestedType != nil {
			field = requestedType.ByName[op.Field]
		}
	}
	if field == nil && schema != nil && schema.Interfaces["Note"] != nil {
		field = schema.Interfaces["Note"].ByName[op.Field]
	}
	if field == nil {
		return nil, "", fmt.Errorf("%w: field %s is not declared for the note root", errMissingField, op.Field)
	}
	if field.SourceKind == FieldSourceInline || (field.Kind != FieldKindScalar && field.Kind != FieldKindEnum && field.Kind != FieldKindLink) {
		return nil, "", fmt.Errorf("%w: field %s is not root metadata", errUnsupportedTarget, op.Field)
	}
	if op.RequireScalarList && (field.Kind != FieldKindScalar || !field.List) {
		return nil, "", fmt.Errorf("%w: field %s is not a scalar list", errUnsupportedTarget, op.Field)
	}
	if op.IsLinks && field.Kind != FieldKindLink {
		return nil, "", fmt.Errorf("%w: field %s is not a link field", errUnsupportedTarget, op.Field)
	}
	names := FieldSourceNames(field)
	if len(names) == 0 {
		var noteType *NoteType
		if node != nil {
			noteType = node.Type
		}
		names = []string{DefaultPropertyName(field.Name, propertyCaseOrDefault(noteType))}
	}
	key, err := metadataAuthoredKey(projection, names, names[0])
	if err != nil {
		return nil, "", err
	}
	return field, key, nil
}

func metadataAuthoredKey(projection noteformat.Projection, names []string, fallback string) (string, error) {
	found := ""
	for _, fact := range projection.Facts.RootMetadata {
		for _, name := range names {
			if !strings.EqualFold(strings.TrimSpace(fact.Key), strings.TrimSpace(name)) {
				continue
			}
			if found != "" && found != fact.Key {
				return "", fmt.Errorf("%w: multiple authored keys map to root field %s", errCollectionDrift, fallback)
			}
			found = fact.Key
		}
	}
	if found != "" {
		return found, nil
	}
	return fallback, nil
}

func metadataFactForField(schema *Schema, projection noteformat.Projection, op setFieldOp) (noteformat.RootMetadataFact, bool, error) {
	names := []string{typeFieldName}
	if strings.EqualFold(op.Field, "title") {
		names = []string{"title"}
	} else if op.RawRootMetadata && (strings.EqualFold(op.Field, "tags") || strings.EqualFold(op.Field, "aliases")) {
		names = []string{strings.ToLower(strings.TrimSpace(op.Field))}
	} else if !strings.EqualFold(op.Field, typeFieldName) {
		var field *Field
		typeName := strings.TrimSpace(op.Ref.TypeName)
		if typeName == "" {
			for _, fact := range projection.Facts.RootMetadata {
				if strings.EqualFold(strings.TrimSpace(fact.Key), typeFieldName) {
					if value, ok := fact.Value.Export().(string); ok {
						typeName = strings.TrimSpace(value)
					}
				}
			}
		}
		if noteType := schema.Types[typeName]; noteType != nil {
			field = noteType.ByName[op.Field]
		}
		if field == nil && schema.Interfaces["Note"] != nil {
			field = schema.Interfaces["Note"].ByName[op.Field]
		}
		if field == nil {
			return noteformat.RootMetadataFact{}, false, fmt.Errorf("%w: field %s is not declared", errMissingField, op.Field)
		}
		names = FieldSourceNames(field)
		if len(names) == 0 {
			names = []string{DefaultPropertyName(field.Name, PropertyCaseKebab)}
		}
	}
	var found *noteformat.RootMetadataFact
	for index := range projection.Facts.RootMetadata {
		fact := &projection.Facts.RootMetadata[index]
		for _, name := range names {
			if strings.EqualFold(strings.TrimSpace(fact.Key), strings.TrimSpace(name)) {
				if found != nil {
					return noteformat.RootMetadataFact{}, false, fmt.Errorf("%w: root metadata field %s is ambiguous", errCollectionDrift, op.Field)
				}
				found = fact
			}
		}
	}
	if found == nil {
		return noteformat.RootMetadataFact{}, false, nil
	}
	return *found, true, nil
}

func providerRootFieldValueForReplay(state *documentState, op setFieldOp) ([]string, string, error) {
	if state == nil {
		return nil, "", fmt.Errorf("%w: provider state is required", errMissingNode)
	}
	_, projection, _, err := state.projectProviderRoot(state.content)
	if err != nil {
		return nil, "", err
	}
	fact, present, err := metadataFactForField(state.schema, projection, op)
	if err != nil {
		return nil, "", err
	}
	if !present {
		return nil, "unset", nil
	}
	value := fact.Value.Export()
	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...), "list", nil
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			values = append(values, fmt.Sprint(item))
		}
		return values, "list", nil
	default:
		return []string{fmt.Sprint(typed)}, "scalar", nil
	}
}

func providerMetadataValue(schema *Schema, field *Field, op setFieldOp) (any, error) {
	values := append([]string(nil), op.Values...)
	if field.List {
		out := make([]any, 0, len(values))
		for _, value := range values {
			converted, err := providerScalarMetadataValue(schema, field, value)
			if err != nil {
				return nil, err
			}
			out = append(out, converted)
		}
		return out, nil
	}
	if len(values) != 1 {
		return nil, fmt.Errorf("%w: field %s expects one value", errUnsupportedTarget, field.Name)
	}
	return providerScalarMetadataValue(schema, field, values[0])
}

func providerScalarMetadataValue(schema *Schema, field *Field, raw string) (any, error) {
	raw = strings.TrimSpace(raw)
	if field.Kind == FieldKindLink {
		return raw, nil
	}
	if !validateScalarValue(raw, field, schema) {
		return nil, fmt.Errorf("%w: invalid %s value for field %s", errUnsupportedTarget, field.TypeName, field.Name)
	}
	switch field.TypeName {
	case "Boolean":
		value, _ := strconv.ParseBool(strings.ToLower(raw))
		return value, nil
	case "Int", "Float":
		return json.Number(raw), nil
	default:
		return raw, nil
	}
}
