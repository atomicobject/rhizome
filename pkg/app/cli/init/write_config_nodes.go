package init

import (
	"github.com/atomicobject/rhizome/pkg/fileio"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"gopkg.in/yaml.v3"
)

func loadConfigNode(path string) (*yaml.Node, error) {
	data, err := fileio.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, err
	}
	return &node, nil
}

func ensureConfigNode() *yaml.Node {
	return &yaml.Node{
		Kind: yaml.DocumentNode,
		Content: []*yaml.Node{{
			Kind: yaml.MappingNode,
			Tag:  "!!map",
		}},
	}
}

func rootMapping(doc *yaml.Node) *yaml.Node {
	if doc == nil {
		return nil
	}
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		return doc.Content[0]
	}
	if doc.Kind == yaml.MappingNode {
		return doc
	}
	return nil
}

func getMappingValue(root *yaml.Node, key string) *yaml.Node {
	if root == nil || root.Kind != yaml.MappingNode || key == "" {
		return nil
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		k := root.Content[i]
		if k != nil && k.Value == key {
			return root.Content[i+1]
		}
	}
	return nil
}

func setKnownMappingKey(root *yaml.Node, key string, value *yaml.Node, schema any) {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == key && root.Content[i+1].Kind == yaml.MappingNode && value.Kind == yaml.MappingNode {
			repoconfig.MergeKnownYAMLMapping(root.Content[i+1], value, schema)
			return
		}
	}
	setMappingKey(root, key, value)
}

func deleteKnownMappingKey(root *yaml.Node, key string, schema any) {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != key || root.Content[i+1].Kind != yaml.MappingNode {
			continue
		}
		repoconfig.MergeKnownYAMLMapping(root.Content[i+1], &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}, schema)
		if len(root.Content[i+1].Content) > 0 {
			return
		}
		break
	}
	deleteMappingKey(root, key)
}

func setMappingKey(root *yaml.Node, key string, value *yaml.Node) {
	if root == nil || root.Kind != yaml.MappingNode || key == "" || value == nil {
		return
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		k := root.Content[i]
		if k != nil && k.Value == key {
			root.Content[i+1] = value
			return
		}
	}
	root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
}

func deleteMappingKey(root *yaml.Node, key string) {
	if root == nil || root.Kind != yaml.MappingNode || key == "" {
		return
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		k := root.Content[i]
		if k != nil && k.Value == key {
			root.Content = append(root.Content[:i], root.Content[i+2:]...)
			return
		}
	}
}

func encodeSectionNode(v any) (*yaml.Node, error) {
	data, err := yaml.Marshal(v)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	root := rootMapping(&doc)
	if root == nil {
		root = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	}
	return root, nil
}
