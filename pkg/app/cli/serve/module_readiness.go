package serve

import (
	"context"
	"fmt"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/actions"
	initactions "github.com/atomicobject/rhizome/pkg/app/cli/init"
	"github.com/atomicobject/rhizome/pkg/app/modulereadiness"
	"github.com/atomicobject/rhizome/pkg/app/web"
	"github.com/atomicobject/rhizome/pkg/noteformat"
	"github.com/atomicobject/rhizome/pkg/ontology/query"
	"github.com/atomicobject/rhizome/pkg/ontology/schema"
)

func moduleReadiness(root, vault string, formats noteformat.Registry, changed func()) *modulereadiness.Coordinator {
	runner := actions.New(root)
	runner.Vault = vault
	runner.Lifecycle = true
	return modulereadiness.New(modulereadiness.Options{
		Root: root, Runner: runner, Trust: runner.Trust, Changed: changed,
		Include: func(ctx context.Context, paths []string) error {
			return initactions.EnsureModuleSourceIncludes(ctx, root, paths, formats)
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
		return fmt.Errorf("ontology query schema: %w\nSchema files:\n%s", err, strings.Join(authored.Files, "\n"))
	}
	return nil
}
