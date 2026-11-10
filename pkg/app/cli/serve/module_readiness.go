package serve

import (
	"context"
	"fmt"
	"slices"

	"github.com/atomicobject/rhizome/pkg/app/actions"
	initactions "github.com/atomicobject/rhizome/pkg/app/cli/init"
	"github.com/atomicobject/rhizome/pkg/app/modulereadiness"
	"github.com/atomicobject/rhizome/pkg/app/web"
	"github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/schema"
)

func moduleReadiness(root, vault string, changed func()) *modulereadiness.Coordinator {
	runner := actions.New(root)
	runner.Vault = vault
	return modulereadiness.New(modulereadiness.Options{
		Root: root, Runner: runner, Trust: runner.Trust, Changed: changed,
		Include: func(ctx context.Context, paths []string) error {
			return initactions.EnsureModuleSourceIncludes(ctx, root, paths)
		},
		Schema: func() error { return moduleSchema(root) },
	})
}

const moduleReadinessEvent = "starter-readiness.changed"

var _ web.ModuleReadiness = (*modulereadiness.Coordinator)(nil)

func moduleSchema(root string) error {
	authored, err := schema.LoadSchema(root)
	if err != nil {
		return fmt.Errorf("authored ontology: %w", err)
	}
	if _, err = query.BuildExecutableSchema(authored); err != nil {
		names := make([]string, 0, len(authored.Types))
		for name := range authored.Types {
			names = append(names, name)
		}
		slices.Sort(names)
		return fmt.Errorf("executable query schema for types %v in %v: %w", names, authored.Files, err)
	}
	return nil
}
