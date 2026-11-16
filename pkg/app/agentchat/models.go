package agentchat

import (
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/atomicobject/rhizome/pkg/harness"
)

// featuredFamilies are the model families the picker shows by default, the
// latest of each; "Show all models" lists the rest. The first family each
// agent offers is its default model.
var featuredFamilies = map[harness.Kind][]string{
	harness.KindClaude: {"opus", "fable", "sonnet"},
	harness.KindCodex:  {"sol", "astra", "terra"},
}

var modelVersion = regexp.MustCompile(`^\d+(\.\d+)*$`)

// curateModels marks the latest model of each featured family, read from
// names such as "Opus 5.5" or "GPT-6.1-Sol". A bare family name, such as
// Claude Code's signed-out "Opus", is the agent's alias for its latest. The
// latest of the agent's first family becomes its default. An agent whose names
// match no family keeps its own default and has nothing featured, so the
// picker lists all its models.
func curateModels(kind harness.Kind, models []harness.ModelOption) []harness.ModelOption {
	families := featuredFamilies[kind]
	latest := make(map[string]int, len(families))
	versions := make([][]int, len(models))
	for i, model := range models {
		family, version := modelFamily(model, families)
		if family == "" {
			continue
		}
		versions[i] = version
		if best, ok := latest[family]; !ok || slices.Compare(version, versions[best]) > 0 {
			latest[family] = i
		}
	}
	curated := slices.Clone(models)
	for _, i := range latest {
		curated[i].Featured = true
	}
	for _, family := range families {
		if i, ok := latest[family]; ok {
			for j := range curated {
				curated[j].Default = j == i
			}
			break
		}
	}
	return curated
}

func modelFamily(model harness.ModelOption, families []string) (string, []int) {
	name := model.DisplayName
	if name == "" {
		name = model.ID
	}
	family, version := "", []int(nil)
	for _, token := range strings.FieldsFunc(strings.ToLower(name), func(r rune) bool { return r == ' ' || r == '-' || r == '_' }) {
		if family == "" && slices.Contains(families, token) {
			family = token
		}
		if version == nil && modelVersion.MatchString(token) {
			for _, part := range strings.Split(token, ".") {
				n, _ := strconv.Atoi(part)
				version = append(version, n)
			}
		}
	}
	if version == nil {
		version = []int{math.MaxInt}
	}
	return family, version
}
