package providerbudget

import "regexp"

var jevVersion = regexp.MustCompile(`^jev-(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// IsJevModel accepts the latest alias and bounded numeric release IDs.
// TypeSafe resolves model availability; its model list need not include every
// supported version. Unrelated names and other aliases stay disabled.
func IsJevModel(model string) bool {
	return model == "jev-latest" || len(model) <= 64 && jevVersion.MatchString(model)
}
