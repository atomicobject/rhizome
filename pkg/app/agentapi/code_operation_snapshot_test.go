package agentapi

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/stretchr/testify/require"
	"testing"
)

// Public describe fingerprints protect schemas and metadata through ownership
// moves; the start/discovery entries include the current upstream contracts.
func TestCodeOperationSchemaContracts(t *testing.T) {
	expected := map[string]string{
		"action_list":              "4820fff520932dee20c55d9a89d3870aef4b40464ed1ef67b058e82b981f2230",
		"action_run":               "befff2c184e50536b935443beecbb60aab56268bc4aeb826746a287bb7b755f3",
		"check_paths":              "b454a87eb4b135408bac46ff68a4933c0de44b97bcd3d1c08c7ede3a4753c5fd",
		"code_rationale":           "5c5a46fca519a40d53dcbaaee957db1a3e707635ecfe0119f3a30d368ebd03a8",
		"code_references":          "54139de4ffa9cda87a5a6cc6e714de102d9502e9258ee29122aa57d7fe9fabc4",
		"code_symbol":              "003e05e02cd69c09952fa536afb725c4f4009067681a9a266069d498f1f1eee6",
		"code_symbol_context":      "671691d34635c5f4e954700c8c69d3737cda6dfcc318bb4a78f7764aaf94ced0",
		"community_list":           "8a22dd55004b9e8979685fd06423f69464d628a58726b82a3e5e8c09ad747b7e",
		"current_user":             "d52cfca9bd7ab13430854e9060eeaf474a5bdeff67195e044fac7b7dc280777e",
		"evaluate":                 "8325fba318839710dddb6b7223d35ec56984f0b0d7c9d8edd028156f7d8d2bb6",
		"evaluate_batch":           "464fbfe48a6682123f38d9d2d4d06399548131bbc5d8a20cca7d9f2540efd231",
		"external_references":      "887d63d4f6fd6325adeb52525aa419702123d363d5b9575cc7e004644a975bcd",
		"file_context":             "a3a07a7add13dd08d8e2bf8171910ad45c44dcc3ee1bc287d66e76cac417e5d6",
		"files":                    "1ec76e0234e80334e0c97cd77746f54d005d22ec3bd0ffe2814b41bfe802c290",
		"find_connections":         "8bcfe94cda9453ff1f3c22be745bb56ec58a798d50d55f26753e314a84fb0faa",
		"generate":                 "22d95f0dea7a7317d313370660c87abb9fe3f64109148c01e982152617328188",
		"graph_path":               "038bdbec2b7f0795cc1cf71d964d963c217aee3bf3738068d00b4002777799f8",
		"list_properties":          "04fc0093820ef47e96488bd52efaccf2bd0ce4f7afa18377e2fd31d8f6518f9d",
		"list_tags":                "e3014b4e9a83584c02cbbde0d7bb6249ff4df22bfb31bf58584cf4b604b418e8",
		"next_id":                  "ba2124e0b010ce3364b2db1419efeaad4e6a0c78565601b9505b03518267b64a",
		"node_link":                "b0a2f14ea50e6257f046f4ac022f01b49c77c71b3d8f637009c6c28f1de844cf",
		"note_move":                "ac1a34fa82431083ac13ce03e450ddfb1d239566661343a361ede7f82a92a21d",
		"note_rename_heading":      "2c2adff686465350a1c1fbadc320c91321f5d3f66603a87eece545fc4b29da02",
		"ontology_authoring_guide": "dcb3f6fe49740d76676bdc2df4b9dd3ff73b6d37d8d130905c9d1aafd5a07796",
		"ontology_inspect":         "cafdb536f914cf3e912968851ffd741cca0acbe5b65b827360b919398c3f89c9",
		"ontology_query":           "442785dfcd5e37e4349ce015124f6cc2f9c75c020c87a96aa7af1fda4e42d977",
		"ontology_query_schema":    "c8beb45593fdaaddcaf9ac985eb6dc587f39d6c7afc7cc6f3901a95cd018c7a5",
		"ontology_reference":       "996c1b66a9d1711220eb5b6ad56e7c46be40c23ae919194c05b0ac997c4a4034",
		"query_recipe":             "765d86d2e8ba7ba492a8a7df242b6cf22a97a3da3fe1e5f163e3b15cb0af7987",
		"report":                   "2316162fabf2d714efde17c1d942903104cf706548c1e0cf37fa9be311fb73cc",
		"semantic_query":           "1a8c5e22d67160aa363afbe19df948987a6a614db8df1873d86d640653ecf8ba",
		"start":                    "fe4555ab6167b603fd1781e439ebfb62419d16f3f04cf1ba5b0c0cc6daa2b525",
		"surface":                  "b048502ec8bc44e071ba002343ba9065a48b6686b887363d19301d07c6f71c9f",
		"validate":                 "9b2d099bac57b1faa899e69a8a575b6d0086cebb91edf237ffd19f7844e72d73",
		"vault_context":            "30533e3662deffcc9dc7e78f0bd6be5f6795b08d3b922820eb680e4ed59a52f4",
		"vault_health":             "04f7bf82759586c8a4f9882bb544d3b0ef519df1401f04d30f40a556dbe09370",
		"view":                     "72a1d49cf169f9ec22949a2f0f7e20acf0b9f8b0ad36a32e56e799814a6d33f5",
	}
	require.Len(t, CodeOperationDescriptors(), len(expected))
	for _, descriptor := range CodeOperationDescriptors() {
		encoded, err := json.Marshal(map[string]any{"input": descriptor.CodeContract.InputSchema, "output": descriptor.CodeContract.OutputSchema})
		require.NoError(t, err)
		require.Equal(t, expected[descriptor.Name], fmt.Sprintf("%x", sha256.Sum256(encoded)), descriptor.Name)
	}
}

func TestSemanticContinuationDescriptionMatchesValidation(t *testing.T) {
	descriptor, ok := CodeOperationDescriptor("semantic_query")
	require.True(t, ok)
	require.Contains(t, descriptor.CodeContract.Effects, "provide queries on every page; continuation requests repeat the same queries")
	require.ErrorContains(t, ValidateCodeInput("semantic_query", map[string]any{"continuationToken": "opaque"}), "input.queries is required; continuation requests repeat the same queries")
	require.NoError(t, ValidateCodeInput("semantic_query", map[string]any{"queries": []any{"indexing"}, "continuationToken": "opaque"}))
}

func TestCodeOperationConditionsStayOnSelectedDescriptor(t *testing.T) {
	for _, test := range []struct {
		operation string
		input     map[string]any
		error     string
	}{
		{"ontology_reference", map[string]any{"compact": true}, "input.type is required"},
		{"files", map[string]any{}, "input.inputs is required without continuationToken"},
		{"view", map[string]any{"action": "run"}, "input.id is required"},
		{"current_user", map[string]any{"action": "set"}, "input.personTitleOrRef is required"},
		{"find_connections", map[string]any{}, "input.note or input.text is required"},
		{"node_link", map[string]any{}, "input.refs or input.targets is required"},
		{"note_move", map[string]any{}, "input.source or input.sources is required"},
		{"note_move", map[string]any{"source": "a", "target": "b", "toFolder": "c"}, "input.target cannot be combined with input.toFolder"},
		{"note_move", map[string]any{"sources": []any{"a"}}, "input.source and input.target are required unless sources and toFolder are used"},
	} {
		require.EqualError(t, ValidateCodeInput(test.operation, test.input), test.error, test.operation)
	}
	for _, test := range []struct {
		operation string
		input     map[string]any
	}{
		{"current_user", map[string]any{"action": "show"}},
		{"view", map[string]any{"action": "list"}},
		{"node_link", map[string]any{"targets": []any{}}},
		{"note_move", map[string]any{"sources": []any{"a"}, "toFolder": "b"}},
	} {
		require.NoError(t, ValidateCodeInput(test.operation, test.input), test.operation)
	}
}
