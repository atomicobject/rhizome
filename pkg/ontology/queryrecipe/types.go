// Package queryrecipe loads and validates saved GraphQL query recipes.
package queryrecipe

import ontologyquery "github.com/atomicobject/rhizome/pkg/ontology/query"

// WHY: APIVersion is intentionally rooted at `rhizome.query-recipe.v1` (not
// `ontology-query-recipe`) because saved recipes broadened to runtime-enriched
// roots (`code`) that are not strictly ontology-bound. The bump from
// `rhizome.ontology-query-recipe.v1` is a pure rename — no semantic change to
// the envelope shape — and `validateMetadata` rejects the old value as
// `unsupported_api_version` so stale recipes surface explicitly.
// Spec link: [[saved-query-recipes#^spec-0052-us5-ac6]].
const APIVersion = "rhizome.query-recipe.v1"

type InputMode string

const (
	InputModeNone           InputMode = "none"
	InputModeOptionalAnchor InputMode = "optional_anchor"
	InputModeRequiredAnchor InputMode = "required_anchor"
	InputModeMultiAnchor    InputMode = "multi_anchor"
)

type Source struct {
	Path  string `json:"path,omitempty"`
	Line  int    `json:"line,omitempty"`
	Block string `json:"block,omitempty"`
}

type Recipe struct {
	APIVersion         string             `json:"apiVersion" yaml:"apiVersion"`
	ID                 string             `json:"id" yaml:"id"`
	Name               string             `json:"name" yaml:"name"`
	Problem            string             `json:"problem" yaml:"problem"`
	InputSpec          InputSpec          `json:"inputSpec" yaml:"inputSpec"`
	Query              QuerySpec          `json:"query" yaml:"query"`
	OutputContract     OutputContract     `json:"outputContract" yaml:"outputContract"`
	AdaptationGuidance AdaptationGuidance `json:"adaptationGuidance" yaml:"adaptationGuidance"`
	Examples           []Example          `json:"examples,omitempty" yaml:"examples,omitempty"`
	Tags               []string           `json:"tags,omitempty" yaml:"tags,omitempty"`
	Source             Source             `json:"source,omitempty" yaml:"-"`
}

type InputSpec struct {
	Mode               InputMode `json:"mode" yaml:"mode"`
	PrimaryInput       string    `json:"primaryInput,omitempty" yaml:"primaryInput,omitempty"`
	BroadAlternativeID string    `json:"broadAlternativeId,omitempty" yaml:"broadAlternativeId,omitempty"`
	Inputs             []Input   `json:"inputs,omitempty" yaml:"inputs,omitempty"`
}

type Input struct {
	Name        string `json:"name" yaml:"name"`
	Required    bool   `json:"required,omitempty" yaml:"required,omitempty"`
	Kind        string `json:"kind,omitempty" yaml:"kind,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Default     string `json:"default,omitempty" yaml:"default,omitempty"`
}

type QuerySpec struct {
	GraphQL string `json:"graphQL" yaml:"graphQL"`
}

type OutputContract struct {
	ExpectedPaths []string `json:"expectedPaths,omitempty" yaml:"expectedPaths,omitempty"`
	RowPath       string   `json:"rowPath,omitempty" yaml:"rowPath,omitempty"`
	Empty         string   `json:"empty" yaml:"empty"`
	Partial       string   `json:"partial,omitempty" yaml:"partial,omitempty"`
	HighVolume    string   `json:"highVolume,omitempty" yaml:"highVolume,omitempty"`
}

type AdaptationGuidance struct {
	Summary string   `json:"summary" yaml:"summary"`
	Rules   []string `json:"rules,omitempty" yaml:"rules,omitempty"`
}

type Example struct {
	Name   string            `json:"name,omitempty" yaml:"name,omitempty"`
	Inputs map[string]string `json:"inputs,omitempty" yaml:"inputs,omitempty"`
}

type Issue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Recipe  string `json:"recipe,omitempty"`
	Path    string `json:"path,omitempty"`
	Line    int    `json:"line,omitempty"`
	Field   string `json:"field,omitempty"`
}

type Dependencies struct {
	Roots        []string `json:"roots,omitempty"`
	RuntimeRoots []string `json:"runtimeRoots,omitempty"`
	Fields       []string `json:"fields,omitempty"`
	TypeNames    []string `json:"typeNames,omitempty"`
	EnumValues   []string `json:"enumValues,omitempty"`
	Placeholders []string `json:"placeholders,omitempty"`
}

type ValidationResult struct {
	Recipes []Recipe `json:"recipes"`
	Issues  []Issue  `json:"issues,omitempty"`
}

type CompiledRecipe struct {
	Recipe       Recipe                       `json:"recipe"`
	Query        string                       `json:"query"`
	Variables    map[string]any               `json:"variables,omitempty"`
	Dependencies Dependencies                 `json:"dependencies"`
	Prepared     *ontologyquery.PreparedQuery `json:"-"`
}

type RunResult struct {
	Recipe         Recipe               `json:"recipe"`
	Inputs         map[string]string    `json:"inputs,omitempty"`
	Variables      map[string]any       `json:"variables,omitempty"`
	Query          string               `json:"query"`
	OutputContract OutputContract       `json:"outputContract"`
	Result         ontologyquery.Result `json:"result"`
}
