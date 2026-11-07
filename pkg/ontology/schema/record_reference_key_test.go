package schema

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecordReferenceTargetKeyContract(t *testing.T) {
	for _, tc := range []struct {
		name, targets, key string
		valid              bool
		want               []string
	}{
		{"scalar", `type Evidence @node(paths:["*.md"]){aoId:String @field(source:"id")}`, "aoId", true, nil},
		{"required scalar", `type Evidence @node(paths:["*.md"]){aoId:String!}`, "aoId", true, nil},
		{"missing field", `type Evidence @node(paths:["*.md"]){name:String}`, "aoId", false, nil},
		{"list", `type Evidence @node(paths:["*.md"]){aoId:[String!]}`, "aoId", false, nil},
		{"ID", `type Evidence @node(paths:["*.md"]){aoId:ID}`, "aoId", false, nil},
		{"integer", `type Evidence @node(paths:["*.md"]){aoId:Int}`, "aoId", false, nil},
		{"empty", `type Evidence @node(paths:["*.md"]){aoId:String}`, "", false, nil},
		{"interface", `interface Evidence {aoId:String} type Assertion implements Evidence @node(paths:["*.md"]){aoId:String @field(source:"id")}`, "aoId", true, nil},
		{"interface implementor missing key", `interface Evidence {name:String} type Assertion implements Evidence @node(paths:["*.md"]){name:String aoId:String} type Other implements Evidence @node(paths:["other/*.md"]){name:String}`, "aoId", false, nil},
		{"record unchanged", `type Evidence @node(locator:EMBEDDED) ` + recordTestSource + ` {id:ID @field(path:"id")}`, "aoId", true, nil},
		{"ordered list", `type Evidence @node(paths:["*.md"]){aoId:String @field(source:"id")}`, `["$fileStem","aoId"]`, true, []string{"$fileStem", "aoId"}},
		{"file stem alone", `type Evidence @node(paths:["*.md"]){name:String}`, `["$fileStem"]`, true, []string{"$fileStem"}},
		{"list missing field", `type Evidence @node(paths:["*.md"]){name:String}`, `["$fileStem","aoId"]`, false, nil},
		{"empty list", `type Evidence @node(paths:["*.md"]){aoId:String}`, `[]`, false, nil},
		{"repeated key", `type Evidence @node(paths:["*.md"]){aoId:String}`, `["aoId","aoId"]`, false, nil},
		{"unknown reserved key", `type Evidence @node(paths:["*.md"]){aoId:String}`, `["$path"]`, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key := `"` + tc.key + `"`
			if strings.HasPrefix(tc.key, "[") {
				key = tc.key
			}
			sdl := tc.targets + ` type Row @node(locator:EMBEDDED) ` + recordTestSource + ` {refs:[String!] @field(path:"refs") @references(to:["Evidence"],targetKey:` + key + `,mode:STRICT)}`
			compiled, err := recordSchema(t, sdl)
			if tc.valid {
				require.NoError(t, err)
				want := tc.want
				if want == nil {
					want = []string{tc.key}
				}
				require.Equal(t, want, compiled.Types["Row"].ByName["refs"].Record.References.TargetKeys)
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
