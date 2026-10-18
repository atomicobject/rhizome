package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func copyIsolatedPopulation(source, destination string, excludes []string) (map[string]struct{}, error) {
	source, _ = filepath.Abs(source)
	canonicalSource, err := filepath.EvalSymlinks(source)
	if err != nil {
		return nil, err
	}
	allowed := map[string]struct{}{}
	err = filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return os.MkdirAll(destination, 0o755)
		}
		if excludedPopulationPath(rel, excludes) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		// WalkDir does not descend into a symlinked directory, so staging would
		// silently drop the schema the normal loader follows.
		if entry.Type()&os.ModeSymlink != 0 && authoredRhizomeInput(rel, true) {
			if info, err := os.Stat(path); err == nil && info.IsDir() {
				return fmt.Errorf("symlinked ontology directory is not supported in isolated populations: %s", rel)
			}
		}
		if strings.HasPrefix(rel, ".rhizome/") && !authoredRhizomeInput(rel, entry.IsDir()) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(destination, filepath.FromSlash(rel))
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		readPath := path
		if info.Mode()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				return err
			}
			resolved, _ = filepath.Abs(resolved)
			if resolved != canonicalSource && !strings.HasPrefix(resolved, canonicalSource+string(filepath.Separator)) {
				return fmt.Errorf("symlink escapes source population: %s", rel)
			}
			resolvedRel, err := filepath.Rel(canonicalSource, resolved)
			if err != nil {
				return err
			}
			if excludedPopulationPath(filepath.ToSlash(resolvedRel), excludes) {
				return fmt.Errorf("symlink targets excluded source population: %s", rel)
			}
			readPath = resolved
		}
		in, err := os.Open(readPath)
		if err != nil {
			return err
		}
		defer in.Close()
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, in)
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr == nil {
			allowed[rel] = struct{}{}
		}
		return closeErr
	})
	return allowed, err
}

func validateCorpusDirectoryName(name string) error {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) {
		return fmt.Errorf("invalid corpus name %q", name)
	}
	return nil
}

// authoredRhizomeInput reports whether a .rhizome path is committed vault
// configuration the isolated index needs: config, ignore rules, and the
// top-level ontology SDL files schema.LoadSchema reads. Generated runtime
// state (index, caches, sessions) stays behind.
func authoredRhizomeInput(rel string, dir bool) bool {
	switch {
	case rel == ".rhizome/config.yml" || rel == ".rhizome/ignore":
		return !dir
	case rel == ".rhizome/ontology":
		return dir
	case strings.HasPrefix(rel, ".rhizome/ontology/"):
		name := strings.TrimPrefix(rel, ".rhizome/ontology/")
		return !dir && !strings.Contains(name, "/") && filepath.Ext(name) == ".graphql"
	default:
		return false
	}
}

func fileFingerprint(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(content)), nil
}

func excludedPopulationPath(path string, excludes []string) bool {
	path = strings.Trim(filepath.ToSlash(path), "/")
	if strings.HasPrefix(path, "docs/reference/analysis/") {
		name := strings.ToLower(filepath.Base(path))
		if strings.Contains(name, "evaluation") || strings.Contains(name, "evals") {
			return true
		}
	}
	for _, prefix := range repositoryAnswerArtifactPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	for _, prefix := range excludes {
		prefix = strings.Trim(filepath.ToSlash(prefix), "/")
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}
