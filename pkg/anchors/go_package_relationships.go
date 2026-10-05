package codeanchor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const GoRelationshipAnalyzerVersion = "v2"

type GoPackageKey struct {
	ImportPath   string `json:"importPath"`
	Directory    string `json:"directory"`
	PackageName  string `json:"packageName"`
	BuildVariant string `json:"buildVariant"`
}

func (k GoPackageKey) String() string {
	return strings.Join([]string{k.ImportPath, k.Directory, k.PackageName, k.BuildVariant}, "\x00")
}

func (k GoPackageKey) StorageKey() string {
	digest := sha256.Sum256([]byte(k.String()))
	return hex.EncodeToString(digest[:])
}

func CurrentGoBuildVariant() string {
	return runtime.GOOS + "/" + runtime.GOARCH + "/with-tests"
}

type GoPackageSource struct {
	Path        string
	Hash        string
	Content     []byte
	ParseStatus ParseStatus
}

type GoPackageMember struct {
	Path        string
	Hash        string
	ParseStatus ParseStatus
	PackageKey  string
}

type GoPackageSnapshot struct {
	Package          GoPackageKey
	MembershipDigest string
	Membership       []GoPackageMember
	Sources          []GoPackageSource
}

type GoRelationshipKind string

const (
	GoRelationshipCalls      GoRelationshipKind = "calls"
	GoRelationshipImplements GoRelationshipKind = "implements"
)

type GoDerivedRelationship struct {
	Kind        GoRelationshipKind
	SourcePath  string
	SourceFQN   string
	TargetFQN   string
	PointerOnly bool
}

type GoRelationshipDiagnostic struct {
	Code   string
	Path   string
	Detail string
}

type GoPackageRelationshipAnalysis struct {
	Package          GoPackageKey
	MembershipDigest string
	AnalyzerVersion  string
	Complete         bool
	Diagnostics      []GoRelationshipDiagnostic
	Relationships    []GoDerivedRelationship
}

type GoPackageRelationshipReplacement struct {
	Package          GoPackageKey
	MembershipDigest string
	AnalyzerVersion  string
	Complete         bool
	Diagnostics      []GoRelationshipDiagnostic
	Relationships    []GoDerivedRelationship
	Remove           bool
}

// GoPackageMembershipDigest binds analysis to the complete package snapshot.
// Callers must include every sibling selected for this package/build variant.
func GoPackageMembershipDigest(key GoPackageKey, members []GoPackageMember) string {
	ordered := append([]GoPackageMember(nil), members...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Path < ordered[j].Path })
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s\n", key.String())
	for _, source := range ordered {
		_, _ = fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\n", source.Path, source.Hash, source.ParseStatus, source.PackageKey)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// AnalyzeGoPackageRelationships resolves same-package calls and implicit
// implementations with go/types. Imports are deliberately unavailable except
// unsafe, so analysis never starts a subprocess or reads a module cache.
func AnalyzeGoPackageRelationships(ctx context.Context, snapshot GoPackageSnapshot) (GoPackageRelationshipAnalysis, error) {
	result := GoPackageRelationshipAnalysis{
		Package:          snapshot.Package,
		MembershipDigest: snapshot.MembershipDigest,
		AnalyzerVersion:  GoRelationshipAnalyzerVersion,
		Complete:         true,
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if strings.TrimSpace(snapshot.Package.ImportPath) == "" || strings.TrimSpace(snapshot.Package.PackageName) == "" || strings.TrimSpace(snapshot.Package.Directory) == "" {
		return result, fmt.Errorf("Go package identity requires import path, directory, and package name")
	}
	membership := snapshot.Membership
	if len(membership) == 0 {
		membership = make([]GoPackageMember, 0, len(snapshot.Sources))
		for _, source := range snapshot.Sources {
			membership = append(membership, GoPackageMember{Path: source.Path, Hash: source.Hash, ParseStatus: source.ParseStatus, PackageKey: snapshot.Package.StorageKey()})
		}
	}
	expectedDigest := GoPackageMembershipDigest(snapshot.Package, membership)
	if snapshot.MembershipDigest == "" || snapshot.MembershipDigest != expectedDigest {
		return result, fmt.Errorf("Go package membership digest mismatch")
	}
	memberByPath := make(map[string]GoPackageMember, len(membership))
	for _, member := range membership {
		if strings.TrimSpace(member.Path) == "" {
			return result, fmt.Errorf("Go package membership contains an empty path")
		}
		if _, duplicate := memberByPath[member.Path]; duplicate {
			return result, fmt.Errorf("Go package membership contains duplicate path %s", member.Path)
		}
		memberByPath[member.Path] = member
	}
	sourceByPath := make(map[string]GoPackageSource, len(snapshot.Sources))
	for _, source := range snapshot.Sources {
		if _, duplicate := sourceByPath[source.Path]; duplicate {
			return result, fmt.Errorf("Go package sources contain duplicate path %s", source.Path)
		}
		member, exists := memberByPath[source.Path]
		if !exists {
			return result, fmt.Errorf("Go package source %s is absent from membership", source.Path)
		}
		if member.Hash != source.Hash || member.ParseStatus != source.ParseStatus {
			return result, fmt.Errorf("Go package source metadata mismatch for %s", source.Path)
		}
		if member.PackageKey != snapshot.Package.StorageKey() {
			return result, fmt.Errorf("Go package source %s has mismatched package identity", source.Path)
		}
		sourceByPath[source.Path] = source
	}
	for _, member := range membership {
		switch member.PackageKey {
		case snapshot.Package.StorageKey():
			if _, exists := sourceByPath[member.Path]; !exists {
				result.Complete = false
				result.Diagnostics = append(result.Diagnostics, GoRelationshipDiagnostic{Code: "source_missing", Path: member.Path})
			}
		case "":
			// An unclassified Go sibling can contain declarations or methods for
			// this package. Absence cannot be used as complete method-set proof.
			result.Complete = false
			result.Diagnostics = append(result.Diagnostics, GoRelationshipDiagnostic{Code: "source_package_unclassified", Path: member.Path, Detail: string(member.ParseStatus)})
		}
	}
	if !result.Complete {
		sortGoRelationshipDiagnostics(result.Diagnostics)
		return result, nil
	}

	fset := token.NewFileSet()
	files := make([]*ast.File, 0, len(snapshot.Sources))
	pathByFile := make(map[*ast.File]string, len(snapshot.Sources))
	pathByPos := make(map[string]string, len(snapshot.Sources))
	for _, source := range snapshot.Sources {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		actualHash := sha256.Sum256(source.Content)
		if source.Hash == "" || !strings.EqualFold(source.Hash, hex.EncodeToString(actualHash[:])) {
			return result, fmt.Errorf("Go package source hash mismatch for %s", source.Path)
		}
		matches, matchErr := goSourceMatchesActiveBuild(source, sourceByPath)
		if matchErr != nil {
			result.Complete = false
			result.Diagnostics = append(result.Diagnostics, GoRelationshipDiagnostic{Code: "source_build_constraint_error", Path: source.Path, Detail: matchErr.Error()})
			continue
		}
		if !matches {
			continue
		}
		if source.ParseStatus != "" && source.ParseStatus != ParseOK {
			result.Complete = false
			result.Diagnostics = append(result.Diagnostics, GoRelationshipDiagnostic{Code: "source_parse_untrusted", Path: source.Path, Detail: string(source.ParseStatus)})
			continue
		}
		file, err := parser.ParseFile(fset, source.Path, source.Content, parser.AllErrors)
		if err != nil || file == nil {
			result.Complete = false
			result.Diagnostics = append(result.Diagnostics, GoRelationshipDiagnostic{Code: "source_parse_error", Path: source.Path, Detail: errorText(err)})
			continue
		}
		if file.Name == nil || file.Name.Name != snapshot.Package.PackageName {
			return result, fmt.Errorf("Go package name mismatch for %s: got %q want %q", source.Path, file.Name.Name, snapshot.Package.PackageName)
		}
		files = append(files, file)
		pathByFile[file] = source.Path
		pathByPos[fset.Position(file.Package).Filename] = source.Path
	}
	if len(files) == 0 || !result.Complete {
		return result, nil
	}

	info := &types.Info{
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
	}
	importer := &rejectingGoImporter{unavailable: make(map[string]struct{})}
	var typeErrors []error
	conf := types.Config{
		Importer: importer,
		Error: func(err error) {
			typeErrors = append(typeErrors, err)
		},
	}
	pkg, checkErr := conf.Check(snapshot.Package.ImportPath, fset, files, info)
	if pkg == nil {
		result.Complete = false
		result.Diagnostics = append(result.Diagnostics, GoRelationshipDiagnostic{Code: "type_check_failed", Detail: errorText(checkErr)})
		return result, nil
	}
	for path := range importer.unavailable {
		result.Diagnostics = append(result.Diagnostics, GoRelationshipDiagnostic{Code: "dependency_unavailable", Detail: path})
	}
	for _, err := range typeErrors {
		result.Diagnostics = append(result.Diagnostics, GoRelationshipDiagnostic{Code: "type_check_error", Path: typeErrorPath(fset, err, pathByPos), Detail: err.Error()})
	}
	result.Complete = checkErr == nil

	for _, file := range files {
		path := pathByFile[file]
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || fn.Name == nil {
				continue
			}
			owner, _ := info.Defs[fn.Name].(*types.Func)
			ownerFQN, ok := goObjectFQN(owner)
			if !ok || owner.Pkg() != pkg {
				continue
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				if ctx.Err() != nil {
					return false
				}
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				target := goCalledObject(call.Fun, info)
				targetFQN, exact := goObjectFQN(target)
				if !exact || !goLocalCallTargetDeclaredInSnapshot(target, pkg, fset, pathByPos) {
					return true
				}
				result.Relationships = append(result.Relationships, GoDerivedRelationship{Kind: GoRelationshipCalls, SourcePath: path, SourceFQN: ownerFQN, TargetFQN: targetFQN})
				return true
			})
		}
	}

	// Missing or untrusted siblings make method-set absence unknowable. Imports
	// may be unavailable, but go/types can still prove a local implementation
	// when every required method and its matched signature are complete.
	for _, name := range pkg.Scope().Names() {
		obj, ok := pkg.Scope().Lookup(name).(*types.TypeName)
		if !ok {
			continue
		}
		iface, ok := obj.Type().Underlying().(*types.Interface)
		if !ok || !goInterfaceProofComplete(iface) {
			continue
		}
		iface.Complete()
		for _, candidateName := range pkg.Scope().Names() {
			candidate, ok := pkg.Scope().Lookup(candidateName).(*types.TypeName)
			if !ok || candidate == obj {
				continue
			}
			named, ok := types.Unalias(candidate.Type()).(*types.Named)
			if !ok {
				continue
			}
			valueImplements := types.Implements(named, iface)
			pointerImplements := types.Implements(types.NewPointer(named), iface)
			if !valueImplements && !pointerImplements {
				continue
			}
			methodSet := types.NewMethodSet(named)
			if !valueImplements {
				methodSet = types.NewMethodSet(types.NewPointer(named))
			}
			if !goImplementationProofComplete(methodSet, iface) {
				continue
			}
			sourcePath := pathForPosition(fset, candidate.Pos(), pathByPos)
			result.Relationships = append(result.Relationships, GoDerivedRelationship{
				Kind: GoRelationshipImplements, SourcePath: sourcePath, SourceFQN: snapshot.Package.ImportPath + "." + candidate.Name(), TargetFQN: snapshot.Package.ImportPath + "." + obj.Name(), PointerOnly: !valueImplements,
			})
		}
	}

	result.Relationships = dedupeGoRelationships(result.Relationships)
	sortGoRelationshipDiagnostics(result.Diagnostics)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	return result, nil
}

func goSourceMatchesActiveBuild(source GoPackageSource, sources map[string]GoPackageSource) (bool, error) {
	buildContext := build.Default
	buildContext.OpenFile = func(name string) (io.ReadCloser, error) {
		normalized := filepath.ToSlash(filepath.Clean(name))
		candidate, ok := sources[normalized]
		if !ok {
			return nil, fmt.Errorf("build constraint source %s is unavailable", normalized)
		}
		return io.NopCloser(bytes.NewReader(candidate.Content)), nil
	}
	directory, name := filepath.Split(filepath.FromSlash(source.Path))
	directory = strings.TrimSuffix(directory, string(filepath.Separator))
	if directory == "" {
		directory = "."
	}
	return buildContext.MatchFile(directory, name)
}

// goLocalCallTargetDeclaredInSnapshot proves call identity without requiring
// every parameter and result type to be available. An unavailable standard or
// external dependency can make a local function signature incomplete while
// its declaration, receiver selection, package, and canonical FQN remain exact.
// Interface implementation proof retains the stricter full-signature check.
func goLocalCallTargetDeclaredInSnapshot(target types.Object, pkg *types.Package, fset *token.FileSet, pathByPos map[string]string) bool {
	fn, ok := target.(*types.Func)
	if !ok || fn.Pkg() != pkg || !fn.Pos().IsValid() {
		return false
	}
	filename := fset.Position(fn.Pos()).Filename
	_, trusted := pathByPos[filename]
	return trusted
}

func sortGoRelationshipDiagnostics(diagnostics []GoRelationshipDiagnostic) {
	sort.Slice(diagnostics, func(i, j int) bool {
		if diagnostics[i].Code != diagnostics[j].Code {
			return diagnostics[i].Code < diagnostics[j].Code
		}
		if diagnostics[i].Path != diagnostics[j].Path {
			return diagnostics[i].Path < diagnostics[j].Path
		}
		return diagnostics[i].Detail < diagnostics[j].Detail
	})
}

type rejectingGoImporter struct{ unavailable map[string]struct{} }

func (i *rejectingGoImporter) Import(path string) (*types.Package, error) {
	if path == "unsafe" {
		return types.Unsafe, nil
	}
	i.unavailable[path] = struct{}{}
	return nil, fmt.Errorf("offline Go relationship analysis cannot import %q", path)
}

func goCalledObject(fun ast.Expr, info *types.Info) *types.Func {
	switch expr := fun.(type) {
	case *ast.Ident:
		fn, _ := info.Uses[expr].(*types.Func)
		return fn
	case *ast.SelectorExpr:
		if selection := info.Selections[expr]; selection != nil {
			fn, _ := selection.Obj().(*types.Func)
			return fn
		}
		fn, _ := info.Uses[expr.Sel].(*types.Func)
		return fn
	case *ast.IndexExpr:
		return goCalledObject(expr.X, info)
	case *ast.IndexListExpr:
		return goCalledObject(expr.X, info)
	default:
		return nil
	}
}

func goObjectFQN(fn *types.Func) (string, bool) {
	if fn == nil || fn.Pkg() == nil || strings.TrimSpace(fn.Pkg().Path()) == "" {
		return "", false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return "", false
	}
	if sig.Recv() == nil {
		return fn.Pkg().Path() + "." + fn.Name(), true
	}
	recv := sig.Recv().Type()
	if pointer, ok := recv.(*types.Pointer); ok {
		recv = pointer.Elem()
	}
	named, ok := recv.(*types.Named)
	if !ok || named.Obj() == nil || named.Obj().Pkg() == nil {
		return "", false
	}
	return named.Obj().Pkg().Path() + "." + named.Obj().Name() + "." + fn.Name(), true
}

func goInterfaceProofComplete(iface *types.Interface) bool {
	if iface == nil {
		return false
	}
	iface.Complete()
	return iface.IsMethodSet() && newGoTypeProof().complete(iface)
}

func goImplementationProofComplete(methods *types.MethodSet, iface *types.Interface) bool {
	proof := newGoTypeProof()
	if !proof.complete(iface) {
		return false
	}
	for i := 0; i < iface.NumMethods(); i++ {
		required := iface.Method(i)
		selection := methods.Lookup(required.Pkg(), required.Name())
		if selection == nil {
			return false
		}
		actual, _ := selection.Obj().(*types.Func)
		if !proof.complete(required.Type()) || !proof.complete(actual.Type()) {
			return false
		}
	}
	return true
}

type goTypeProof struct{ state map[types.Type]uint8 }

func newGoTypeProof() *goTypeProof { return &goTypeProof{state: make(map[types.Type]uint8)} }

func (p *goTypeProof) complete(t types.Type) bool {
	if t == nil {
		return false
	}
	t = types.Unalias(t)
	switch p.state[t] {
	case 1:
		return true // a recursive edge is valid while its enclosing proof remains valid
	case 2:
		return true
	case 3:
		return false
	}
	p.state[t] = 1
	valid := false
	switch typed := t.(type) {
	case *types.Basic:
		valid = typed.Kind() != types.Invalid
	case *types.Pointer:
		valid = p.complete(typed.Elem())
	case *types.Array:
		valid = p.complete(typed.Elem())
	case *types.Slice:
		valid = p.complete(typed.Elem())
	case *types.Map:
		valid = p.complete(typed.Key()) && p.complete(typed.Elem())
	case *types.Chan:
		valid = p.complete(typed.Elem())
	case *types.Tuple:
		valid = true
		for i := 0; i < typed.Len(); i++ {
			if !p.complete(typed.At(i).Type()) {
				valid = false
				break
			}
		}
	case *types.Signature:
		valid = p.complete(typed.Params()) && p.complete(typed.Results())
		if valid && typed.TypeParams() != nil {
			for i := 0; i < typed.TypeParams().Len(); i++ {
				if !p.complete(typed.TypeParams().At(i).Constraint()) {
					valid = false
					break
				}
			}
		}
	case *types.Named:
		// Predeclared named types such as error have no package and remain valid.
		valid = typed.Obj() != nil && p.complete(typed.Underlying())
	case *types.Interface:
		typed.Complete()
		valid = true
		for i := 0; valid && i < typed.NumEmbeddeds(); i++ {
			valid = p.complete(typed.EmbeddedType(i))
		}
		for i := 0; valid && i < typed.NumMethods(); i++ {
			valid = p.complete(typed.Method(i).Type())
		}
	case *types.Struct:
		valid = true
		for i := 0; i < typed.NumFields(); i++ {
			if !p.complete(typed.Field(i).Type()) {
				valid = false
				break
			}
		}
	case *types.TypeParam:
		valid = p.complete(typed.Constraint())
	case *types.Union:
		valid = true
		for i := 0; i < typed.Len(); i++ {
			if !p.complete(typed.Term(i).Type()) {
				valid = false
				break
			}
		}
	}
	if valid {
		p.state[t] = 2
	} else {
		p.state[t] = 3
	}
	return valid
}

func pathForPosition(fset *token.FileSet, pos token.Pos, paths map[string]string) string {
	if pos == token.NoPos {
		return ""
	}
	return paths[fset.Position(pos).Filename]
}

func typeErrorPath(fset *token.FileSet, err error, paths map[string]string) string {
	if typed, ok := err.(types.Error); ok {
		return pathForPosition(fset, typed.Pos, paths)
	}
	return ""
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func dedupeGoRelationships(in []GoDerivedRelationship) []GoDerivedRelationship {
	sort.Slice(in, func(i, j int) bool {
		a, b := in[i], in[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.SourcePath != b.SourcePath {
			return a.SourcePath < b.SourcePath
		}
		if a.SourceFQN != b.SourceFQN {
			return a.SourceFQN < b.SourceFQN
		}
		if a.TargetFQN != b.TargetFQN {
			return a.TargetFQN < b.TargetFQN
		}
		return !a.PointerOnly && b.PointerOnly
	})
	out := make([]GoDerivedRelationship, 0, len(in))
	for _, relationship := range in {
		if len(out) > 0 {
			last := out[len(out)-1]
			if last.Kind == relationship.Kind && last.SourcePath == relationship.SourcePath && last.SourceFQN == relationship.SourceFQN && last.TargetFQN == relationship.TargetFQN {
				continue
			}
		}
		out = append(out, relationship)
	}
	return out
}
