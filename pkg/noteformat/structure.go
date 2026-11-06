package noteformat

// OwnerKey and ValueID are source-local addresses, never canonical graph identity.
type OwnerKey string
type ValueID uint32

type OwnerShape string

const (
	OwnerDocument     OwnerShape = "DOCUMENT"
	OwnerSection      OwnerShape = "SECTION"
	OwnerListItem     OwnerShape = "LIST_ITEM"
	OwnerCheckboxItem OwnerShape = "CHECKBOX_ITEM"
	OwnerMapping      OwnerShape = "MAPPING"
	OwnerSequence     OwnerShape = "SEQUENCE"
	OwnerScalar       OwnerShape = "SCALAR"
)

type StructureFacts struct {
	Root   OwnerKey
	Owners []OwnerFact
	Values []SourceValueFact
}

type OwnerFact struct {
	Key          OwnerKey
	Parent       OwnerKey
	Collection   string
	Ordinal      int
	Order        int
	Shape        OwnerShape
	Address      string
	Range        OptionalSourceRange
	OwnRanges    []SourceRange
	Value        ValueID
	Properties   []PropertyOccurrenceFact
	Title        *TitleFact
	DisplayTitle *TitleFact
	Locators     []OwnerLocatorFact
	Markdown     *MarkdownOwnerFact
}

type PropertyOccurrenceFact struct {
	Key                         string
	Source                      string
	Value                       ValueID
	Range, KeyRange, ValueRange OptionalSourceRange
}

type OwnerLocatorFact struct {
	Kind  FragmentTargetKind
	Value string
	Range OptionalSourceRange
}

type MarkdownOwnerFact struct {
	SourceID                           string
	HeadingLevel                       int
	Markers                            []TagFact
	Checkbox                           *CheckboxFact
	ItemTitle, ItemSummary, ItemDetail *TitleFact
	MalformedBlockID                   string
	ContentRange                       OptionalSourceRange
	MarkerRange                        OptionalSourceRange
}

type CheckboxFact struct {
	Checked    bool
	TokenRange SourceRange
}

// NewStructureFacts validates source-local structure and detaches its payloads.
// A negative sourceLength defers the upper byte bound to Runtime.Project.
func NewStructureFacts(facts StructureFacts, sourceLength int) (*StructureFacts, error) {
	if err := ValidateStructureFacts(&facts, sourceLength); err != nil {
		return nil, err
	}
	return facts.Copy(), nil
}

func copyTitle(in *TitleFact) *TitleFact {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

// Copy detaches every mutable collection, including syntax-specific evidence.
func (s *StructureFacts) Copy() *StructureFacts {
	if s == nil {
		return nil
	}
	out := *s
	out.Owners = append([]OwnerFact(nil), s.Owners...)
	out.Values = append([]SourceValueFact(nil), s.Values...)
	for i := range out.Values {
		v := &out.Values[i]
		v.Items = append([]ValueID(nil), v.Items...)
		v.Entries = append([]SourceEntryFact(nil), v.Entries...)
		if v.Scalar != nil {
			scalar := v.Scalar.clone()
			v.Scalar = &scalar
		}
	}
	for i := range out.Owners {
		o := &out.Owners[i]
		o.OwnRanges = append([]SourceRange(nil), o.OwnRanges...)
		o.Properties = append([]PropertyOccurrenceFact(nil), o.Properties...)
		o.Locators = append([]OwnerLocatorFact(nil), o.Locators...)
		o.Title = copyTitle(o.Title)
		o.DisplayTitle = copyTitle(o.DisplayTitle)
		if o.Markdown != nil {
			md := *o.Markdown
			md.Markers = append([]TagFact(nil), md.Markers...)
			md.ItemTitle = copyTitle(md.ItemTitle)
			md.ItemSummary = copyTitle(md.ItemSummary)
			md.ItemDetail = copyTitle(md.ItemDetail)
			if md.Checkbox != nil {
				cb := *md.Checkbox
				md.Checkbox = &cb
			}
			o.Markdown = &md
		}
	}
	return &out
}
