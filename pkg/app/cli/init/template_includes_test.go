package init

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/atomicobject/rhizome/pkg/noteformat/builtin"
	vaultpaths "github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/vault/notediscovery"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestEnsureIncludesCoverTemplatePaths(t *testing.T) {
	tests := []struct {
		name        string
		startGlobs  []string
		paths       []string
		wantAdded   []string
		wantChanged bool
		wantFinal   []string
	}{
		{
			name:        "empty includes is left untouched (matches everything)",
			startGlobs:  nil,
			paths:       []string{"docs/specs/feature.md"},
			wantAdded:   nil,
			wantChanged: false,
			wantFinal:   nil,
		},
		{
			name:        "covered by existing glob",
			startGlobs:  []string{"docs/**/*.md"},
			paths:       []string{"docs/specs/feature.md", "docs/efforts/EFF-0001.md"},
			wantAdded:   nil,
			wantChanged: false,
			wantFinal:   []string{"docs/**/*.md"},
		},
		{
			name:       "exact HTML file already authorized",
			startGlobs: []string{"docs/efforts/example/plan.html"},
			paths:      []string{"docs/efforts/example/plan.html"},
			wantFinal:  []string{"docs/efforts/example/plan.html"},
		},
		{
			name:        "uncovered top-level dir is added once",
			startGlobs:  []string{"vault/**/*.md"},
			paths:       []string{"docs/specs/feature.md", "docs/efforts/EFF-0001.md"},
			wantAdded:   []string{"docs/**/*.md"},
			wantChanged: true,
			wantFinal:   []string{"vault/**/*.md", "docs/**/*.md"},
		},
		{
			name:        "skip dot-prefixed top-level dirs (skills, ontology)",
			startGlobs:  []string{"vault/**/*.md"},
			paths:       []string{".agents/skills/foo/SKILL.md", ".rhizome/ontology/x.graphql"},
			wantAdded:   nil,
			wantChanged: false,
			wantFinal:   []string{"vault/**/*.md"},
		},
		{
			name:        "non-markdown files ignored",
			startGlobs:  []string{"vault/**/*.md"},
			paths:       []string{"docs/specs/x.png", ".rhizome/views/foo.yaml"},
			wantAdded:   nil,
			wantChanged: false,
			wantFinal:   []string{"vault/**/*.md"},
		},
		{
			name:        "multiple uncovered top-level dirs",
			startGlobs:  []string{"vault/**/*.md"},
			paths:       []string{"docs/a.md", "playbooks/b.md", "docs/c.md"},
			wantAdded:   []string{"docs/**/*.md", "playbooks/**/*.md"},
			wantChanged: true,
			wantFinal:   []string{"vault/**/*.md", "docs/**/*.md", "playbooks/**/*.md"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &obsidian.LocalConfig{Notes: obsidian.LocalVaultConfig{Includes: append([]string(nil), tc.startGlobs...)}}
			added, changed := ensureIncludesCoverTemplatePaths(cfg, tc.paths)
			if changed != tc.wantChanged {
				t.Fatalf("changed=%v, want %v (added=%v)", changed, tc.wantChanged, added)
			}
			if !reflect.DeepEqual(added, tc.wantAdded) {
				t.Fatalf("added=%v, want %v", added, tc.wantAdded)
			}
			if !reflect.DeepEqual(cfg.Notes.Includes, tc.wantFinal) {
				t.Fatalf("final includes=%v, want %v", cfg.Notes.Includes, tc.wantFinal)
			}
		})
	}
}

func TestEffortHTMLIncludes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		includes []string
		want     []string
	}{
		{"omitted", nil, []string{"**/*.md", "docs/efforts/**/*.html"}},
		{"default", []string{"**/*.md"}, []string{"**/*.md", "docs/efforts/**/*.html"}},
		{"docs", []string{"docs/**/*.md"}, []string{"docs/**/*.md", "docs/efforts/**/*.html"}},
		{"custom", []string{"notes/**/*.md"}, []string{"notes/**/*.md", "docs/**/*.md", "docs/efforts/**/*.html"}},
		{"template directory only", []string{"**/*.md", "docs/efforts/templates/workspace/**/*.html"}, []string{"**/*.md", "docs/efforts/templates/workspace/**/*.html", "docs/efforts/**/*.html"}},
		{"all template directories only", []string{"**/*.md", "docs/efforts/templates/**/*.html"}, []string{"**/*.md", "docs/efforts/templates/**/*.html", "docs/efforts/**/*.html"}},
		{"broad include", []string{"docs/**/*"}, []string{"docs/**/*", "docs/efforts/**/*.html"}},
		{"exact workspace include", []string{"**/*.md", "docs/efforts/**/*.html"}, []string{"**/*.md", "docs/efforts/**/*.html"}},
		{"broader explicit include", []string{"**/*.md", "docs/**/*.html"}, []string{"**/*.md", "docs/**/*.html"}},
		{"uppercase explicit include", []string{"**/*.md", "docs/**/*.HTML"}, []string{"**/*.md", "docs/**/*.HTML"}},
		{"HTML brace include", []string{"**/*.md", "docs/**/*.{html,htm}"}, []string{"**/*.md", "docs/**/*.{html,htm}"}},
		{"mixed brace include", []string{"docs/**/*.{html,md}"}, []string{"docs/**/*.{html,md}", "docs/efforts/**/*.html"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			excludes := []string{"docs/efforts/private/**", "docs/generated/**"}
			cfg := &obsidian.LocalConfig{Notes: obsidian.LocalVaultConfig{Includes: tc.includes, Excludes: append([]string(nil), excludes...)}}
			paths := []string{"docs/efforts/example/index.md", "docs/efforts/example/plan.html", "docs/efforts/example/sub/progress.html"}
			starterPaths := []string{"docs/efforts/example/index.md", "docs/efforts/templates/workspace/overview.html.template", "docs/efforts/templates/workspace/plan.html.template"}
			starterCfg := &obsidian.LocalConfig{Notes: obsidian.LocalVaultConfig{Includes: append([]string(nil), tc.includes...)}}
			ensureIncludesCoverTemplatePaths(starterCfg, starterPaths)
			if !reflect.DeepEqual(starterCfg.Notes.Includes, tc.want) {
				t.Fatalf("starter includes=%v, want %v", starterCfg.Notes.Includes, tc.want)
			}
			ensureIncludesCoverTemplatePaths(cfg, paths)
			if !reflect.DeepEqual(cfg.Notes.Includes, tc.want) {
				t.Fatalf("includes=%v, want %v", cfg.Notes.Includes, tc.want)
			}
			if !reflect.DeepEqual(cfg.Notes.Excludes, excludes) {
				t.Fatalf("excludes changed: %v", cfg.Notes.Excludes)
			}
			formats, err := builtin.NewRegistry()
			require.NoError(t, err)
			plan, err := notediscovery.Compile(notediscovery.Config{
				Mode: notediscovery.CollectionVault, Includes: starterCfg.Notes.Includes, Registry: formats,
				Ignore: func(rel vaultpaths.RelPath) bool { return pathMatchesAnyGlob(rel.String(), excludes) },
			})
			require.NoError(t, err)
			for _, path := range paths {
				decision, err := plan.Classify(vaultpaths.RelPath(path), true)
				require.NoError(t, err)
				require.Equal(t, notediscovery.Note, decision.Owner, path)
			}
			ignored, err := plan.Classify(vaultpaths.RelPath("docs/efforts/private/plan.html"), true)
			require.NoError(t, err)
			require.Equal(t, notediscovery.Ignored, ignored.Owner)
			unrelated, err := plan.Classify(vaultpaths.RelPath("src/page.html"), true)
			require.NoError(t, err)
			require.Equal(t, notediscovery.Code, unrelated.Owner)
			if added, changed := ensureIncludesCoverTemplatePaths(starterCfg, starterPaths); changed || len(added) != 0 {
				t.Fatalf("second starter pass changed includes: %v", added)
			}
			if added, changed := ensureIncludesCoverTemplatePaths(cfg, paths); changed || len(added) != 0 {
				t.Fatalf("second pass changed includes: %v", added)
			}
		})
	}
}

func TestUnrelatedHTMLDoesNotAddIncludes(t *testing.T) {
	cfg := &obsidian.LocalConfig{}
	paths := []string{"docs/other/plan.html", "docs/efforts-other/plan.html", ".agents/skills/plan.html", ".rhizome/plan.html"}
	paths = append(paths, "docs/other/plan.html.template", "docs/efforts/templates/workspace/effort.css.template")
	if added, changed := ensureIncludesCoverTemplatePaths(cfg, paths); changed || len(added) != 0 {
		t.Fatalf("unrelated HTML changed includes: %v", added)
	}
	if cfg.Notes.Includes != nil {
		t.Fatalf("default includes changed: %v", cfg.Notes.Includes)
	}
}

func TestReconcileEffortHTMLIncludesAddsOnce(t *testing.T) {
	root := t.TempDir()
	cfg := obsidian.LocalConfig{Notes: obsidian.LocalVaultConfig{
		Includes: []string{"docs/**/*.md"}, Excludes: []string{"docs/efforts/private/**"},
	}}
	paths := []string{"docs/efforts/example/plan.html"}
	if _, changed, err := ensureInstalledTemplateIncludes(root, &cfg, paths); err != nil || !changed {
		t.Fatalf("first reconcile: changed=%v err=%v", changed, err)
	}
	if _, changed, err := ensureInstalledTemplateIncludes(root, &cfg, paths); err != nil || changed {
		t.Fatalf("second reconcile: changed=%v err=%v", changed, err)
	}
}

func TestReconcileExistingWorkspaceTemplates(t *testing.T) {
	for _, asset := range []string{"overview.html.template", "plan.html.template", "effort.css.template", ""} {
		t.Run(asset, func(t *testing.T) {
			root := t.TempDir()
			if asset != "" {
				workspace := filepath.Join(root, "docs/efforts/templates/workspace")
				require.NoError(t, os.MkdirAll(workspace, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(workspace, asset), []byte("installed asset"), 0o644))
			}
			cfg := obsidian.LocalConfig{Notes: obsidian.LocalVaultConfig{Includes: []string{"**/*.md"}}}
			_, changed, err := ensureInstalledTemplateIncludes(root, &cfg, nil)
			require.NoError(t, err)
			wantHTML := asset == "overview.html.template" || asset == "plan.html.template"
			require.Equal(t, wantHTML, changed)
			if wantHTML {
				require.Equal(t, []string{"**/*.md", "docs/efforts/**/*.html"}, cfg.Notes.Includes)
			} else {
				require.Equal(t, []string{"**/*.md"}, cfg.Notes.Includes)
			}
		})
	}
}

func TestReconcilePartialMarkdownAndHTMLTemplates(t *testing.T) {
	for _, asset := range []string{"effort.md.template", "workspace/work-log.md.template", "workspace/materials/material.md.template"} {
		for _, tc := range []struct {
			name           string
			includes, want []string
		}{
			{"narrow", []string{"notes/**/*.md"}, []string{"notes/**/*.md", "docs/**/*.md", "docs/efforts/**/*.html"}},
			{"broad", []string{"docs/**/*"}, []string{"docs/**/*", "docs/efforts/**/*.html"}},
			{"explicit", []string{"docs/**/*.md", "docs/**/*.html"}, []string{"docs/**/*.md", "docs/**/*.html"}},
		} {
			t.Run(asset+"/"+tc.name, func(t *testing.T) {
				root := t.TempDir()
				for _, name := range []string{asset, "workspace/overview.html.template"} {
					path := filepath.Join(root, "docs/efforts/templates", name)
					require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
					require.NoError(t, os.WriteFile(path, []byte("installed asset"), 0o644))
				}
				cfg := obsidian.LocalConfig{Notes: obsidian.LocalVaultConfig{Includes: tc.includes, Excludes: []string{"docs/efforts/private/**"}}}
				_, _, err := ensureInstalledTemplateIncludes(root, &cfg, nil)
				require.NoError(t, err)
				require.ElementsMatch(t, tc.want, cfg.Notes.Includes)
				require.Equal(t, []string{"docs/efforts/private/**"}, cfg.Notes.Excludes)
				_, changed, err := ensureInstalledTemplateIncludes(root, &cfg, nil)
				require.NoError(t, err)
				require.False(t, changed)
			})
		}
	}
}

func TestInstalledTemplateIncludesRelativeRoot(t *testing.T) {
	parent := t.TempDir()
	t.Chdir(parent)
	workspace := filepath.Join("project", "docs/efforts/templates/workspace")
	require.NoError(t, os.MkdirAll(workspace, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "overview.html.template"), []byte("installed asset"), 0o644))
	cfg := obsidian.LocalConfig{Notes: obsidian.LocalVaultConfig{Includes: []string{"**/*.md"}}}
	added, changed, err := ensureInstalledTemplateIncludes("project", &cfg, nil)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, []string{"docs/efforts/**/*.html"}, added)
	_, changed, err = ensureInstalledTemplateIncludes("project/../project", &cfg, nil)
	require.NoError(t, err)
	require.False(t, changed)
}

func TestInstalledTemplateIncludesSymlinkContainment(t *testing.T) {
	for _, outside := range []bool{false, true} {
		for _, aliasRoot := range []bool{false, true} {
			for _, linkPath := range []string{"docs", "docs/efforts/templates"} {
				t.Run(fmt.Sprintf("outside=%t/aliasRoot=%t/link=%s", outside, aliasRoot, linkPath), func(t *testing.T) {
					root, err := filepath.EvalSymlinks(t.TempDir())
					require.NoError(t, err)
					target := filepath.Join(root, "assets")
					if outside {
						target = t.TempDir()
					}
					workspace := filepath.Join(target, "workspace")
					if linkPath == "docs" {
						workspace = filepath.Join(target, "efforts/templates/workspace")
					}
					require.NoError(t, os.MkdirAll(workspace, 0o755))
					require.NoError(t, os.WriteFile(filepath.Join(workspace, "overview.html.template"), []byte("installed asset"), 0o644))
					require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, linkPath)), 0o755))
					require.NoError(t, os.Symlink(target, filepath.Join(root, linkPath)))
					if aliasRoot {
						alias := filepath.Join(t.TempDir(), "vault")
						require.NoError(t, os.Symlink(root, alias))
						root = alias
					}
					cfg := obsidian.LocalConfig{Notes: obsidian.LocalVaultConfig{Includes: []string{"**/*.md"}}}
					_, changed, err := ensureInstalledTemplateIncludes(root, &cfg, nil)
					if outside {
						require.ErrorIs(t, err, vaultpaths.ErrOutsideVault)
						require.False(t, changed)
						require.Equal(t, []string{"**/*.md"}, cfg.Notes.Includes)
						return
					}
					require.ErrorContains(t, err, "must not use directory symlinks")
					require.False(t, changed)
					require.Equal(t, []string{"**/*.md"}, cfg.Notes.Includes)
				})
			}
		}
	}
}

func TestInstalledTemplateIncludesDiscoverGeneratedHTML(t *testing.T) {
	for _, aliasRoot := range []bool{false, true} {
		t.Run(fmt.Sprintf("aliasRoot=%t", aliasRoot), func(t *testing.T) {
			root := t.TempDir()
			for _, rel := range []string{"docs/efforts/templates/workspace/overview.html.template", "docs/efforts/example/overview.html"} {
				path := filepath.Join(root, rel)
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
				require.NoError(t, os.WriteFile(path, []byte("<html></html>"), 0o644))
			}
			if aliasRoot {
				alias := filepath.Join(t.TempDir(), "vault")
				require.NoError(t, os.Symlink(root, alias))
				root = alias
			}
			cfg := obsidian.LocalConfig{Notes: obsidian.LocalVaultConfig{Includes: []string{"**/*.md"}}}
			_, changed, err := ensureInstalledTemplateIncludes(root, &cfg, nil)
			require.NoError(t, err)
			require.True(t, changed)
			files, err := obsidian.DiscoverFiles(obsidian.VaultDefinition{Root: root, Includes: cfg.Notes.Includes})
			require.NoError(t, err)
			require.Contains(t, files, "docs/efforts/example/overview.html")
			_, changed, err = ensureInstalledTemplateIncludes(root, &cfg, nil)
			require.NoError(t, err)
			require.False(t, changed)
		})
	}
}
