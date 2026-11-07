package schemadoc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdmissionGuideDescribesAdditionalCondition(t *testing.T) {
	schema := testCustomSchema(t, `type Assertion @node(paths:["assertions/**/*.md"]){title:String}
extend type Assertion @admit(matches:["products:alpha OR NOT has:products"])`)
	guide, err := RenderGuideMarkdown(schema, GuideOptions{Types: []string{"Assertion"}})
	require.NoError(t, err)
	require.Contains(t, guide, "admission: must additionally match at least one @admit expression: `products:alpha OR NOT has:products`")
	require.Contains(t, guide, "assertions/**/*.md")
}
