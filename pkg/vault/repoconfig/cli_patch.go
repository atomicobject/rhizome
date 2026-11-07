package repoconfig

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/fileio"
	"github.com/atomicobject/rhizome/pkg/sqliteutil"
	"github.com/atomicobject/rhizome/pkg/vault/config"
	"gopkg.in/yaml.v3"
)

// rewriteCliConfig is the only writer of the user configuration file. It
// holds an exclusive file lock across processes, rereads the current file
// under it, lets edit change the YAML tree, and replaces the file atomically.
// Editing the tree rather than re-marshalling a struct keeps keys this binary
// does not model, comments and order. edit receives the root mapping and the
// bytes it was parsed from.
func rewriteCliConfig(env config.Env, edit func(root *yaml.Node, data []byte) error) error {
	dir, path, err := env.CliPath()
	if err != nil {
		return err
	}
	if dir == "" {
		return errors.New(RhizomeConfigDirWriteError)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return errors.New(RhizomeConfigDirWriteError)
	}
	// The schema-init lock is a generic exclusive sidecar lock (flock or
	// LockFileEx); it serializes every Rhizome process writing this file.
	release, err := sqliteutil.LockSchemaInit(context.Background(), path)
	if err != nil {
		return errors.New(RhizomeConfigWriteError)
	}
	defer release()

	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.New(RhizomeConfigReadError)
	}
	var document yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&document); err != nil && !errors.Is(err, io.EOF) {
		return errors.New(RhizomeConfigParseError)
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New(RhizomeConfigParseError)
	}
	if len(document.Content) == 0 {
		document = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	root := document.Content[0]
	if root.Kind != yaml.MappingNode {
		return errors.New(RhizomeConfigParseError)
	}
	if err := edit(root, data); err != nil {
		return err
	}
	data, err = yaml.Marshal(&document)
	if err != nil {
		return errors.New(RhizomeConfigGenerateError)
	}
	// Replacing or deleting an anchored value can strand aliases elsewhere.
	// Reject an unreadable result before touching the original configuration.
	var generated yaml.Node
	if err := yaml.Unmarshal(data, &generated); err != nil {
		return errors.New(RhizomeConfigGenerateError)
	}
	if err := fileio.WriteFileAtomic(path, data, 0o600); err != nil {
		return errors.New(RhizomeConfigWriteError)
	}
	return nil
}

// UpdateCliConfig applies mutate to the current user configuration and saves
// it. The read, mutate and write happen under the configuration file lock, so
// concurrent updates from other processes are not lost. Only top-level fields
// that mutate changed are rewritten; every other key, including keys this
// binary does not model, keeps its YAML text and comments. An error from
// mutate aborts without writing.
func UpdateCliConfig(env config.Env, mutate func(*CliConfig) error) error {
	return rewriteCliConfig(env, func(root *yaml.Node, data []byte) error {
		// Decode twice: mutate may edit the maps of its copy in place.
		var before, after CliConfig
		if err := yaml.Unmarshal(data, &before); err != nil {
			return errors.New(RhizomeConfigParseError)
		}
		_ = yaml.Unmarshal(data, &after)
		if err := mutate(&after); err != nil {
			return err
		}
		desired, err := encodeYAMLDocument(after)
		if err != nil {
			return errors.New(RhizomeConfigGenerateError)
		}
		desiredRoot := yamlDocumentRoot(desired)
		// ponytail: a changed field is replaced whole, so comments inside a
		// changed section (for example env) are lost; patch per map key if
		// that matters.
		schema := reflect.TypeOf(CliConfig{})
		beforeValue, afterValue := reflect.ValueOf(before), reflect.ValueOf(after)
		for i := 0; i < schema.NumField(); i++ {
			if !reflect.DeepEqual(beforeValue.Field(i).Interface(), afterValue.Field(i).Interface()) {
				continue
			}
			key := strings.Split(schema.Field(i).Tag.Get("yaml"), ",")[0]
			if index := yamlMappingKeyIndex(root, key); index >= 0 {
				yamlSetMappingValue(desiredRoot, key, root.Content[index+1])
			} else {
				yamlDeleteMappingKey(desiredRoot, key)
			}
		}
		mergeKnownYAMLMapping(root, desiredRoot, schema)
		return nil
	})
}

// PatchCliConfigValues edits only named values in a provider-settings section.
// Nil deletes a value. Keeping the original YAML tree preserves unrelated and
// future fields, including fields nested inside otherwise known configuration.
func PatchCliConfigValues(env config.Env, section string, changes map[string]*string) error {
	if section != "env" && section != "provider_access" {
		return errors.New("unsupported provider settings section")
	}
	return rewriteCliConfig(env, func(root *yaml.Node, _ []byte) error {
		var values *yaml.Node
		for i := 0; i < len(root.Content); i += 2 {
			if root.Content[i].Value == section {
				values = root.Content[i+1]
				break
			}
		}
		if values == nil {
			values = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: section}, values)
		} else if values.Tag == "!!null" {
			*values = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		}
		if values.Kind != yaml.MappingNode {
			return errors.New(RhizomeConfigParseError)
		}
		// Editing inherited or shared mappings can change unrelated effective
		// settings. Leave these configurations intact for an explicit manual edit.
		for _, mapping := range []*yaml.Node{root, values} {
			if mapping.Anchor != "" {
				return errors.New("Settings cannot edit configuration sections with YAML anchors or merges. Edit the configuration file directly.")
			}
			for i := 0; i < len(mapping.Content); i += 2 {
				if mapping.Content[i].Tag == "!!merge" {
					return errors.New("Settings cannot edit configuration sections with YAML anchors or merges. Edit the configuration file directly.")
				}
			}
		}
		keys := make([]string, 0, len(changes))
		for key := range changes {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			value := changes[key]
			index := -1
			for i := 0; i < len(values.Content); i += 2 {
				if values.Content[i].Value == key {
					index = i
					break
				}
			}
			if value == nil {
				if index >= 0 {
					values.Content = append(values.Content[:index], values.Content[index+2:]...)
				}
			} else {
				node := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: *value}
				if index >= 0 {
					values.Content[index+1] = node
				} else {
					values.Content = append(values.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, node)
				}
			}
		}
		return nil
	})
}
