package schema

// Violations reports a missing required pattern and a matched forbidden pattern.
func (c *TitleConstraint) Violations(value string) (mismatch, forbidden bool) {
	return c.patternRE != nil && !c.patternRE.MatchString(value),
		c.notPatternRE != nil && c.notPatternRE.MatchString(value)
}

// Violations reports a missing required pattern and a matched forbidden pattern.
func (c *FieldFormatConstraint) Violations(value string) (mismatch, forbidden bool) {
	return c.patternRE != nil && !c.patternRE.MatchString(value),
		c.notPatternRE != nil && c.notPatternRE.MatchString(value)
}

// AuthoredOrder returns the explicit presentation order, including an authored zero.
func (view EnumValueView) AuthoredOrder() *int {
	if !view.orderSet {
		return nil
	}
	order := view.Order
	return &order
}
