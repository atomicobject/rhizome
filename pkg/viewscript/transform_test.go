package viewscript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTransformTSX(t *testing.T) {
	out, err := Transform("poc/board.tsx", []byte(`import { Card } from "./Card.tsx";
type Props = { title: string };
export default function Board({ title }: Props) { return <Card>{title}</Card>; }`))
	if err != nil {
		t.Fatal(err)
	}
	code := string(out)
	for _, want := range []string{`from "react/jsx-runtime"`, `from "./Card.tsx"`, "export {"} {
		if !strings.Contains(code, want) && !(want == "export {" && strings.Contains(code, "export default")) {
			t.Fatalf("missing %q in:\n%s", want, code)
		}
	}
	if strings.Contains(code, "type Props") {
		t.Fatalf("types were not stripped:\n%s", code)
	}
}

func TestTransformReportsLocation(t *testing.T) {
	_, err := Transform("poc/broken.tsx", []byte("export default function () {\n  return <div>;\n}"))
	if err == nil || !strings.HasPrefix(err.Error(), "poc/broken.tsx:") {
		t.Fatalf("want located error, got %v", err)
	}
}

func TestNeedsTransform(t *testing.T) {
	css := []byte(`import './a.css'`)
	if !NeedsTransform("a.tsx", nil) || NeedsTransform("a.js", []byte("export const a = 1")) || NeedsTransform("a.css", css) {
		t.Fatal("unexpected loader selection")
	}
	if !NeedsTransform("a.js", css) || !NeedsTransform("a.mjs", []byte(`import"../b.CSS";`)) {
		t.Fatal("a plain module importing a stylesheet needs its import rewritten")
	}
}

func TestTransformPlainModuleRewritesOnlyStylesheetImports(t *testing.T) {
	out, err := Transform("poc/board.mjs", []byte("import './board.css'\nimport { part } from './part.js'\nexport const value = part ?? null\n"))
	if err != nil {
		t.Fatal(err)
	}
	code := string(out)
	for _, want := range []string{`import "./board.css?rhizome-css";`, `from "./part.js"`, "part ?? null", "export const value"} {
		if !strings.Contains(code, want) {
			t.Fatalf("missing %q in:\n%s", want, code)
		}
	}
}

func TestCheckDirReportsBrokenPlainModulesThatNeedTransform(t *testing.T) {
	root := t.TempDir()
	for rel, body := range map[string]string{
		"poc/styled.js": "import './a.css'\nexport const = ;",
		"poc/raw.js":    "export const = ;",
	} {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, _, err := CheckDir(os.DirFS(root), "poc")
	if err != nil || len(got) != 1 || !strings.HasPrefix(got[0], "poc/styled.js:2:") {
		t.Fatalf("diagnostics = %q, err = %v", got, err)
	}
}

func TestCheckDirReportsEveryBrokenScript(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"poc/board.tsx":              "export default () => <p>ok</p>;",
		"poc/components/Broken.tsx":  "export const Broken = () => <div>;",
		"poc/node_modules/x/bad.tsx": "export const = ;",
		"other/bad.tsx":              "export const = ;",
	}
	for rel, body := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, unreadable, err := CheckDir(os.DirFS(root), "poc")
	if err != nil || len(unreadable) != 0 || len(got) != 1 || !strings.HasPrefix(got[0], "poc/components/Broken.tsx:1:") {
		t.Fatalf("diagnostics = %q, unreadable = %q, err = %v", got, unreadable, err)
	}
	if _, _, err := CheckDir(os.DirFS(root), "absent"); err == nil {
		t.Fatal("a missing folder must be an error, not an empty report")
	}
}

func TestCheckDirReportsUnreadableScripts(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "poc", "board.tsx")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("export default null"), 0o000); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(script); err == nil {
		t.Skip("file permissions do not block reads here")
	}
	diagnostics, unreadable, err := CheckDir(os.DirFS(root), "poc")
	if err != nil || len(diagnostics) != 0 || len(unreadable) != 1 || !strings.HasPrefix(unreadable[0], "poc/board.tsx: cannot read") {
		t.Fatalf("diagnostics = %q, unreadable = %q, err = %v", diagnostics, unreadable, err)
	}
}
