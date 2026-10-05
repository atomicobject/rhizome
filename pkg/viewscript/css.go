package viewscript

import (
	"path"
	"regexp"
	"strings"
)

// cssModuleQuery marks a request for a stylesheet as a module (SPEC-0111). A
// view's side-effect import of a sibling .css file is rewritten to it, because
// a browser will not run a stylesheet as a module script.
const cssModuleQuery = "rhizome-css"

// esbuild prints every side-effect import alone on its line with double
// quotes, so its output, unlike the authored source, has one form to match.
var cssSideEffectImport = regexp.MustCompile(`(?m)^import "(\.\.?/[^"\n]*\.(?i:css))";$`)

func rewriteCSSImports(code []byte) []byte {
	return cssSideEffectImport.ReplaceAll(code, []byte(`import "$1?`+cssModuleQuery+`";`))
}

// IsCSSModuleRequest reports whether a request for rel asks for the module
// that applies the stylesheet rather than the stylesheet itself.
func IsCSSModuleRequest(rel, rawQuery string) bool {
	return rawQuery == cssModuleQuery && strings.EqualFold(path.Ext(rel), ".css")
}

// CSSModule is the response to every stylesheet module request. It links the
// stylesheet it was loaded for, once per document, and finishes evaluating only
// after the stylesheet loads, so the view never renders unstyled.
const CSSModule = `const url = new URL(import.meta.url);
url.search = "";
if (![...document.querySelectorAll('link[rel="stylesheet"]')].some((link) => link.href === url.href)) {
  const link = document.createElement("link");
  link.rel = "stylesheet";
  link.href = url.href;
  await new Promise((resolve) => {
    link.onload = link.onerror = resolve;
    document.head.append(link);
  });
}
`
