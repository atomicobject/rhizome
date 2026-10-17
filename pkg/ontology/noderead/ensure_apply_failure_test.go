package noderead

import "github.com/atomicobject/rhizome/pkg/ontology"

const ensureApplySchema = `
type ProductSpec @node(paths: ["specs/product.md"]) { stories: StoriesSection @contains(level: H2, heading: "Stories") }
type StoriesSection implements Section { stories: [UserStory!] @contains(level: H3) }
type UserStory implements Section @node(locator: EMBEDDED) { related: ProductSpec @link }
`
const ensureApplySource = "# Product\n\n## Stories\n\n### Story A\nrelated:: [[specs/product]]\n"

func ensureApplyRequest() ResolveRequest {
	return ResolveRequest{Targets: []NodeTarget{{Input: "specs/product.md#Story A"}}, EnsureLinkTarget: ontology.EnsureLinkTargetApply, Hydrate: HydrateOptions{Profile: HydrateSummary}}
}
