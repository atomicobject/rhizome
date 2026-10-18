package operations

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/atomicobject/rhizome/pkg/validate"
	"github.com/stretchr/testify/require"
)

func TestValidationJSONOmitsPrivateRepairBytes(t *testing.T) {
	for _, length := range []int{64, 128_000} {
		source := strings.Repeat("private source ", length)
		replacement := strings.Repeat("private replacement ", length)
		plan := validate.RepairPlan{Operations: []validate.RepairOperation{{
			ID: "operation", Kind: validate.RepairOperationWrite, Path: "note.md", SourceHash: validate.SourceHash([]byte(source)),
			Content: []byte(replacement), Expected: []validate.ExpectedText{{StartByte: 0, EndByte: len(source), Text: source, Replacement: replacement}},
		}}}
		var err error
		plan.Fingerprint, err = validate.RepairPlanFingerprint(plan)
		require.NoError(t, err)
		var output bytes.Buffer
		require.NoError(t, RenderCI(&output, CIFormatJSON, ValidationResult{Result: validate.Result{FixPlan: &plan}}))
		require.Less(t, output.Len(), 2048, "diagnostic output is bounded independently of complete file payloads")
		var decoded map[string]any
		require.NoError(t, json.Unmarshal(output.Bytes(), &decoded))
		operation := decoded["fixPlan"].(map[string]any)["operations"].([]any)[0].(map[string]any)
		require.NotContains(t, operation, "content")
		span := operation["expected"].([]any)[0].(map[string]any)
		require.NotContains(t, span, "text")
		require.NotContains(t, span, "replacement")
		require.NotContains(t, output.String(), "private source")
		require.NotContains(t, output.String(), "private replacement")
	}
}
