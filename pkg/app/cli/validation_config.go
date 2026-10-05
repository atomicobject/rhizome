package actions

import (
	"slices"

	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
)

// ValidationSuiteConfigFromLocal copies the repo-local validation overlays
// into the validation package's suite-composition contract.
func ValidationSuiteConfigFromLocal(local obsidian.LocalValidationConfig) validate.SuiteConfig {
	return validate.SuiteConfig{
		Default: validate.SuiteOverlay{
			Add:  slices.Clone(local.Default.Add),
			Skip: slices.Clone(local.Default.Skip),
		},
		All: validate.SuiteOverlay{
			Add:  slices.Clone(local.All.Add),
			Skip: slices.Clone(local.All.Skip),
		},
	}
}
