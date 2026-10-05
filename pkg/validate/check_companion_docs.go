package validate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/paths"
)

type companionDocUse struct {
	Owner string
	Path  string
}

// RunCompanionDocs validates ontology @companionDocs paths against the vault
// root. These paths feed authoring-guide retrieval, so broken paths otherwise
// stay silent until an agent needs the companion context.
func RunCompanionDocs(runCtx RunContext) CheckResult {
	result := CheckResult{Name: CheckCompanionDocs, OK: true}
	schema, err := ontology.LoadSchema(runCtx.VaultPath)
	if errors.Is(err, ontology.ErrNoOntologyFiles) {
		result.Skipped = true
		result.Summary = "no ontology schema"
		return result
	}
	if err != nil {
		result.OK = false
		result.Error = err.Error()
		return result
	}

	uses := collectCompanionDocUses(schema)
	if len(uses) == 0 {
		result.Skipped = true
		result.Summary = "no companion docs declared"
		return result
	}

	vaultPaths, err := paths.NewVaultPaths(runCtx.VaultPath)
	if err != nil {
		result.OK = false
		result.Error = err.Error()
		return result
	}

	issues := make([]Issue, 0)
	seen := map[string]struct{}{}
	for _, use := range uses {
		key := use.Owner + "\x00" + use.Path
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		rel, relErr := vaultPaths.RelStrict(use.Path)
		if relErr != nil || strings.TrimSpace(rel.String()) == "" {
			issues = append(issues, Issue{
				Code:    "companion_doc_outside_vault",
				Source:  use.Owner,
				Target:  use.Path,
				Message: fmt.Sprintf("%s companion doc path is outside the vault", use.Owner),
			})
			continue
		}
		abs, absErr := vaultPaths.Abs(rel)
		if absErr != nil || strings.TrimSpace(abs.String()) == "" {
			issues = append(issues, Issue{
				Code:    "companion_doc_invalid_path",
				Source:  use.Owner,
				Target:  use.Path,
				Message: fmt.Sprintf("%s companion doc path could not be resolved", use.Owner),
			})
			continue
		}
		info, statErr := os.Stat(abs.String())
		if statErr != nil {
			code := "companion_doc_missing"
			if !errors.Is(statErr, os.ErrNotExist) {
				code = "companion_doc_stat_failed"
			}
			issues = append(issues, Issue{
				Code:    code,
				Path:    rel.String(),
				Source:  use.Owner,
				Target:  use.Path,
				Message: fmt.Sprintf("%s companion doc does not resolve: %v", use.Owner, statErr),
			})
			continue
		}
		if info.IsDir() {
			issues = append(issues, Issue{
				Code:    "companion_doc_directory",
				Path:    rel.String(),
				Source:  use.Owner,
				Target:  use.Path,
				Message: fmt.Sprintf("%s companion doc points to a directory, not a file", use.Owner),
			})
		}
	}

	result.IssueCount = len(issues)
	if len(issues) == 0 {
		result.Summary = fmt.Sprintf("%d companion doc path(s) checked", len(seen))
		return result
	}
	result.OK = false
	result.Summary = fmt.Sprintf("%d companion doc issue(s)", len(issues))
	result.Issues = append(result.Issues, issues...)
	return result
}

func collectCompanionDocUses(schema *ontology.Schema) []companionDocUse {
	if schema == nil {
		return nil
	}
	var out []companionDocUse
	for _, name := range sortedNoteTypeNames(schema.Types) {
		noteType := schema.Types[name]
		out = appendCompanionDocUses(out, name, noteType.CompanionDocs)
		for _, field := range noteType.Fields {
			out = appendCompanionDocUses(out, name+"."+field.Name, field.CompanionDocs)
		}
	}
	for _, name := range sortedInterfaceNames(schema.Interfaces) {
		iface := schema.Interfaces[name]
		out = appendCompanionDocUses(out, name, iface.CompanionDocs)
		for _, field := range iface.Fields {
			out = appendCompanionDocUses(out, name+"."+field.Name, field.CompanionDocs)
		}
	}
	return out
}

func appendCompanionDocUses(out []companionDocUse, owner string, docs []ontology.CompanionDocRef) []companionDocUse {
	for _, doc := range docs {
		path := strings.TrimSpace(doc.Path)
		if path == "" {
			out = append(out, companionDocUse{Owner: owner})
			continue
		}
		out = append(out, companionDocUse{Owner: owner, Path: filepath.ToSlash(path)})
	}
	return out
}

func sortedNoteTypeNames(types map[string]*ontology.NoteType) []string {
	names := make([]string, 0, len(types))
	for name := range types {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedInterfaceNames(types map[string]*ontology.InterfaceType) []string {
	names := make([]string, 0, len(types))
	for name := range types {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
