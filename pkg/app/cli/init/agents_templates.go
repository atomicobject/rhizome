package init

import (
	rhizomemdtemplates "github.com/atomicobject/rhizome/docs/rhizome-md-templates"
)

// Load major static chunks used to generate the managed Rhizome guidance block.
//
// Docs:
// - [RHIZOME.md templates + rzm init](docs/reference/guides/RHIZOME.md templates + rzm init.md)
func loadRhizomeMdTemplate(name string) (string, error) {
	return rhizomemdtemplates.Load(name)
}
