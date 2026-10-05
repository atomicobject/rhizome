package codeanchor

// Docs:
// - [Code anchors (Hub)](docs/hubs/Code anchors (Hub).md)
// - [Code anchors - frontmatter syntax](docs/reference/guides/code-anchors-frontmatter-syntax.md)

import (
	"fmt"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Frontmatter represents the YAML fields we care about.
type Frontmatter struct {
	Title       string                  `yaml:"title"`
	Anchors     []AnchorDefinition      `yaml:"anchors"`      // legacy
	CodeAnchors map[string][]AnchorSpec `yaml:"code-anchors"` // new syntax: lang -> anchors (supports scalar shorthand)
}

// AnchorDefinition captures user-defined anchors inside front matter.
type AnchorDefinition struct {
	Define struct {
		Label   string              `yaml:"label"`
		Kind    AnchorKind          `yaml:"kind"`
		Lang    Lang                `yaml:"lang"`
		BaseSym *SymbolRef          `yaml:"baseSymbol"`
		Ann     *AnnotationSelector `yaml:"annotation"`
	} `yaml:"define"`
	BaseClass  string `yaml:"baseClass"`  // shorthand: "lang:pkg.Name" or "pkg.Name"
	Annotation string `yaml:"annotation"` // shorthand for decorator: "lang:pkg.Decorator"
	// FunctionUse is a legacy shorthand for function anchors; it behaves like "baseClass"/"symbol".
	FunctionUse string `yaml:"functionUse"`
}

// AnchorInlineSpec is the new, simplified anchor definition per language block.
type AnchorInlineSpec struct {
	Label     string            `yaml:"label,omitempty"`     // optional override; defaults from symbol/call/decorator name
	Ref       string            `yaml:"ref,omitempty"`       // shorthand selector; supports optional prefixes (symbol:/calls:/decorator:/glob:/dir:)
	Symbol    string            `yaml:"symbol,omitempty"`    // fully-qualified symbol (module.path.name)
	Calls     string            `yaml:"calls,omitempty"`     // function call target (module.path.func)
	BaseClass string            `yaml:"baseClass,omitempty"` // fully-qualified base type; matches subclasses/implementations where supported
	Decorator string            `yaml:"decorator,omitempty"` // decorator/annotation symbol
	Dir       string            `yaml:"dir,omitempty"`       // legacy alias for glob (directory special-case)
	Glob      string            `yaml:"glob,omitempty"`      // glob pattern (doublestar)
	Globs     []string          `yaml:"globs,omitempty"`     // multiple glob patterns (doublestar)
	Args      map[string]string `yaml:"args,omitempty"`      // optional arg filters (for decorators)
}

// AnchorSpec supports both mapping-style anchor specs and a scalar shorthand.
//
// Scalar shorthand examples:
// - `some.module.SymbolName`           (defaults to symbol match-all)
// - `calls:some.module.func`           (explicit call target; currently an alias for symbol matching)
// - `decorator:some.module.Decorator`  (annotation/decorator selector)
// - `glob:backend/**/*.py`             (path/glob selector)
type AnchorSpec struct {
	AnchorInlineSpec `yaml:",inline"`
	Raw              string `yaml:"-"`
}

func (s *AnchorSpec) UnmarshalYAML(value *yaml.Node) error {
	if value == nil {
		return nil
	}
	switch value.Kind {
	case yaml.ScalarNode:
		s.Raw = strings.TrimSpace(value.Value)
		return nil
	case yaml.MappingNode:
		var spec AnchorInlineSpec
		if err := value.Decode(&spec); err != nil {
			return err
		}
		s.AnchorInlineSpec = spec
		return nil
	default:
		return fmt.Errorf("invalid code anchor spec (expected scalar or mapping)")
	}
}

// ParseNote parses a note file (markdown) and extracts anchors and references.
func ParseNote(path, content string) (Note, error) {
	fm := extractFrontmatter(content)

	if fm == nil {
		return Note{
			Path:  path,
			Title: titleFromPath(path),
		}, nil
	}

	var anchors []Anchor
	var err error
	// Prefer new code-anchors syntax when present.
	if len(fm.CodeAnchors) > 0 {
		anchors, err = buildAnchorsNew(fm.CodeAnchors)
	} else {
		anchors, err = buildAnchors(fm.Anchors)
	}
	if err != nil {
		return Note{}, err
	}

	title := fm.Title
	if title == "" {
		title = titleFromPath(path)
	}

	return Note{
		Path:           path,
		Title:          title,
		DefinedAnchors: anchors,
	}, nil
}

func extractFrontmatter(content string) *Frontmatter {
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, "---") {
		return nil
	}
	parts := strings.SplitN(trimmed, "---", 3)
	if len(parts) < 3 {
		return nil
	}
	raw := strings.TrimSpace(parts[1])
	var fm Frontmatter
	if err := yaml.Unmarshal([]byte(raw), &fm); err != nil {
		// Gracefully degrade: treat invalid YAML as "no frontmatter" rather than failing.
		// This allows notes with malformed frontmatter to still be indexed for content.
		return nil
	}
	return &fm
}

func titleFromPath(path string) string {
	// Handle both POSIX and Windows separators for portable titles.
	path = strings.ReplaceAll(path, "\\", "/")
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func buildAnchors(defs []AnchorDefinition) ([]Anchor, error) {
	var anchors []Anchor
	for i, def := range defs {
		anchor, err := anchorFromDefinition(def)
		if err != nil {
			return nil, fmt.Errorf("anchor %d: %w", i, err)
		}
		if anchor.Label == "" {
			return nil, fmt.Errorf("anchor %d: label is required", i)
		}
		if anchor.Kind == "" && anchor.BaseSym == nil && anchor.Ann == nil {
			return nil, fmt.Errorf("anchor %q: must specify kind, baseSymbol/baseClass, annotation, or symbol", anchor.Label)
		}
		anchors = append(anchors, anchor)
	}
	return anchors, nil
}

func buildAnchorsNew(byLang map[string][]AnchorSpec) ([]Anchor, error) {
	var anchors []Anchor
	for langKey, specs := range byLang {
		lang := normalizeLang(langKey)
		for i, spec := range specs {
			inline, err := resolveInlineSpec(spec)
			if err != nil {
				return nil, fmt.Errorf("anchor %d (%s): %w", i, langKey, err)
			}
			anchor, err := anchorFromInlineSpec(lang, inline)
			if err != nil {
				return nil, fmt.Errorf("anchor %d (%s): %w", i, langKey, err)
			}
			anchors = append(anchors, anchor)
		}
	}
	return anchors, nil
}

func resolveInlineSpec(spec AnchorSpec) (AnchorInlineSpec, error) {
	// Mapping-style spec already populated.
	if strings.TrimSpace(spec.Raw) == "" {
		return spec.AnchorInlineSpec, nil
	}

	raw := strings.TrimSpace(spec.Raw)
	if raw == "" {
		return AnchorInlineSpec{}, fmt.Errorf("empty anchor spec")
	}

	// Allow optional prefix ("kind:value"). Default is symbol matching.
	kind := ""
	value := raw
	if idx := strings.Index(raw, ":"); idx > 0 {
		prefix := strings.ToLower(strings.TrimSpace(raw[:idx]))
		rest := strings.TrimSpace(raw[idx+1:])
		switch prefix {
		case "symbol", "calls", "baseclass", "base_class", "extends", "decorator", "annotation", "glob", "dir":
			kind = prefix
			value = rest
		}
	}

	if value == "" {
		return AnchorInlineSpec{}, fmt.Errorf("empty anchor target")
	}

	switch kind {
	case "", "symbol":
		return AnchorInlineSpec{Symbol: value}, nil
	case "calls":
		return AnchorInlineSpec{Calls: value}, nil
	case "baseclass", "base_class", "extends":
		return AnchorInlineSpec{BaseClass: value}, nil
	case "decorator", "annotation":
		return AnchorInlineSpec{Decorator: value}, nil
	case "glob":
		return AnchorInlineSpec{Glob: value}, nil
	case "dir":
		return AnchorInlineSpec{Dir: value}, nil
	default:
		// Should be unreachable given the switch above; treat as default symbol.
		return AnchorInlineSpec{Symbol: value}, nil
	}
}

// normalizeLang converts user-friendly language names to internal constants.
func normalizeLang(s string) Lang {
	switch strings.ToLower(s) {
	case "python", "py":
		return LangPy
	case "kotlin", "kt":
		return LangKotlin
	case "go", "golang":
		return LangGo
	case "java":
		return LangJava
	case "typescript", "ts", "javascript", "js":
		return LangTS
	case "csharp", "cs", "c#":
		return LangCs
	case "swift":
		return LangSwift
	case "php":
		return LangPhp
	default:
		return Lang(s)
	}
}

func anchorFromInlineSpec(lang Lang, spec AnchorInlineSpec) (Anchor, error) {
	// Determine which field is set.
	setCount := 0
	if spec.Ref != "" {
		setCount++
	}
	if spec.Symbol != "" {
		setCount++
	}
	if spec.Calls != "" {
		setCount++
	}
	if spec.BaseClass != "" {
		setCount++
	}
	if spec.Decorator != "" {
		setCount++
	}
	if spec.Dir != "" {
		setCount++
	}
	if spec.Glob != "" {
		setCount++
	}
	if len(spec.Globs) > 0 {
		setCount++
	}
	if setCount == 0 {
		return Anchor{}, fmt.Errorf("missing ref/symbol/calls/baseClass/decorator/dir/glob/globs")
	}
	if setCount > 1 {
		return Anchor{}, fmt.Errorf("only one of ref/symbol/calls/baseClass/decorator/dir/glob/globs may be set")
	}

	var sym SymbolRef
	var kind AnchorKind
	switch {
	case spec.Ref != "":
		parsed, err := resolveInlineSpec(AnchorSpec{Raw: spec.Ref})
		if err != nil {
			return Anchor{}, err
		}
		// Preserve label/args when using ref:.
		parsed.Label = spec.Label
		if len(spec.Args) > 0 {
			parsed.Args = spec.Args
		}
		return anchorFromInlineSpec(lang, parsed)
	case spec.Symbol != "":
		s, err := parseSymbolSpec(spec.Symbol, lang)
		if err != nil {
			return Anchor{}, err
		}
		if s.Pkg == "" {
			return Anchor{}, fmt.Errorf("symbol must be fully-qualified (pkg.Name); got %q", spec.Symbol)
		}
		sym = s
		kind = AnchorFunc // symbol: matches definition + references
	case spec.Calls != "":
		s, err := parseSymbolSpec(spec.Calls, lang)
		if err != nil {
			return Anchor{}, err
		}
		if s.Pkg == "" {
			return Anchor{}, fmt.Errorf("calls must be fully-qualified (pkg.Name); got %q", spec.Calls)
		}
		sym = s
		// calls: is a legacy alias; function anchors match both definitions and call sites.
		kind = AnchorFunc
	case spec.BaseClass != "":
		s, err := parseSymbolSpec(spec.BaseClass, lang)
		if err != nil {
			return Anchor{}, err
		}
		if s.Pkg == "" {
			return Anchor{}, fmt.Errorf("baseClass must be fully-qualified (pkg.Name); got %q", spec.BaseClass)
		}
		sym = s
		kind = AnchorBaseClass
	case spec.Decorator != "":
		s, err := parseSymbolSpec(spec.Decorator, lang)
		if err != nil {
			return Anchor{}, err
		}
		label := spec.Label
		if label == "" {
			label = s.Name
		}
		return Anchor{
			Label: label,
			Kind:  AnchorAnnotation,
			Lang:  s.Lang,
			Ann: &AnnotationSelector{
				Symbol:     s,
				ArgFilters: spec.Args,
			},
		}, nil
	case spec.Dir != "" || spec.Glob != "" || len(spec.Globs) > 0:
		// dir: is a legacy alias for glob matching; directory expansion is handled
		// later when ingesting the note (we may not have filesystem context here).
		var raw []string
		if spec.Dir != "" {
			raw = append(raw, spec.Dir)
		} else if spec.Glob != "" {
			raw = append(raw, spec.Glob)
		} else {
			raw = append(raw, spec.Globs...)
		}
		var globs []string
		for _, g := range raw {
			g = strings.TrimSpace(g)
			if g == "" {
				continue
			}
			globs = append(globs, g)
		}
		if len(globs) == 0 {
			return Anchor{}, fmt.Errorf("empty glob")
		}
		label := spec.Label
		if label == "" {
			label = globs[0]
		}
		return Anchor{
			Label: label,
			Kind:  AnchorGlob,
			Lang:  lang,
			Globs: globs,
		}, nil
	default:
		return Anchor{}, fmt.Errorf("missing ref/symbol/calls/baseClass/decorator/dir/glob/globs")
	}

	label := spec.Label
	if label == "" {
		label = sym.Name
	}

	return Anchor{
		Label:   label,
		Kind:    kind,
		Lang:    sym.Lang,
		BaseSym: &sym,
	}, nil
}

func anchorFromDefinition(def AnchorDefinition) (Anchor, error) {
	// Shorthand forms take precedence if provided.
	if def.BaseClass != "" {
		sym, err := parseSymbolSpec(def.BaseClass, def.Define.Lang)
		if err != nil {
			return Anchor{}, err
		}
		if sym.Pkg == "" {
			return Anchor{}, fmt.Errorf("baseClass must be fully-qualified (pkg.Name); got %q", def.BaseClass)
		}
		label := def.Define.Label
		if label == "" {
			label = sym.Name
		}
		return Anchor{
			Label:   label,
			Kind:    AnchorBaseClass,
			Lang:    sym.Lang,
			BaseSym: &sym,
		}, nil
	}
	if def.Annotation != "" {
		sym, err := parseSymbolSpec(def.Annotation, def.Define.Lang)
		if err != nil {
			return Anchor{}, err
		}
		label := def.Define.Label
		if label == "" {
			label = sym.Name
		}
		return Anchor{
			Label: label,
			Kind:  AnchorAnnotation,
			Lang:  sym.Lang,
			Ann: &AnnotationSelector{
				Symbol: sym,
			},
		}, nil
	}
	if def.FunctionUse != "" {
		sym, err := parseSymbolSpec(def.FunctionUse, def.Define.Lang)
		if err != nil {
			return Anchor{}, err
		}
		if sym.Pkg == "" {
			return Anchor{}, fmt.Errorf("functionUse must be fully-qualified (pkg.Name); got %q", def.FunctionUse)
		}
		label := def.Define.Label
		if label == "" {
			label = sym.Name
		}
		return Anchor{
			Label:   label,
			Kind:    AnchorFunc,
			Lang:    sym.Lang,
			BaseSym: &sym,
		}, nil
	}

	// Original verbose form.
	label := def.Define.Label
	kind := def.Define.Kind
	if kind == "" {
		kind = AnchorBaseClass
	}
	anchor := Anchor{
		Label:   label,
		Kind:    kind,
		Lang:    def.Define.Lang,
		BaseSym: def.Define.BaseSym,
		Ann:     def.Define.Ann,
	}
	if anchor.Ann != nil && anchor.Ann.Symbol.Lang == "" {
		anchor.Ann.Symbol.Lang = anchor.Lang
	}
	if anchor.BaseSym != nil && anchor.BaseSym.Lang == "" {
		anchor.BaseSym.Lang = anchor.Lang
	}
	if (anchor.Kind == AnchorBaseClass || anchor.Kind == AnchorFunc) && anchor.BaseSym != nil && anchor.BaseSym.Pkg == "" {
		return Anchor{}, fmt.Errorf("%s anchors require a non-empty pkg (fully-qualified pkg.Name)", anchor.Kind)
	}
	return anchor, nil
}

func parseSymbolSpec(spec string, defaultLang Lang) (SymbolRef, error) {
	if spec == "" {
		return SymbolRef{}, fmt.Errorf("empty symbol spec")
	}
	lang := defaultLang
	rest := spec
	// Only consume a `lang:` prefix when the part before the first colon is a
	// recognized language tag. This avoids stripping the `::` method separator
	// in PHP-style FQNs (e.g. `Polyglot\Todo\Svc::method`).
	if idx := strings.Index(spec, ":"); idx > 0 {
		head := spec[:idx]
		if isKnownLangTag(head) {
			lang = normalizeLang(head)
			rest = spec[idx+1:]
		}
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return SymbolRef{}, fmt.Errorf("invalid symbol spec %q", spec)
	}
	pkg := ""
	name := rest
	member := false
	if lang == LangPhp {
		// PHP FQNs use `\` for namespaces and `::` for methods/static members.
		// Split on the last occurrence of either separator; preserve which
		// separator was used so the resolved SymbolRef can be re-emitted in
		// the same form by normalizeSymbol.
		pkg, name, member = splitPhpSymbol(rest)
	} else if dot := strings.LastIndex(rest, "."); dot >= 0 {
		pkg = rest[:dot]
		name = rest[dot+1:]
	}
	if name == "" {
		return SymbolRef{}, fmt.Errorf("invalid symbol spec %q", spec)
	}
	// Note: Empty pkg is only supported for annotation anchors (decorators) as a wildcard.
	// For symbol/calls/baseClass, parsing/validation should reject empty pkg to avoid
	// ambiguous matching and silently-empty scopes.
	return SymbolRef{Lang: lang, Pkg: pkg, Name: name, Member: member}, nil
}

// isKnownLangTag reports whether head (the part before the first colon in a
// symbol spec) matches a known language tag recognized by normalizeLang.
func isKnownLangTag(head string) bool {
	switch strings.ToLower(head) {
	case "python", "py", "kotlin", "kt", "go", "golang", "java",
		"typescript", "ts", "javascript", "js", "csharp", "cs", "c#",
		"swift", "php":
		return true
	}
	return false
}

// splitPhpSymbol splits a PHP-style FQN into (pkg, name, member) using the last
// separator (`::` preferred for method/static splits, otherwise `\`). The member
// return value reports whether `::` was the separator — i.e. whether the symbol
// refers to a class member rather than a top-level namespace symbol.
func splitPhpSymbol(s string) (string, string, bool) {
	if i := strings.LastIndex(s, "::"); i >= 0 {
		return s[:i], s[i+2:], true
	}
	if i := strings.LastIndex(s, `\`); i >= 0 {
		return s[:i], s[i+1:], false
	}
	return "", s, false
}
