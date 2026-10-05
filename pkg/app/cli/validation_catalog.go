package actions

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/atomicobject/rhizome/pkg/validate"
)

// ValidationCatalog is the stable machine-readable model behind validate
// list. Checks remain in registry display order.
type ValidationCatalog struct {
	Checks []ValidationCatalogCheck `json:"checks"`
}

// ValidationCatalogCheck exposes only the public check identity and the
// registry metadata needed to select, prepare, and remediate it.
type ValidationCatalogCheck struct {
	Name                       string                              `json:"name"`
	Description                string                              `json:"description"`
	Category                   validate.CheckCategory              `json:"category"`
	Suites                     []validate.CheckSuite               `json:"suites"`
	Applicability              validate.CheckApplicability         `json:"applicability"`
	ProjectionDomains          []validate.ProjectionDomain         `json:"projectionDomains"`
	ExecutionSurfaces          []validate.ExecutionSurface         `json:"executionSurfaces"`
	PrerequisiteMode           validate.PrerequisiteMode           `json:"prerequisiteMode"`
	MissingPrerequisiteOutcome validate.MissingPrerequisiteOutcome `json:"missingPrerequisiteOutcome"`
	PreparationCommand         string                              `json:"preparationCommand,omitempty"`
	Remediation                validate.RemediationSupport         `json:"remediation"`
}

// BuildValidationCatalog projects the authoritative registry into its public
// list model without retaining mutable descriptor slices.
func BuildValidationCatalog() ValidationCatalog {
	descriptors := validate.CheckDescriptors()
	checks := make([]ValidationCatalogCheck, 0, len(descriptors))
	for _, descriptor := range descriptors {
		checks = append(checks, ValidationCatalogCheck{
			Name:                       descriptor.CLIName,
			Description:                descriptor.Description,
			Category:                   descriptor.Category,
			Suites:                     slices.Clone(descriptor.Suites),
			Applicability:              descriptor.Applicability,
			ProjectionDomains:          slices.Clone(descriptor.ProjectionDomains),
			ExecutionSurfaces:          slices.Clone(descriptor.ExecutionSurfaces),
			PrerequisiteMode:           descriptor.PrerequisiteMode,
			MissingPrerequisiteOutcome: descriptor.MissingPrerequisiteOutcome,
			PreparationCommand:         descriptor.PreparationCommand,
			Remediation:                descriptor.Remediation,
		})
	}
	return ValidationCatalog{Checks: checks}
}

// RenderValidationCatalogJSON renders indented machine-valid JSON with one
// trailing newline for direct stdout use.
func RenderValidationCatalogJSON(catalog ValidationCatalog) ([]byte, error) {
	payload, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(payload, '\n'), nil
}

// RenderValidationCatalogHuman renders deterministic explanatory text in the
// same registry order as the JSON catalog.
func RenderValidationCatalogHuman(catalog ValidationCatalog) string {
	var output strings.Builder
	output.WriteString("Validation checks\n")
	for _, check := range catalog.Checks {
		output.WriteString("\n- ")
		output.WriteString(check.Name)
		output.WriteString(": ")
		output.WriteString(check.Description)
		output.WriteString("\n  Category: ")
		output.WriteString(string(check.Category))
		output.WriteString("\n  Suites: ")
		output.WriteString(joinValidationCatalogValues(check.Suites))
		output.WriteString("\n  Applicability: ")
		output.WriteString(string(check.Applicability))
		output.WriteString("\n  Required projections: ")
		output.WriteString(joinValidationCatalogValues(check.ProjectionDomains))
		output.WriteString("\n  Supported surfaces: ")
		output.WriteString(joinValidationCatalogValues(check.ExecutionSurfaces))
		output.WriteString("\n  Prerequisite mode: ")
		output.WriteString(string(check.PrerequisiteMode))
		output.WriteString("\n  Missing prerequisite: ")
		output.WriteString(string(check.MissingPrerequisiteOutcome))
		if check.PreparationCommand != "" {
			output.WriteString("\n  Preparation: ")
			output.WriteString(check.PreparationCommand)
		}
		output.WriteString("\n  Remediation: ")
		output.WriteString(string(check.Remediation.Kind))
		if check.Remediation.Command != "" {
			output.WriteString(" (")
			output.WriteString(check.Remediation.Command)
			output.WriteByte(')')
		}
		output.WriteByte('\n')
	}
	return output.String()
}

func joinValidationCatalogValues[T ~string](values []T) string {
	items := make([]string, len(values))
	for index, value := range values {
		items[index] = string(value)
	}
	return strings.Join(items, ", ")
}
