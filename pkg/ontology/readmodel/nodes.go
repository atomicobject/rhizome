package readmodel

type NodeRow struct {
	NodeID                string
	NotePath              string
	NodeRefJSON           string
	NodeKind              string
	TypeName              string
	ParentNodeID          string
	ParentTypeName        string
	Title                 string
	SourceLocator         string
	Fragment              string
	BlockID               string
	DisplayLabel          string
	LocatorStatus         string
	StartByte             int64
	EndByte               int64
	StructuralFingerprint string
	SchemaHash            string
	UpdatedAt             int64
}

// FieldValueRow is a schema-aware, node-scoped ontology field value for
// query/read-model execution. It intentionally sidecars NodeRow instead of
// note_property_values: note properties stay note-scoped metadata, while these
// rows are keyed by ontology node_id.
//
// NOTE: `FieldName` is stored lowercased at write time; the schema's
// `FieldDoc.Name` is camelCase. Callers matching field rows back to a schema
// field MUST compare case-insensitively (e.g. `strings.EqualFold`) — passing
// the camelCase name to filters works because the SQL filter is also
// case-insensitive, but the returned rows always carry the lowercased form.
type FieldValueRow struct {
	NodeID              string
	NotePath            string
	TypeName            string
	FieldName           string
	FieldKind           string
	SourceKind          string
	ValueKind           string
	ValueText           string
	ValueNorm           string
	ValueBool           *bool
	ValueInt            *int64
	ValueReal           *float64
	ValueDate           *string
	ValueDateTime       *string
	TargetNodeID        string
	TargetRefJSON       string
	TargetNotePath      string
	TargetTypeName      string
	TargetSourceLocator string
	ListOrdinal         int
	SchemaHash          string
	UpdatedAt           int64
}

// LinkDependencyRow records the target-resolution dependency for an indexed
// link field. It lets incremental ontology sync refresh only source notes whose
// canonical link field rows may change when aliases, note paths, or target
// types change.
type LinkDependencyRow struct {
	SourceNotePath         string
	NodeID                 string
	TypeName               string
	FieldName              string
	TargetInput            string
	TargetInputNorm        string
	ResolvedTargetNotePath string
	ResolvedTargetTypeName string
	SchemaHash             string
	UpdatedAt              int64
}

// NodeReadModel is the atomic replacement unit for ontology node identity,
// queryable field values, and field-resolution dependencies.
type NodeReadModel struct {
	FullReplace      bool
	NotePaths        []string
	Nodes            []NodeRow
	FieldValues      []FieldValueRow
	LinkDependencies []LinkDependencyRow
}

type FieldOperator string

const (
	FieldOpEq     FieldOperator = "eq"
	FieldOpIn     FieldOperator = "in"
	FieldOpExists FieldOperator = "exists"
	FieldOpGT     FieldOperator = "gt"
	FieldOpGTE    FieldOperator = "gte"
	FieldOpLT     FieldOperator = "lt"
	FieldOpLTE    FieldOperator = "lte"
)

type FieldPredicate struct {
	FieldName string
	Op        FieldOperator
	Values    []FieldValueRow
}

type FieldSort struct {
	FieldName string
	ValueKind string
	Desc      bool
	NullsLast bool
}

type NodeQueryPlan struct {
	// TypeNames scopes the query to one or more concrete type rows. Single-type
	// callers pass []string{name}; interface roots may pass multiple
	// implementor names so the SQL emits a single n.type_name IN (...) WHERE
	// clause and the offset/limit applies globally across implementors.
	TypeNames  []string
	Predicates []FieldPredicate
	Sort       []FieldSort
	Limit      int
	Offset     int
}
