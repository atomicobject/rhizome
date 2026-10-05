package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvaluateContentErrors(t *testing.T) {
	for _, test := range []struct {
		kind, content, message string
	}{
		{"workspace-id", "EFF-0042", "DATETIME"},
		{"spec-metadata", "---\ntype: [\n---\n", "invalid governing spec frontmatter"},
	} {
		t.Run(test.kind, func(t *testing.T) {
			_, err := evaluate(request{Kind: test.kind, Content: test.content})
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("expected content error containing %q, got %v", test.message, err)
			}
		})
	}
}

func TestEvaluateCanonicalSnapshots(t *testing.T) {
	value, err := evaluate(request{Kind: "workspace-id", Content: "EFF-2026-09-13-10-00"})
	if err != nil || value != true {
		t.Fatalf("valid workspace id: got %v, %v", value, err)
	}
	value, err = evaluate(request{Kind: "spec-metadata", Content: "---\ntype: TechnicalSpec\n---\n"})
	if err != nil {
		t.Fatal(err)
	}
	if metadata, ok := value.(map[string]any); !ok || metadata["type"] != "TechnicalSpec" {
		t.Fatalf("valid spec metadata: got %#v", value)
	}
}

func TestTargetTypeUsesSuppliedSnapshotSchema(t *testing.T) {
	checkout := t.TempDir()
	dir := filepath.Join(checkout, ".rhizome", "ontology")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dirty.graphql"), []byte("invalid dirty checkout schema"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(checkout)
	// The standalone API still reads the checkout. Snapshot requests must not.
	if _, err := loadTargetSchema(nil); err == nil {
		t.Fatal("standalone parser ignored dirty checkout schema")
	}
	_, err := evaluate(request{Kind: "target-type", Content: `{"path":"snapshot.md","content":"valid content"}`})
	var schemaError *snapshotSchemaError
	if err == nil || errors.As(err, &schemaError) {
		t.Fatalf("standalone parser error compatibility changed: %v", err)
	}

	for _, test := range []struct{ name, selector, want string }{
		{"parent", "docs/efforts/*/materials/old.html", "EffortMaterial"},
		{"final", "docs/efforts/*/materials/new.html", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			content, err := json.Marshal(map[string]any{
				"path": "docs/efforts/example/materials/old.html", "content": "<html><body>Plan</body></html>",
				"schema_files": map[string]string{"effort.graphql": `type EffortMaterial @node(paths: ["` + test.selector + `"]) { title: String }`},
			})
			if err != nil {
				t.Fatal(err)
			}
			got, err := evaluate(request{Kind: "target-type", Content: string(content)})
			if err != nil {
				t.Fatal(err)
			}
			if test.want != "" && got != test.want {
				t.Fatalf("got %v, want %s", got, test.want)
			}
			if test.want == "" && got == "EffortMaterial" {
				t.Fatalf("final schema accepted historical selector")
			}
		})
	}
}

func TestExplicitEmptySnapshotSchemaDoesNotUseCheckout(t *testing.T) {
	_, err := loadTargetSchema(map[string]string{})
	if err == nil {
		t.Fatal("empty snapshot schema unexpectedly loaded")
	}
}

func TestSnapshotSchemaFailuresAreDistinctFromContentErrors(t *testing.T) {
	for _, kind := range []string{"target-type", "spec-like"} {
		for _, schema := range []string{"type Broken {", "type Broken @node { field: UndefinedType }"} {
			content, err := json.Marshal(map[string]any{
				"path": "snapshot.md", "content": "---\ntype: TechnicalSpec\n---\n",
				"schema_files": map[string]string{"broken.graphql": schema},
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = evaluate(request{Kind: kind, Content: string(content)})
			var schemaError *snapshotSchemaError
			if !errors.As(err, &schemaError) {
				t.Fatalf("%s: expected snapshot schema failure, got %v", kind, err)
			}
		}
	}
	_, err := evaluate(request{Kind: "spec-metadata", Content: "---\ntype: [\n---\n"})
	var schemaError *snapshotSchemaError
	if err == nil || errors.As(err, &schemaError) {
		t.Fatalf("expected ordinary metadata error, got %v", err)
	}
}
