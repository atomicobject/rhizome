package viewconfig

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

func (v *VariantSet) UnmarshalYAML(node *yaml.Node) error {
	if node == nil || node.Kind != yaml.MappingNode {
		return fmt.Errorf("variants must be a mapping")
	}
	*v = VariantSet{}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		value := node.Content[i+1]
		switch key {
		case "table":
			var table *TableVariant
			if err := value.Decode(&table); err != nil {
				return err
			}
			v.Table = table
		case "card":
			card, err := decodeCardSpec(value)
			if err != nil {
				v.cardDecodeError = err.Error()
				continue
			}
			v.Card = card
		case "kanban":
			kanban, err := decodeKanbanVariant(value)
			if err != nil {
				v.kanbanDecodeError = err.Error()
				continue
			}
			v.Kanban = kanban
		}
	}
	return nil
}

func decodeCardSpec(node *yaml.Node) (*CardSpec, error) {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("card variant must be a mapping")
	}
	var card CardSpec
	if err := node.Decode(&card); err != nil {
		return nil, err
	}
	return &card, nil
}

func decodeKanbanVariant(node *yaml.Node) (*KanbanVariant, error) {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("kanban variant must be a mapping")
	}
	var kanban KanbanVariant
	if err := node.Decode(&kanban); err != nil {
		return nil, err
	}
	return &kanban, nil
}
