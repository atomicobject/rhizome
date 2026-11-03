package repoconfig

import (
	"bytes"
	"errors"
	"io"
	"os"
	"sort"

	"github.com/atomicobject/rhizome/pkg/fileio"
	"github.com/atomicobject/rhizome/pkg/vault/config"
	"gopkg.in/yaml.v3"
)

// PatchCliConfigValues edits only named values in a provider-settings section.
// Nil deletes a value. Keeping the original YAML tree preserves unrelated and
// future fields, including fields nested inside otherwise known configuration.
// The caller holds the native app's global configuration lock.
func PatchCliConfigValues(env config.Env, section string, changes map[string]*string) error {
	if section != "env" && section != "provider_access" {
		return errors.New("unsupported provider settings section")
	}
	dir, path, err := env.CliPath()
	if err != nil {
		return errors.New(RhizomeConfigReadError)
	}
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
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return errors.New(RhizomeConfigDirWriteError)
	}
	if err := fileio.WriteFileAtomic(path, data, 0o600); err != nil {
		return errors.New(RhizomeConfigWriteError)
	}
	return nil
}
