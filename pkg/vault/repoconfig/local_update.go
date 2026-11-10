package repoconfig

import (
	"context"
	"os"
	"reflect"

	"github.com/atomicobject/rhizome/pkg/sqliteutil"
)

func lockLocalConfig(ctx context.Context, dir string) (func(), error) {
	if err := os.MkdirAll(rhizomeDir(dir), 0755); err != nil {
		return nil, err
	}
	return sqliteutil.LockSchemaInit(ctx, rhizomeConfigPath(dir))
}

// UpdateLocalConfig reads, edits, and saves under the same lock as ordinary saves.
// The callback must not call another configuration writer.
func UpdateLocalConfig(ctx context.Context, dir string, edit func(*LocalConfig) error) error {
	release, err := lockLocalConfig(ctx, dir)
	if err != nil {
		return err
	}
	defer release()
	before, err := LoadLocalConfig(dir)
	if err != nil {
		return err
	}
	cfg, err := LoadLocalConfig(dir)
	if err != nil {
		return err
	}
	if err := edit(cfg); err != nil {
		return err
	}
	if reflect.DeepEqual(before, cfg) {
		return nil
	}
	return saveLocalConfig(dir, *cfg)
}
