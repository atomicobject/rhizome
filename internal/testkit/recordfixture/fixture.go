// Package recordfixture supplies a synthetic mixed-format vault, never AO data.
package recordfixture

const SDL = `
type Host @node(default:true) { status:String }
type Evidence @node(paths:["evidence.md"]) { name:String models:[Service!] @link }
type Service @node(locator:EMBEDDED) @source(shape:RECORD,paths:["model/services.yaml"],select:"services",key:{field:"id"},title:["name","$key"],space:"selected",logicalKind:"service",membership:{path:"products",includes:"booking",includeMissing:true}) {
 id:ID @field(path:"id")
 name:String @field(path:"name")
 empty:String @field(path:"empty")
 nothing:String @field(path:"nothing")
 absent:String @field(path:"absent")
 refs:[Service!] @field(path:"refs") @references(to:["Service"],mode:STRICT,values:BY_SHAPE,edge:{name:"uses"})
 checks:[String!] @field(path:"checks") @references(to:["Service"],mode:STRICT,values:ELEMENTS)
 tolerant:[String!] @field(path:"tolerant") @references(to:["Service"],mode:TOLERANT,values:ELEMENTS,allowWildcard:true)
 reads:[Service!] @field(path:"reads") @references(to:["Service"],mode:STRICT,encoding:TYPED,values:ELEMENTS,targets:[{tag:"field",type:"Service",memberPath:"fields[].name"}],edge:{name:"reads"})
 jobs:[Job!] @contains(shape:RECORD,select:"jobs",edge:{name:"made-of"})
}
type Job @node(locator:EMBEDDED) @source(shape:RECORD,paths:["model/services.yaml"],select:"services[].jobs",key:null,title:["$value"],scalarAs:"id",container:"Service",space:"selected",logicalKind:"job") { id:String @field(path:"id") }
type Grouped @node(locator:EMBEDDED) @source(shape:RECORD,paths:["model/groups.yml"],select:"groups{}[]",key:{field:"id"},title:["name"],space:"selected",logicalKind:"grouped") { id:ID @field(path:"id") name:String @field(path:"name") group:String @field(path:"group",fallback:MAPPING_KEY) }
type Transition @node(locator:EMBEDDED) @source(shape:RECORD,paths:["generated/model.json"],select:"links",key:{template:"{from}>{to}"},title:["$key"],space:"selected",logicalKind:"transition") { from:String @field(path:"from") to:String @field(path:"to") }
type AccountEntry @node(locator:EMBEDDED) @source(shape:RECORD,paths:["account.yaml"],select:"entries",key:null,title:["$key"],scalarAs:"id",space:"selected",logicalKind:"entry",sourceOrder:0,membership:{path:"products",includes:"booking",includeMissing:true}) { id:String @field(path:"id") }
type ProductEntry @node(locator:EMBEDDED) @source(shape:RECORD,paths:["product.yaml"],select:"entries",key:null,title:["$key"],scalarAs:"id",space:"selected",logicalKind:"entry",sourceOrder:1) { id:String @field(path:"id") }
`

var Includes = []string{"*.md", "model/*.{yaml,yml}", "account.yaml", "product.yaml", "generated/model.json"}

const Services = `services:
  - id: A
    name: Shared title
    empty: ''
    nothing: null
    refs: [B, B, missing]
    checks: [B]
    tolerant: [unknown, '*']
    reads: [field:B.code]
    jobs: [plan, ship]
    raw: {ordered: [null, '', [], {}], a/b~: untouched alpha evidence}
  - id: B
    name: Shared title
    refs: []
    fields: [{name: code}]
    raw: beta evidence belongs only to B
  - id: hidden
    name: Other product
    products: [other]
`

func Files() map[string]string {
	return map[string]string{
		"model/services.yaml":       Services,
		"model/groups.yml":          "groups:\n  core:\n    - {id: G1, name: Group one}\n    - {id: G2, name: Group two, group: authored}\n  edge:\n    - {id: G3, name: Group three}\n",
		"generated/model.json":      `{"links":[{"from":"A","to":"B","raw":{"saved":true,"empty":[]}}]}`,
		"generated/unselected.json": `{"links":[{"from":"hidden","to":"hidden"}]}`,
		"unselected.yaml":           "services: [{id: A, name: Unselected collision}]\n",
		"account.yaml":              "entries: [{id: account, products: [booking]}, {id: hidden, products: [other]}]\n",
		"product.yaml":              "entries: [product]\n",
		"evidence.md":               "---\nname: Evidence\n---\n# Evidence\n\nRecorded Markdown evidence.\n",
	}
}
