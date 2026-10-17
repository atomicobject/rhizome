package repoconfig

import (
	"bytes"

	"errors"
	"fmt"
	"io"
	"os"

	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/fileio"

	"gopkg.in/yaml.v3"
)

var unknownYAMLFieldPattern = regexp.MustCompile(`field ([^ ]+) not found in type`)

func decodeLocalConfig(configPath string, data []byte) (*LocalConfig, error) {
	cfg, warnings, err := decodeCanonicalYAML[LocalConfig](configPath, data)
	if err != nil {
		return nil, err
	}
	cfg.Rhizome.BinaryManager = strings.TrimSpace(cfg.Rhizome.BinaryManager)
	if _, err := cfg.Rhizome.UsesExternalBinaryManager(); err != nil {
		return nil, fmt.Errorf("%s: %w", configPath, err)
	}
	cfg.Warnings = warnings
	cfg.sourceYAML, err = parseYAMLDocument(data)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

func decodeCanonicalYAML[T any](configPath string, data []byte) (T, []ConfigWarning, error) {
	var cfg T
	if strings.TrimSpace(string(data)) == "" {
		return cfg, nil, nil
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return cfg, nil, nil
		}
		keys := unknownYAMLFields(err)
		if len(keys) == 0 {
			return cfg, nil, err
		}
		warning := ConfigWarning{
			Path:          configPath,
			OffendingKeys: keys,
			DocsHint:      LocalConfigDocsHint,
		}
		decoder = yaml.NewDecoder(bytes.NewReader(data))
		if err := decoder.Decode(&cfg); err != nil {
			return cfg, nil, err
		}
		if err := ensureSingleYAMLDocument(decoder); err != nil {
			return cfg, nil, err
		}
		return cfg, []ConfigWarning{warning}, nil
	}
	if err := ensureSingleYAMLDocument(decoder); err != nil {
		return cfg, nil, err
	}
	return cfg, nil, nil
}

func ensureSingleYAMLDocument(decoder *yaml.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("repo-local config must contain exactly one YAML document")
		}
		return err
	}
	return nil
}

func parseYAMLDocument(data []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if strings.TrimSpace(string(data)) != "" {
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return nil, err
		}
	}
	if len(doc.Content) == 0 {
		doc.Kind = yaml.DocumentNode
		doc.Content = []*yaml.Node{{
			Kind: yaml.MappingNode,
			Tag:  "!!map",
		}}
		return &doc, nil
	}
	if doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("repo-local config must contain a YAML mapping")
	}
	return &doc, nil
}

func saveYAMLDocumentPreservingUnknown(path string, value any, source *yaml.Node, schema reflect.Type) error {
	desired, err := encodeYAMLDocument(value)
	if err != nil {
		return err
	}
	if source != nil {
		sourceRoot := yamlDocumentRoot(source)
		desiredRoot := yamlDocumentRoot(desired)
		if sourceRoot == nil || desiredRoot == nil {
			return errors.New("repo-local config must contain a YAML mapping")
		}
		mergeKnownYAMLMapping(sourceRoot, desiredRoot, schema)
		desired = source
	}
	root := yamlDocumentRoot(desired)
	if root != nil && len(root.Content) == 0 && strings.HasSuffix(path, localWorkflowStateFile) {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(LocalConfigYAMLIndent)
	if err := enc.Encode(desired); err != nil {
		_ = enc.Close()
		return err
	}
	_ = enc.Close()
	// Identical replacement would spuriously notify configuration watchers.
	if existing, err := fileio.ReadFile(path); err == nil && bytes.Equal(existing, buf.Bytes()) {
		return nil
	}
	return fileio.WriteFileAtomicPreservingMode(path, buf.Bytes(), 0o644)
}

func encodeYAMLDocument(value any) (*yaml.Node, error) {
	data, err := yaml.Marshal(value)
	if err != nil {
		return nil, err
	}
	return parseYAMLDocument(data)
}

func yamlDocumentRoot(doc *yaml.Node) *yaml.Node {
	if doc == nil || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil
	}
	return doc.Content[0]
}

func mergeKnownYAMLMapping(existing, desired *yaml.Node, schema reflect.Type) {
	fields := yamlKnownFieldTypes(schema)
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fieldType := fields[key]
		existingIndex := yamlMappingKeyIndex(existing, key)
		desiredIndex := yamlMappingKeyIndex(desired, key)
		if desiredIndex >= 0 {
			desiredValue := desired.Content[desiredIndex+1]
			if existingIndex >= 0 && yamlStructType(fieldType) && existing.Content[existingIndex+1].Kind == yaml.MappingNode && desiredValue.Kind == yaml.MappingNode {
				mergeKnownYAMLMapping(existing.Content[existingIndex+1], desiredValue, fieldType)
				continue
			}
			yamlSetMappingValue(existing, key, desiredValue)
			continue
		}
		if existingIndex < 0 {
			continue
		}
		existingValue := existing.Content[existingIndex+1]
		if yamlStructType(fieldType) && existingValue.Kind == yaml.MappingNode {
			empty := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			mergeKnownYAMLMapping(existingValue, empty, fieldType)
			if len(existingValue.Content) > 0 {
				continue
			}
		}
		yamlDeleteMappingKey(existing, key)
	}
}

// MergeKnownYAMLMapping updates fields owned by schema while retaining
// unrecognized fields in the existing mapping, including nested fields.
func MergeKnownYAMLMapping(existing, desired *yaml.Node, schema any) {
	if existing == nil || desired == nil {
		return
	}
	mergeKnownYAMLMapping(existing, desired, reflect.TypeOf(schema))
}

func yamlKnownFieldTypes(schema reflect.Type) map[string]reflect.Type {
	for schema.Kind() == reflect.Pointer {
		schema = schema.Elem()
	}
	fields := make(map[string]reflect.Type)
	if schema.Kind() != reflect.Struct {
		return fields
	}
	for i := 0; i < schema.NumField(); i++ {
		field := schema.Field(i)
		if field.PkgPath != "" {
			continue
		}
		tag := field.Tag.Get("yaml")
		parts := strings.Split(tag, ",")
		if parts[0] == "-" {
			continue
		}
		if slices.Contains(parts[1:], "inline") {
			for key, fieldType := range yamlKnownFieldTypes(field.Type) {
				fields[key] = fieldType
			}
			continue
		}
		name := parts[0]
		if name == "" {
			name = strings.ToLower(field.Name)
		}
		fields[name] = field.Type
	}
	return fields
}

func yamlStructType(valueType reflect.Type) bool {
	for valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	return valueType.Kind() == reflect.Struct
}

func yamlMappingKeyIndex(mapping *yaml.Node, key string) int {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return i
		}
	}
	return -1
}

func yamlSetMappingValue(mapping *yaml.Node, key string, value *yaml.Node) {
	if index := yamlMappingKeyIndex(mapping, key); index >= 0 {
		mapping.Content[index+1] = value
		return
	}
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
}

func yamlDeleteMappingKey(mapping *yaml.Node, key string) {
	if index := yamlMappingKeyIndex(mapping, key); index >= 0 {
		mapping.Content = append(mapping.Content[:index], mapping.Content[index+2:]...)
	}
}

func unknownYAMLFields(err error) []string {
	matches := unknownYAMLFieldPattern.FindAllStringSubmatch(err.Error(), -1)
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		if len(match) > 1 {
			seen[match[1]] = struct{}{}
		}
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func hasAnyTopLevelYAMLKey(data []byte, keys ...string) bool {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil || len(doc.Content) == 0 {
		return false
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return false
	}
	wanted := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		wanted[key] = struct{}{}
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if _, ok := wanted[root.Content[i].Value]; ok {
			return true
		}
	}
	return false
}
