package operations

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/stretchr/testify/require"
)

func TestValidationReplanCommandKeepsApplyTextInsideArguments(t *testing.T) {
	request := ValidationProductRequest{Selectors: []string{"broken-links"}, Surface: validate.SurfaceAgent, VaultName: "docs --apply here", ScopeNote: "Notes/A --apply B.md", SkipAnchors: true, AllowHistorical: true}
	require.Equal(t, "rzm agent validate fix broken-links --vault 'docs --apply here' --skip-anchors --scope-note 'Notes/A --apply B.md' --allow-historical", BuildValidationProductReplanCommand(request))
	require.Equal(t, "rzm agent validate fix broken-links --apply --vault 'docs --apply here' --skip-anchors --scope-note 'Notes/A --apply B.md' --allow-historical", BuildValidationProductApplyCommand(request))
}
