package actions

import (
	"os"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/stretchr/testify/require"
)

func TestBuildValidationCatalog_UsesRegistryOrderAndPublicNames(t *testing.T) {
	t.Parallel()

	descriptors := validate.CheckDescriptors()
	catalog := BuildValidationCatalog()

	require.Len(t, catalog.Checks, len(descriptors))
	for index, check := range catalog.Checks {
		require.Equal(t, descriptors[index].CLIName, check.Name)
		require.NotContains(t, check.Name, "_")
	}
	require.Equal(t, []validate.CheckSuite{validate.SuiteDefault, validate.SuiteAll}, catalog.Checks[0].Suites)
	require.Equal(t, []validate.CheckSuite{validate.SuiteDefault, validate.SuiteAll}, catalog.Checks[1].Suites)
	require.Equal(t, []validate.CheckSuite{validate.SuiteDefault, validate.SuiteAll}, catalog.Checks[2].Suites)
	require.Equal(t, []validate.CheckSuite{validate.SuiteAudit}, catalog.Checks[len(catalog.Checks)-1].Suites)
}

func TestRenderValidationCatalog_GoldenJSON(t *testing.T) {
	t.Parallel()

	want, err := os.ReadFile("testdata/validation_catalog.json.golden")
	require.NoError(t, err)

	got, err := RenderValidationCatalogJSON(validationCatalogGoldenFixture())
	require.NoError(t, err)
	require.Equal(t, normalizeValidationCatalogGolden(string(want)), string(got))
}

func TestRenderValidationCatalog_GoldenHuman(t *testing.T) {
	t.Parallel()

	want, err := os.ReadFile("testdata/validation_catalog.txt.golden")
	require.NoError(t, err)

	got := RenderValidationCatalogHuman(validationCatalogGoldenFixture())
	require.Equal(t, normalizeValidationCatalogGolden(string(want)), got)
	require.Contains(t, got, "Preparation: rzm index")
	require.NotContains(t, got, "Preparation: none")
}

func TestBuildValidationCatalog_ReturnsIndependentCopies(t *testing.T) {
	t.Parallel()

	catalog := BuildValidationCatalog()
	catalog.Checks[0].Suites[0] = validate.SuiteAudit
	fresh := BuildValidationCatalog()
	require.Equal(t, validate.SuiteDefault, fresh.Checks[0].Suites[0])
	require.False(t, strings.Contains(fresh.Checks[0].Name, "_"))
}

func validationCatalogGoldenFixture() ValidationCatalog {
	full := BuildValidationCatalog()
	fixture := ValidationCatalog{Checks: make([]ValidationCatalogCheck, 0, 2)}
	for _, check := range full.Checks {
		if check.Name == "broken-links" || check.Name == "code-anchors" {
			fixture.Checks = append(fixture.Checks, check)
		}
	}
	return fixture
}

func normalizeValidationCatalogGolden(value string) string {
	return strings.ReplaceAll(value, "\r\n", "\n")
}
