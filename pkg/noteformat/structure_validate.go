package noteformat

import (
	"fmt"
	"slices"
)

func structuralRange(r OptionalSourceRange, limit int) error {
	if !r.Present {
		if r.Range != (SourceRange{}) {
			return fmt.Errorf("absent range has nonzero bytes")
		}
		return nil
	}
	if r.Range.StartByte < 0 || r.Range.EndByte < r.Range.StartByte || (limit >= 0 && r.Range.EndByte > limit) {
		return fmt.Errorf("invalid structural range [%d,%d)", r.Range.StartByte, r.Range.EndByte)
	}
	return nil
}
func structuralContains(parent, child OptionalSourceRange) bool {
	return !parent.Present || !child.Present || (child.Range.StartByte >= parent.Range.StartByte && child.Range.EndByte <= parent.Range.EndByte)
}

// ValidateStructureFacts rejects malformed provider envelopes before publication.
// Adjacency and color walks bound validation to owners, values and occurrences.
func ValidateStructureFacts(s *StructureFacts, sourceLength int) error {
	if s == nil {
		return nil
	}
	owners := make(map[OwnerKey]int, len(s.Owners))
	orders := make(map[int]bool, len(s.Owners))
	members := make(map[OwnerKey]map[string]map[int]bool)
	children := make(map[OwnerKey][]int)
	valueOwners := map[ValueID]bool{}
	addresses := map[string]bool{}
	for i, o := range s.Owners {
		if o.Key == "" {
			return fmt.Errorf("empty owner key")
		}
		if _, exists := owners[o.Key]; exists {
			return fmt.Errorf("duplicate owner key %q", o.Key)
		}
		owners[o.Key] = i
		if o.Address != "" {
			if addresses[o.Address] {
				return fmt.Errorf("duplicate owner address %q", o.Address)
			}
			addresses[o.Address] = true
		}
		switch o.Shape {
		case OwnerDocument, OwnerSection, OwnerListItem, OwnerCheckboxItem, OwnerMapping, OwnerSequence, OwnerScalar:
		default:
			return fmt.Errorf("unknown owner shape %q", o.Shape)
		}
		if o.Ordinal < 0 || o.Order < 0 || orders[o.Order] {
			return fmt.Errorf("invalid or duplicate owner order/ordinal for %q", o.Key)
		}
		orders[o.Order] = true
		if i > 0 && o.Order <= s.Owners[i-1].Order {
			return fmt.Errorf("owners are not in source order")
		}
		if err := structuralRange(o.Range, sourceLength); err != nil {
			return fmt.Errorf("owner %q: %w", o.Key, err)
		}
		previous := -1
		for _, r := range o.OwnRanges {
			optional := OptionalSourceRange{Present: true, Range: r}
			if err := structuralRange(optional, sourceLength); err != nil {
				return err
			}
			if !structuralContains(o.Range, optional) || r.StartByte < previous {
				return fmt.Errorf("invalid own ranges for %q", o.Key)
			}
			previous = r.EndByte
		}
		if o.Value != 0 {
			if int(o.Value) > len(s.Values) || valueOwners[o.Value] {
				return fmt.Errorf("dangling or duplicate owner value for %q", o.Key)
			}
			valueOwners[o.Value] = true
		}
		for _, title := range []*TitleFact{o.Title, o.DisplayTitle} {
			if title != nil {
				if err := structuralRange(title.Range, sourceLength); err != nil {
					return err
				}
				if !structuralContains(o.Range, title.Range) {
					return fmt.Errorf("title outside owner")
				}
			}
		}
		for _, p := range o.Properties {
			if p.Key == "" || p.Value == 0 || int(p.Value) > len(s.Values) {
				return fmt.Errorf("invalid property for %q", o.Key)
			}
			switch p.Source {
			case "frontmatter", "inline", "property":
			default:
				return fmt.Errorf("invalid property source %q", p.Source)
			}
			for _, r := range []OptionalSourceRange{p.Range, p.KeyRange, p.ValueRange} {
				if err := structuralRange(r, sourceLength); err != nil {
					return err
				}
				if !structuralContains(o.Range, r) {
					return fmt.Errorf("property outside owner")
				}
			}
			if !structuralContains(p.Range, p.KeyRange) || !structuralContains(p.Range, p.ValueRange) {
				return fmt.Errorf("property component outside occurrence")
			}
		}
		for _, l := range o.Locators {
			switch l.Kind {
			case FragmentTargetHeading, FragmentTargetBlock, FragmentTargetElementID, FragmentTargetLegacyName:
			default:
				return fmt.Errorf("invalid owner locator kind %q", l.Kind)
			}
			if l.Value == "" {
				return fmt.Errorf("empty owner locator")
			}
			if err := structuralRange(l.Range, sourceLength); err != nil {
				return err
			}
			if !structuralContains(o.Range, l.Range) {
				return fmt.Errorf("locator outside owner")
			}
		}
		if err := validateMarkdownOwner(o, sourceLength); err != nil {
			return err
		}
		if o.Parent != "" {
			if members[o.Parent] == nil {
				members[o.Parent] = map[string]map[int]bool{}
			}
			if members[o.Parent][o.Collection] == nil {
				members[o.Parent][o.Collection] = map[int]bool{}
			}
			if members[o.Parent][o.Collection][o.Ordinal] {
				return fmt.Errorf("duplicate membership ordinal")
			}
			members[o.Parent][o.Collection][o.Ordinal] = true
			children[o.Parent] = append(children[o.Parent], i)
		}
	}
	root, ok := owners[s.Root]
	if !ok || s.Root == "" || s.Owners[root].Parent != "" || s.Owners[root].Shape != OwnerDocument {
		return fmt.Errorf("missing or invalid structural root")
	}
	for _, o := range s.Owners {
		if o.Key != s.Root {
			parent, ok := owners[o.Parent]
			if !ok {
				return fmt.Errorf("owner %q has missing parent", o.Key)
			}
			if !structuralContains(s.Owners[parent].Range, o.Range) {
				return fmt.Errorf("child outside parent")
			}
		}
	}
	seen := map[OwnerKey]bool{}
	stack := []OwnerKey{s.Root}
	for len(stack) > 0 {
		k := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[k] {
			return fmt.Errorf("owner cycle")
		}
		seen[k] = true
		siblings := children[k]
		last := -1
		ownIndex := 0
		own := s.Owners[owners[k]].OwnRanges
		lastOrdinal := map[string]int{}
		for _, i := range siblings {
			o := s.Owners[i]
			if previous, ok := lastOrdinal[o.Collection]; ok && o.Ordinal <= previous {
				return fmt.Errorf("membership order disagrees with source order")
			}
			lastOrdinal[o.Collection] = o.Ordinal
			stack = append(stack, o.Key)
			if o.Range.Present {
				if o.Range.Range.StartByte < last {
					return fmt.Errorf("overlapping or unordered siblings")
				}
				last = o.Range.Range.EndByte
			}
			if o.Range.Present {
				for ownIndex < len(own) && own[ownIndex].EndByte <= o.Range.Range.StartByte {
					ownIndex++
				}
				if ownIndex < len(own) && own[ownIndex].StartByte < o.Range.Range.EndByte {
					return fmt.Errorf("own range overlaps child")
				}
			}
		}
	}
	if len(seen) != len(owners) {
		return fmt.Errorf("unreachable or cyclic owners")
	}
	return validateSourceValues(s.Values, sourceLength)
}

func validateMarkdownOwner(o OwnerFact, limit int) error {
	if o.Markdown == nil {
		return nil
	}
	m := o.Markdown
	if m.HeadingLevel < 0 || m.HeadingLevel > 6 {
		return fmt.Errorf("invalid heading level")
	}
	ranges := []OptionalSourceRange{m.ContentRange, m.MarkerRange}
	for _, t := range []*TitleFact{m.ItemTitle, m.ItemSummary, m.ItemDetail} {
		if t != nil {
			ranges = append(ranges, t.Range)
		}
	}
	for _, t := range m.Markers {
		ranges = append(ranges, t.Range)
	}
	if m.Checkbox != nil {
		ranges = append(ranges, OptionalSourceRange{Present: true, Range: m.Checkbox.TokenRange})
	}
	for _, r := range ranges {
		if err := structuralRange(r, limit); err != nil {
			return err
		}
		if !structuralContains(o.Range, r) {
			return fmt.Errorf("Markdown evidence outside owner")
		}
	}
	return nil
}

func validateSourceValues(values []SourceValueFact, limit int) error {
	parents := make([]int, len(values))
	colors := make([]byte, len(values))
	var visit func(ValueID) error
	visit = func(id ValueID) error {
		if id == 0 || int(id) > len(values) {
			return fmt.Errorf("dangling value reference %d", id)
		}
		i := int(id) - 1
		if colors[i] == 1 {
			return fmt.Errorf("value cycle")
		}
		if colors[i] == 2 {
			return nil
		}
		colors[i] = 1
		v := values[i]
		if err := structuralRange(v.Range, limit); err != nil {
			return err
		}
		children := v.Items
		switch v.Kind {
		case MetadataArray:
			if v.Scalar != nil || len(v.Entries) > 0 {
				return fmt.Errorf("array has incompatible payload")
			}
		case MetadataObject:
			if v.Scalar != nil || len(v.Items) > 0 {
				return fmt.Errorf("object has incompatible payload")
			}
			keys := map[string]bool{}
			for _, e := range v.Entries {
				if !slices.Contains([]MetadataValueKind{MetadataString, MetadataInteger}, e.Key.Kind()) || !e.Key.valid() {
					return fmt.Errorf("invalid mapping key")
				}
				k := string(e.Key.CanonicalJSON())
				if keys[k] {
					return fmt.Errorf("duplicate mapping key")
				}
				keys[k] = true
				if err := structuralRange(e.KeyRange, limit); err != nil {
					return err
				}
				if !structuralContains(v.Range, e.KeyRange) {
					return fmt.Errorf("mapping key outside value")
				}
				children = append(children, e.Value)
			}
		default:
			if v.Scalar == nil || v.Scalar.Kind() != v.Kind || !v.Scalar.valid() || len(v.Items) > 0 || len(v.Entries) > 0 {
				return fmt.Errorf("invalid scalar payload")
			}
		}
		for _, child := range children {
			if child == 0 || int(child) > len(values) {
				return fmt.Errorf("dangling value reference")
			}
			parents[child-1]++
			if parents[child-1] > 1 {
				return fmt.Errorf("value has multiple container parents")
			}
			if !structuralContains(v.Range, values[child-1].Range) {
				return fmt.Errorf("child value outside container")
			}
			if err := visit(child); err != nil {
				return err
			}
		}
		colors[i] = 2
		return nil
	}
	for i := range values {
		if err := visit(ValueID(i + 1)); err != nil {
			return err
		}
	}
	return nil
}
