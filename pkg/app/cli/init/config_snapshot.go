package init

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

func configSnapshot(cfg repoconfig.LocalConfig) (string, error) {
	data, err := json.Marshal(cfg)
	return string(data), err
}

// The init publisher holds LockLocalConfig from this check through its writes.
func (r *initRun) checkConfigSnapshot() error {
	current, err := repoconfig.LoadLocalConfig(r.layout.ProjectRoot)
	if !r.layout.HasExistingConfig && errors.Is(err, repoconfig.ErrNoLocalConfig) {
		return nil
	}
	if err != nil {
		return err
	}
	actual, err := configSnapshot(*current)
	if err != nil {
		return err
	}
	expected := r.configSnapshot
	if expected == "" && r.layout.HasExistingConfig {
		expected, err = configSnapshot(r.layout.ExistingLocal)
		if err != nil {
			return err
		}
	}
	if actual != expected {
		return fmt.Errorf("repository settings changed while setup was being prepared; retry setup")
	}
	return nil
}
