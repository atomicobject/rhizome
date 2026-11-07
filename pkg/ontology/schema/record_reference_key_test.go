package schema

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecordReferenceTargetKeyContract(t *testing.T) {
	for _, tc := range []struct {
		name, targets, key string
		valid              bool
	}{
		{"scalar", `type Evidence @node(paths:["*.md"]){aoId:String @field(source:"id")}`, "aoId", true},
		{"required scalar", `type Evidence @node(paths:["*.md"]){aoId:String!}`, "aoId", true},
		{"missing field", `type Evidence @node(paths:["*.md"]){name:String}`, "aoId", false},
		{"list", `type Evidence @node(paths:["*.md"]){aoId:[String!]}`, "aoId", false},
		{"ID", `type Evidence @node(paths:["*.md"]){aoId:ID}`, "aoId", false},
		{"integer", `type Evidence @node(paths:["*.md"]){aoId:Int}`, "aoId", false},
		{"empty", `type Evidence @node(paths:["*.md"]){aoId:String}`, "", false},
		{"interface", `interface Evidence {aoId:String} type Assertion implements Evidence @node(paths:["*.md"]){aoId:String @field(source:"id")}`, "aoId", true},
		{"interface implementor missing key", `interface Evidence {name:String} type Assertion implements Evidence @node(paths:["*.md"]){name:String aoId:String} type Other implements Evidence @node(paths:["other/*.md"]){name:String}`, "aoId", false},
		{"record unchanged", `type Evidence @node(locator:EMBEDDED) ` + recordTestSource + ` {id:ID @field(path:"id")}`, "aoId", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sdl := tc.targets + ` type Row @node(locator:EMBEDDED) ` + recordTestSource + ` {refs:[String!] @field(path:"refs") @references(to:["Evidence"],targetKey:"` + tc.key + `",mode:STRICT)}`
			compiled, err := recordSchema(t, sdl)
			if tc.valid {
				require.NoError(t, err)
				require.Equal(t, tc.key, compiled.Types["Row"].ByName["refs"].Record.References.TargetKey)
			} else {
				var rule *CompileError
				require.ErrorAs(t, err, &rule)
				require.Equal(t, "record_reference_target_key", rule.Code)
				require.Equal(t, "Row", rule.TypeName)
				require.Equal(t, "refs", rule.FieldName)
			}
		})
	}
}
