package update

import (
	"github.com/atomicobject/rhizome/pkg/app/appbundle"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/atomicobject/rhizome/pkg/vault/version"
)

func (r Result) RepositoryRefreshExecutable(cfg *repoconfig.LocalConfig, authority, selectedExecutable string) (string, error) {
	if cfg == nil || (authority != "managed" && authority != "global") {
		return "", nil
	}
	for _, target := range r.Targets {
		previousVersion := version.Version
		if authority == "managed" {
			if target.Role != UpdateTargetRepo || !target.PinWritten {
				continue
			}
			previousVersion = cfg.Rhizome.Version
		} else if target.Role != UpdateTargetGlobal {
			continue
		}
		if !target.BinaryUpdated || NormalizeVersion(target.Version) == NormalizeVersion(previousVersion) {
			continue
		}
		if target.Role == UpdateTargetGlobal {
			bundle, err := appbundle.Resolve(selectedExecutable)
			if err != nil {
				return "", err
			}
			if bundle != nil {
				if bundle.Portable {
					current, err := appbundle.Read(bundle.Root)
					if err != nil {
						return "", err
					}
					return current.ExecutablePath, nil
				}
				return bundle.ExecutablePath, nil
			}
		}
		return target.Path, nil
	}
	return "", nil
}
