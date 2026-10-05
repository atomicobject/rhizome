package validate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

type namespaceFootprintFile struct {
	path   string
	exists bool
	hash   string
	mode   uint32
}

type namespaceDirectoryEntries struct {
	exact  map[string]bool
	folded map[string][]string
	err    error
}

// Listings belong to one observation only. Each file still gets a fresh Lstat;
// directory-entry metadata is not a current file witness on every platform.
type namespaceFootprintObserver struct {
	root        string
	directories map[string]namespaceDirectoryEntries
}

// Canonical caseless matching covers Unicode case and normalization aliases.
// This key selects candidates only; exact directory spelling and SameFile still
// own admission, including when a broader fold groups physically distinct names.
func namespaceCaseCandidateKey(name string) string {
	return norm.NFD.String(cases.Fold().String(norm.NFD.String(name)))
}

func (o *namespaceFootprintObserver) directory(abs string) namespaceDirectoryEntries {
	if entries, ok := o.directories[abs]; ok {
		return entries
	}
	entries := namespaceDirectoryEntries{exact: make(map[string]bool), folded: make(map[string][]string)}
	listing, err := os.ReadDir(abs)
	entries.err = err
	for _, entry := range listing {
		name := entry.Name()
		entries.exact[name] = true
		key := namespaceCaseCandidateKey(name)
		entries.folded[key] = append(entries.folded[key], name)
	}
	o.directories[abs] = entries
	return entries
}

// Existing confinement resolves parent symlinks first. Require exact spelling
// of those resolved directory entries, rather than emitting a phantom parent
// key when later activity changes a directory's case.
func (o *namespaceFootprintObserver) exactParent(abs string) (bool, error) {
	rel, err := filepath.Rel(o.root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false, fmt.Errorf("native refresh parent escapes vault: %s", abs)
	}
	if rel == "." {
		return true, nil
	}
	current := o.root
	for _, name := range strings.Split(rel, string(filepath.Separator)) {
		entries := o.directory(current)
		if entries.err != nil {
			return false, entries.err
		}
		current = filepath.Join(current, name)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if !entries.exact[name] {
			return false, fmt.Errorf("native refresh parent spelling is not current: %s", current)
		}
		if !info.IsDir() {
			return false, fmt.Errorf("native refresh parent is not a direct directory: %s", current)
		}
	}
	return true, nil
}

func (o *namespaceFootprintObserver) file(runCtx RunContext, rel string) (namespaceFootprintFile, error) {
	abs, err := repairAbsPath(runCtx, rel)
	if err != nil {
		return namespaceFootprintFile{}, err
	}
	present, err := o.exactParent(filepath.Dir(abs))
	if err != nil || !present {
		return namespaceFootprintFile{path: rel}, err
	}
	entries := o.directory(filepath.Dir(abs))
	if entries.err != nil {
		return namespaceFootprintFile{}, entries.err
	}
	info, err := os.Lstat(abs)
	if os.IsNotExist(err) {
		return namespaceFootprintFile{path: rel}, nil
	}
	if err != nil {
		return namespaceFootprintFile{}, err
	}
	if !info.Mode().IsRegular() {
		return namespaceFootprintFile{}, fmt.Errorf("native refresh footprint is not regular: %s", rel)
	}
	name := filepath.Base(abs)
	if !entries.exact[name] {
		var matched string
		// Candidate lookup is host-independent; SameFile below proves whether
		// this filesystem actually resolves the requested spelling as an alias.
		for _, candidate := range entries.folded[namespaceCaseCandidateKey(name)] {
			candidateInfo, err := os.Lstat(filepath.Join(filepath.Dir(abs), candidate))
			if err != nil {
				return namespaceFootprintFile{}, err
			}
			if !os.SameFile(info, candidateInfo) {
				continue
			}
			if matched != "" {
				return namespaceFootprintFile{}, fmt.Errorf("native refresh path has ambiguous current spelling: %s", rel)
			}
			matched = candidate
		}
		if matched == "" {
			return namespaceFootprintFile{}, fmt.Errorf("native refresh path has no exact current entry: %s", rel)
		}
		abs = filepath.Join(filepath.Dir(abs), matched)
		// Read the observed entry's own current witness, including Windows mode.
		info, err = os.Lstat(abs)
		if err != nil {
			return namespaceFootprintFile{}, err
		}
		if !info.Mode().IsRegular() {
			return namespaceFootprintFile{}, fmt.Errorf("native refresh current entry is not regular: %s", rel)
		}
	}
	currentRel, err := filepath.Rel(o.root, abs)
	if err != nil || namespaceInternalPath(filepath.ToSlash(currentRel)) {
		return namespaceFootprintFile{}, fmt.Errorf("native refresh path resolves to internal storage: %s", rel)
	}
	content, err := os.ReadFile(abs)
	if err != nil {
		return namespaceFootprintFile{}, err
	}
	return namespaceFootprintFile{path: filepath.ToSlash(currentRel), exists: true, hash: SourceHash(content), mode: repairModeBits(info.Mode())}, nil
}

func snapshotNamespaceFootprint(runCtx RunContext, manifest repairJournalManifest) ([]namespaceFootprintFile, []string, []string, error) {
	vaultPaths, err := paths.NewVaultPaths(runCtx.VaultPath)
	if err != nil {
		return nil, nil, nil, err
	}
	observer := namespaceFootprintObserver{root: filepath.FromSlash(vaultPaths.Root()), directories: make(map[string]namespaceDirectoryEntries)}
	records := make(map[string]namespaceFootprintFile)
	var changed, deleted []string
	for _, rel := range repairManifestPostApplyPaths(manifest) {
		file, err := observer.file(runCtx, rel)
		if err != nil {
			return nil, nil, nil, err
		}
		if !file.exists || file.path != rel {
			records[rel] = namespaceFootprintFile{path: rel}
			deleted = append(deleted, rel)
		}
		if file.exists {
			records[file.path] = file
			changed = append(changed, file.path)
		}
	}
	var files []namespaceFootprintFile
	keys := make([]string, 0, len(records))
	for path := range records {
		keys = append(keys, path)
	}
	for _, path := range sortedUnique(keys) {
		files = append(files, records[path])
	}
	return files, sortedUnique(changed), sortedUnique(deleted), nil
}
