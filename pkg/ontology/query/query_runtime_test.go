package query

import (
	"context"
	"github.com/atomicobject/rhizome/pkg/diag"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

func TestExecute_RuntimeIdentifierStrategyMetadata(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type Spec @node(paths: ["notes/specs/*.md"]) {
  id: String! @field @identifier(preferred: true, prefix: "SPEC")
}
`, nil)
	require.NotNil(t, env.execSchema.Schema.Types["IdentifierStrategy"].EnumValues.ForName("SEQUENTIAL"))
	require.NotNil(t, env.execSchema.Schema.Types["IdentifierStrategy"].EnumValues.ForName("DATETIME"))
	requireSchemaField(t, env.execSchema, "IdentifierFormat", "strategy", "IdentifierStrategy!")
	prepared, errs := Prepare(env.execSchema, `{
  ontology {
    type(name: "Spec") {
      fields {
        name
        identifierFormat { strategy prefix separator pad }
      }
    }
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	fields := result.Data["ontology"].(map[string]any)["type"].(map[string]any)["fields"].([]any)
	require.Equal(t, map[string]any{
		"name": "id",
		"identifierFormat": map[string]any{
			"strategy":  "SEQUENTIAL",
			"prefix":    "SPEC",
			"separator": "-",
			"pad":       4,
		},
	}, fields[0])
}

func TestExecute_RuntimeNextIDPreservesProspectivePathOrder(t *testing.T) {
	env := newQueryTestEnv(t)
	provider := &recordingOntologyRuntime{}
	prepared, errs := Prepare(env.execSchema, `{
  ontology {
    nextId(type: "Project", paths: ["notes/2026-08-05-14-32-b.md", "notes/2026-08-05-14-32-a.md"]) {
      available
      paths
      allocations { path id base disambiguator }
    }
  }
}`)
	require.Empty(t, errs)
	deps := env.deps(nil)
	deps.OntologyRuntime = provider

	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)
	require.Equal(t, OntologyNextIDRequest{
		Type:  "Project",
		Count: 1,
		Paths: []string{"notes/2026-08-05-14-32-b.md", "notes/2026-08-05-14-32-a.md"},
	}, provider.request)
	next := result.Data["ontology"].(map[string]any)["nextId"].(map[string]any)
	require.Equal(t, []any{"notes/2026-08-05-14-32-b.md", "notes/2026-08-05-14-32-a.md"}, next["paths"])
	require.Equal(t, []any{
		map[string]any{
			"path":          "notes/2026-08-05-14-32-b.md",
			"id":            "PROJECT-2026-08-05-14-32",
			"base":          "PROJECT-2026-08-05-14-32",
			"disambiguator": nil,
		},
		map[string]any{
			"path":          "notes/2026-08-05-14-32-a.md",
			"id":            "PROJECT-2026-08-05-14-32-2",
			"base":          "PROJECT-2026-08-05-14-32",
			"disambiguator": 2,
		},
	}, next["allocations"])
}

func TestExecute_RuntimeIdentifierFormatOmitsPadForDateTime(t *testing.T) {
	env := newCustomQueryTestEnv(t, `
type EffortNote @node(paths: ["efforts/*.md"]) {
  id: String! @identifier(preferred: true, strategy: DATETIME, prefix: "EFF")
}
`, nil)
	prepared, errs := Prepare(env.execSchema, `{
  ontology {
    type(name: "EffortNote") {
      fields { name identifierFormat { strategy pad } }
    }
  }
}`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)
	fields := result.Data["ontology"].(map[string]any)["type"].(map[string]any)["fields"].([]any)
	for _, raw := range fields {
		field := raw.(map[string]any)
		if field["name"] == "id" {
			format := field["identifierFormat"].(map[string]any)
			require.Equal(t, "DATETIME", format["strategy"])
			require.Nil(t, format["pad"])
			return
		}
	}
	t.Fatal("id field not found")
}

func TestExecute_RuntimeRootsReturnPartialUnavailableData(t *testing.T) {
	env := newQueryTestEnv(t)
	prepared, errs := Prepare(env.execSchema, `
{
  ontology {
    schemaHash
    type(name: "Project") {
      name
      role
      fields {
        name
        identifierFormat { prefix pad }
      }
    }
    authoringGuide(type: "Project") {
      available
      warnings { code message }
    }
    nextId(type: "Project", count: 2) {
      available
      errorCode
      ids
      count
      warnings { code }
    }
  }
  code {
    docsForCode(path: "pkg/ontology/query/schema.go") {
      inputPath
      available
      warnings { code path }
    }
  }
}
`)
	require.Empty(t, errs)

	result := Execute(context.Background(), env.deps(nil), env.schema, prepared)
	require.Empty(t, result.Errors)

	ontologyRoot := result.Data["ontology"].(map[string]any)
	require.Equal(t, env.schema.Hash, ontologyRoot["schemaHash"])
	projectType := ontologyRoot["type"].(map[string]any)
	require.Equal(t, "Project", projectType["name"])
	require.Equal(t, "NOTE", projectType["role"])
	require.NotEmpty(t, projectType["fields"].([]any))
	require.False(t, ontologyRoot["authoringGuide"].(map[string]any)["available"].(bool))
	require.Equal(t, "ontology_runtime_unavailable", ontologyRoot["nextId"].(map[string]any)["errorCode"])
	require.Empty(t, ontologyRoot["nextId"].(map[string]any)["ids"].([]any))

	codeRoot := result.Data["code"].(map[string]any)
	require.False(t, codeRoot["docsForCode"].(map[string]any)["available"].(bool))
}

func TestExecute_RuntimeRootsResolveNestedInlineFragments(t *testing.T) {
	env := newQueryTestEnv(t)
	prepared, errs := Prepare(env.execSchema, `
{
  code {
    docsForCode(path: "pkg/ontology/query/schema.go") {
      ... on CodeContextPack {
        available
        docs {
          ... on RuntimePath {
            path
            kind
          }
        }
      }
    }
  }
}
`)
	require.Empty(t, errs)

	deps := env.deps(nil)
	deps.CodeRuntime = fakeCodeRuntime{
		docs: CodeContextPack{
			InputPath:      "pkg/ontology/query/schema.go",
			NormalizedPath: "pkg/ontology/query/schema.go",
			Available:      true,
			Docs:           []RuntimePath{{Path: "pkg/ontology/query/CONTEXT.md", Kind: "ancestor_doc"}},
		},
	}

	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Empty(t, result.Errors)

	codeRoot := result.Data["code"].(map[string]any)
	docsPack := codeRoot["docsForCode"].(map[string]any)
	require.True(t, docsPack["available"].(bool))
	docs := docsPack["docs"].([]any)
	require.Equal(t, "pkg/ontology/query/CONTEXT.md", docs[0].(map[string]any)["path"])
}

func TestExecute_CodeRuntimeErrorKeepsPartialProviderData(t *testing.T) {
	env := newQueryTestEnv(t)
	prepared, errs := Prepare(env.execSchema, `
{
  code {
    docsForCode(path: "../outside.go") {
      inputPath
      normalizedPath
      available
      warnings { code path }
    }
  }
}
`)
	require.Empty(t, errs)

	deps := env.deps(nil)
	deps.CodeRuntime = fakeCodeRuntime{
		docs: CodeContextPack{
			InputPath:      "../outside.go",
			NormalizedPath: "",
			Available:      false,
			Warnings:       []diag.Warning{{Code: "invalid_code_path", Path: "../outside.go"}},
		},
		err: os.ErrPermission,
	}

	result := Execute(context.Background(), deps, env.schema, prepared)
	require.Len(t, result.Errors, 1)

	codeRoot := result.Data["code"].(map[string]any)
	docsPack := codeRoot["docsForCode"].(map[string]any)
	require.False(t, docsPack["available"].(bool))
	require.Equal(t, "../outside.go", docsPack["inputPath"])
	warnings := docsPack["warnings"].([]any)
	require.Equal(t, "invalid_code_path", warnings[0].(map[string]any)["code"])
}

type fakeCodeRuntime struct {
	docs  CodeContextPack
	code  CodeContextPack
	tests CodeContextPack
	err   error
}

type recordingOntologyRuntime struct {
	request OntologyNextIDRequest
}

func (*recordingOntologyRuntime) AuthoringGuide(context.Context, OntologyAuthoringGuideRequest) (OntologyAuthoringGuide, error) {
	return OntologyAuthoringGuide{}, nil
}

func (r *recordingOntologyRuntime) NextID(_ context.Context, request OntologyNextIDRequest) (OntologyNextID, error) {
	r.request = request
	disambiguator := 2
	return OntologyNextID{
		Type:  request.Type,
		Count: len(request.Paths),
		Paths: append([]string(nil), request.Paths...),
		Allocations: []OntologyIDAllocation{
			{
				Path: request.Paths[0],
				ID:   "PROJECT-2026-08-05-14-32",
				Base: "PROJECT-2026-08-05-14-32",
			},
			{
				Path:          request.Paths[1],
				ID:            "PROJECT-2026-08-05-14-32-2",
				Base:          "PROJECT-2026-08-05-14-32",
				Disambiguator: &disambiguator,
			},
		},
		Available: true,
	}, nil
}

func (*recordingOntologyRuntime) CurrentUser(context.Context) (OntologyCurrentUser, error) {
	return OntologyCurrentUser{}, nil
}

func (f fakeCodeRuntime) DocsForCode(context.Context, CodeRuntimeRequest) (CodeContextPack, error) {
	return f.docs, f.err
}

func (f fakeCodeRuntime) CodeForNote(context.Context, CodeRuntimeRequest) (CodeContextPack, error) {
	return f.code, f.err
}

func (f fakeCodeRuntime) TestsForCode(context.Context, CodeRuntimeRequest) (CodeContextPack, error) {
	return f.tests, f.err
}
