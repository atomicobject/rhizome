// Package modulelifecycle reads optional installed module readiness declarations.
package modulelifecycle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/assetroots"
	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"
)

type Descriptor struct {
	Module   string   `yaml:"-"`
	Title    string   `yaml:"title"`
	Observe  string   `yaml:"observe"`
	Setup    string   `yaml:"setup"`
	Index    string   `yaml:"index"`
	Verify   string   `yaml:"verify"`
	Inputs   []string `yaml:"inputs"`
	Excludes []string `yaml:"excludes,omitempty"`
	Problem  string   `yaml:"-"`
}

func Load(root string) ([]Descriptor, error) {
	fsRoot, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer fsRoot.Close()
	modules, err := fsRoot.OpenRoot(assetroots.ModulesDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer modules.Close()
	entries, err := fs.ReadDir(modules.FS(), ".")
	if err != nil {
		return nil, err
	}
	var result []Descriptor
	for _, e := range entries {
		if !e.IsDir() || !assetroots.ValidModuleName(e.Name()) {
			continue
		}
		file := filepath.Join(assetroots.ModuleRoot(e.Name()), "lifecycle.yml")
		data, err := readRegularFile(fsRoot, file, 64<<10)
		if os.IsNotExist(err) {
			continue
		}
		d := Descriptor{Module: e.Name()}
		if err == nil {
			decoder := yaml.NewDecoder(bytes.NewReader(data))
			decoder.KnownFields(true)
			err = decoder.Decode(&d)
		}
		if err == nil {
			err = d.validate()
		}
		if err != nil {
			d.Problem = fmt.Sprintf("%s: %v", file, err)
		}
		result = append(result, d)
	}
	return result, nil
}

func (d Descriptor) validate() error {
	if d.Title == "" || len(d.Inputs) == 0 || len(d.Inputs) > 32 {
		return fmt.Errorf("title and 1 to 32 input paths are required")
	}
	for _, action := range []string{d.Observe, d.Setup, d.Index, d.Verify} {
		if !assetroots.ValidModuleName(action) {
			return fmt.Errorf("action references must name actions in this module")
		}
	}
	if len(d.Excludes) > 32 {
		return fmt.Errorf("at most 32 exclusions are allowed")
	}
	for _, p := range d.Excludes {
		if !safePath(strings.NewReplacer("*", "x", "?", "x").Replace(p)) {
			return fmt.Errorf("invalid exclusion %q", p)
		}
		if _, err := doublestar.Match(p, ""); err != nil {
			return err
		}
	}
	for _, p := range d.Inputs {
		if !safePath(p) {
			return fmt.Errorf("input %q must be a bounded repository-relative path", p)
		}
	}
	return nil
}

func safePath(p string) bool {
	return p != "" && p != "." && filepath.ToSlash(filepath.Clean(p)) == p && !filepath.IsAbs(p) && p != ".." && !strings.HasPrefix(p, "../") && !strings.ContainsAny(p, `\*?[]`) && p != ".git" && !strings.HasPrefix(p, ".git/")
}

// Fingerprint reads at most 64 MiB and visits 20000 paths, never following symlinks.
// Missing inputs are part of the key so installation and removal invalidate it.
func Fingerprint(ctx context.Context, root string, d Descriptor) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	root = resolvedRoot
	fsRoot, err := os.OpenRoot(root)
	if err != nil {
		return "", err
	}
	defer fsRoot.Close()
	h := sha256.New()
	fmt.Fprintf(h, "%+v", d)
	inputs := append([]string{assetroots.ModuleRoot(d.Module) + "/lifecycle.yml"}, d.Inputs...)
	entries, size := 0, int64(0)
	for _, input := range inputs {
		if !safePath(input) {
			return "", fmt.Errorf("unsafe input %q", input)
		}
		if err := noSymlinks(root, input); err != nil {
			return "", err
		}
		err := fs.WalkDir(fsRoot.FS(), input, func(relative string, e fs.DirEntry, err error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			p := filepath.Join(root, filepath.FromSlash(relative))
			if os.IsNotExist(err) {
				fmt.Fprintf(h, "missing:%s", p)
				return nil
			}
			if err != nil {
				return err
			}
			entries++
			if entries > 20000 {
				return fmt.Errorf("readiness inputs exceed 20000 paths; narrow lifecycle inputs")
			}
			rel, err := paths.ToRel(paths.AbsPath(p), root)
			if err != nil {
				return err
			}
			for _, pattern := range d.Excludes {
				if matched, _ := doublestar.Match(pattern, rel.String()); matched {
					if e.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
			}
			if e.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("readiness input cannot be a symlink: %s", p)
			}
			if e.IsDir() {
				fmt.Fprintf(h, "directory:%s;", p)
				return nil
			}
			if !e.Type().IsRegular() {
				return fmt.Errorf("readiness input is not a regular file: %s", p)
			}
			info, err := e.Info()
			if err != nil {
				return err
			}
			size += info.Size()
			if size > 64<<20 {
				return fmt.Errorf("readiness inputs exceed 64 MiB; narrow lifecycle inputs")
			}
			data, err := readRegularFile(fsRoot, rel.String(), (64<<20)-size+info.Size())
			if err != nil {
				return err
			}
			if int64(len(data)) > (64<<20)-size+info.Size() {
				return fmt.Errorf("readiness input grew beyond 64 MiB")
			}
			fmt.Fprintf(h, "%s:%x;", p, sha256.Sum256(data))
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func noSymlinks(root, rel string) error {
	current := root
	for _, part := range strings.Split(rel, "/") {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("readiness input cannot use symlink: %s", current)
		}
	}
	return nil
}
