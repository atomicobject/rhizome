package schemadoc

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRecordGuideDescribesNativeBindings(t *testing.T) {
	schema := testCustomSchema(t, `type Row @node(locator: EMBEDDED) @source(shape:RECORD,paths:["*.native"],select:"rows",key:{field:"id"},title:["name","$key"],space:"test",logicalKind:"row") { name:String @field(path:"details.name") refs:[Row!] @field(path:"refs") @references(to:["Row"],mode:STRICT,values:BY_SHAPE,edge:{name:"uses"}) }`)
	guide, err := RenderGuideMarkdown(schema, GuideOptions{Types: []string{"Row"}})
	require.NoError(t, err)
	for _, expected := range []string{"Native RECORD", `@field(path: "details.name")`, "BY_SHAPE", "Keyed locators are durable", "Positional locators require"} {
		require.Contains(t, guide, expected)
	}
	for _, forbidden := range []string{"^block-id", "@value", "type: Row", "matching markdown headings", "```markdown"} {
		require.NotContains(t, guide, forbidden)
	}
}
