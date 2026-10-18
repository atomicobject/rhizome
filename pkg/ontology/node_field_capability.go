package ontology

import ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"

type NodeFieldValueKind string

const (
	NodeFieldValueKindText     NodeFieldValueKind = "text"
	NodeFieldValueKindDate     NodeFieldValueKind = "date"
	NodeFieldValueKindDateTime NodeFieldValueKind = "datetime"
	NodeFieldValueKindBoolean  NodeFieldValueKind = "boolean"
	NodeFieldValueKindInt      NodeFieldValueKind = "int"
	NodeFieldValueKindFloat    NodeFieldValueKind = "float"
	NodeFieldValueKindEnum     NodeFieldValueKind = "enum"
	NodeFieldValueKindRelation NodeFieldValueKind = "relation"
	NodeFieldValueKindSection  NodeFieldValueKind = "section"
)

type NodeFieldValueOrigin string

const (
	NodeFieldValueOriginAuthored NodeFieldValueOrigin = "authored"
	NodeFieldValueOriginDerived  NodeFieldValueOrigin = "derived"
	NodeFieldValueOriginComputed NodeFieldValueOrigin = "computed"
)

type NodeFieldWriteOperation string

const (
	NodeFieldWriteOperationSetField     NodeFieldWriteOperation = "setField"
	NodeFieldWriteOperationSetLinkField NodeFieldWriteOperation = "setLinkField"
)

// NodeFieldCapability is the authoritative editing contract for one field on
// one owning node. Clients must not join field metadata by name across owners.
type NodeFieldCapability struct {
	OwnerRef            NodeRef                           `json:"ownerRef"`
	OwnerType           string                            `json:"ownerType"`
	TypeName            string                            `json:"typeName"`
	ValueKind           NodeFieldValueKind                `json:"valueKind"`
	List                bool                              `json:"list"`
	Required            bool                              `json:"required"`
	EnumValues          []string                          `json:"enumValues"`
	EnumOptions         []NodeFieldEnumOption             `json:"enumOptions"`
	TargetType          string                            `json:"targetType,omitempty"`
	SourceKind          ontoschema.FieldSource            `json:"sourceKind,omitempty"`
	ValueOrigin         NodeFieldValueOrigin              `json:"valueOrigin"`
	Identifier          bool                              `json:"identifier"`
	PreferredIdentifier bool                              `json:"preferredIdentifier"`
	DisplayImportance   ontoschema.FieldDisplayImportance `json:"displayImportance"`
	WriteOperation      NodeFieldWriteOperation           `json:"writeOperation,omitempty"`
	ReadOnlyReason      string                            `json:"readOnlyReason,omitempty"`
}

// NodeFieldEnumOption presents one admitted enum value, in EnumValues order.
// Label is the @view(label:) or, when none is declared, the raw value.
type NodeFieldEnumOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Tone  string `json:"tone,omitempty"`
	// Stage is the value's declared or inferred lifecycle stage, if any.
	Stage ontoschema.LifecycleStage `json:"stage,omitempty"`
}

func buildNodeFieldCapability(schema *ontoschema.Schema, projection *NodeProjection, field *ontoschema.Field, binding FieldBinding) NodeFieldCapability {
	capability := NodeFieldCapability{
		OwnerRef:          projection.Ref,
		OwnerType:         firstNonEmptyNodeValue(projection.ResolvedType, projection.Ref.TypeName),
		ValueKind:         NodeFieldValueKindText,
		EnumValues:        []string{},
		EnumOptions:       []NodeFieldEnumOption{},
		ValueOrigin:       NodeFieldValueOriginAuthored,
		DisplayImportance: ontoschema.FieldImportanceNormal,
	}
	if projection.Type != nil {
		capability.OwnerType = projection.Type.Name
	}
	if binding.Derived {
		capability.ValueOrigin = NodeFieldValueOriginDerived
	}
	if field == nil {
		capability.ReadOnlyReason = "This field is not declared on the owning node type."
		return capability
	}

	capability.TypeName = field.TypeName
	capability.ValueKind = nodeFieldValueKind(field)
	capability.List = field.List
	capability.Required = field.Required
	capability.SourceKind = field.SourceKind
	capability.Identifier = field.IsIdentifier
	capability.PreferredIdentifier = field.IsPreferredIdentifier
	capability.DisplayImportance = field.Display.EffectiveImportance()
	if field.Kind == ontoschema.FieldKindEnum && schema != nil {
		if enumType := schema.EnumTypes[field.TypeName]; enumType != nil {
			tones := enumType.Tones()
			stages := enumType.Stages()
			for index, value := range enumType.Values {
				if value != nil {
					option := NodeFieldEnumOption{
						Value: value.Name,
						Label: firstNonEmptyNodeValue(value.View.Label, value.Name),
						Tone:  tones[index],
					}
					if stages != nil {
						option.Stage = stages[index].Stage
					}
					capability.EnumValues = append(capability.EnumValues, value.Name)
					capability.EnumOptions = append(capability.EnumOptions, option)
				}
			}
		}
	}
	if field.Kind == ontoschema.FieldKindLink {
		capability.TargetType = field.TypeName
	}
	if reason := nodeFieldReadOnlyReason(field, binding); reason != "" {
		capability.ReadOnlyReason = reason
	} else if field.Kind == ontoschema.FieldKindLink {
		capability.WriteOperation = NodeFieldWriteOperationSetLinkField
	} else {
		capability.WriteOperation = NodeFieldWriteOperationSetField
	}
	return capability
}

func nodeFieldValueKind(field *ontoschema.Field) NodeFieldValueKind {
	if field == nil {
		return NodeFieldValueKindText
	}
	switch field.Kind {
	case ontoschema.FieldKindEnum:
		return NodeFieldValueKindEnum
	case ontoschema.FieldKindLink:
		return NodeFieldValueKindRelation
	case ontoschema.FieldKindSection, ontoschema.FieldKindNeighbor, ontoschema.FieldKindReverse:
		return NodeFieldValueKindSection
	case ontoschema.FieldKindScalar:
		switch ontoschema.ScalarKindForType(field.TypeName) {
		case ontoschema.ScalarKindDate:
			return NodeFieldValueKindDate
		case ontoschema.ScalarKindDateTime:
			return NodeFieldValueKindDateTime
		case ontoschema.ScalarKindBoolean:
			return NodeFieldValueKindBoolean
		case ontoschema.ScalarKindInt:
			return NodeFieldValueKindInt
		case ontoschema.ScalarKindReal:
			return NodeFieldValueKindFloat
		}
	}
	return NodeFieldValueKindText
}

func nodeFieldReadOnlyReason(field *ontoschema.Field, binding FieldBinding) string {
	if field.IsIdentifier {
		return "Identifiers are graph keys and must stay unique. Change them through identifier reconciliation."
	}
	if field.Kind == ontoschema.FieldKindNeighbor || field.Kind == ontoschema.FieldKindReverse {
		return "This relation is computed from neighboring nodes."
	}
	if field.Kind == ontoschema.FieldKindSection {
		return "This field is edited through its section collection."
	}
	if binding.Derived {
		return "This value is derived from the node structure."
	}
	switch field.SourceKind {
	case ontoschema.FieldSourceFrontmatter, ontoschema.FieldSourceInline, ontoschema.FieldSourceCheckbox:
		return ""
	case ontoschema.FieldSourceItemTitle, ontoschema.FieldSourceItemSummary, ontoschema.FieldSourceItemDetail:
		return "This value is edited through the item's content editor."
	default:
		return "This field has no supported authored source."
	}
}
