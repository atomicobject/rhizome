package init

import (
	"context"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/assetroots"
	"gopkg.in/yaml.v3"
)

// History is recovery evidence only for flat module assets lacking ownership.
// Ordinary installations never invoke Git here.
func (g *generatedFiles) recoverLegacyStarterFiles(root string, templates []string) error {
	candidates := map[string]string{}
	for _, template := range templates {
		files, err := starterModuleFiles(template)
		if err != nil {
			return err
		}
		for _, file := range files {
			if file.legacy == "" || g.Written[file.legacy] != "" || g.Written[file.target] != "" || g.Declined[file.legacy] != "" || g.Declined[file.target] != "" {
				continue
			}
			data, exists, err := readIfExists(path.Join(root, file.legacy))
			if err != nil {
				return err
			}
			if exists {
				family := path.Base(assetroots.FlatDir(file.kind))
				candidates[family+":"+strings.TrimPrefix(file.legacy, assetroots.FlatDir(file.kind)+"/")] = fingerprintStarterAsset(data)
			}
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	log, err := exec.CommandContext(ctx, "git", "-C", root, "log", "--format=%H", "-n", "50", "--", ".rhizome/workflows.yml").Output()
	if err != nil {
		return nil
	}
	for _, sha := range strings.Fields(string(log)) {
		data, err := exec.CommandContext(ctx, "git", "-C", root, "show", sha+":./.rhizome/workflows.yml").Output()
		if err != nil {
			continue
		}
		var old struct {
			Management struct {
				SourceFingerprints map[string]string `yaml:"sourceFingerprints"`
			} `yaml:"management"`
		}
		if yaml.Unmarshal(data, &old) != nil {
			continue
		}
		matches := map[string]string{}
		for key, hash := range old.Management.SourceFingerprints {
			_, family, name, ok := parseStarterFingerprintKey(key)
			if family == "queryRecipes" {
				family = "query-recipes"
			}
			if ok && hash != "" && candidates[family+":"+name] == hash {
				matches[key] = hash
			}
		}
		if err := g.foldLegacyStarterFiles(root, matches); err != nil {
			return err
		}
	}
	return nil
}
