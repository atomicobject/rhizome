package validate

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunViewsReportsExperienceProblemsWithFileAndField(t *testing.T) {
	root := t.TempDir()
	site := root + "/.rhizome/modules/team/views/site/"
	writeValidationTestFile(t, site+"experience.yml", "colour: teal\n")
	writeValidationTestFile(t, site+"page.yaml", "apiVersion: rhizome.view.v1\nid: site.page\nname: Page\nsource: {kind: custom, entry: page.tsx}\nmount: {kind: standalone}\n")
	writeValidationTestFile(t, site+"page.tsx", "export function Page() { return null }\n")

	result := runViewsAt(root)
	fields := map[string]string{}
	for _, issue := range result.Issues {
		fields[issue.Code+" "+issue.Field] = issue.Path
	}
	require.Equal(t, ".rhizome/modules/team/views/site/experience.yml", fields["missing_required_field title"])
	require.Equal(t, ".rhizome/modules/team/views/site/experience.yml", fields["unknown_experience_field colour"])
	require.Equal(t, ".rhizome/modules/team/views/site/page.yaml", fields["experience_view_own_frame source.entry"], "the page lacks a default export")
}
