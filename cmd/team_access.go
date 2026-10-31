package cmd

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/app/provideraccess"
	"github.com/atomicobject/rhizome/pkg/app/teamaccess"
	"github.com/atomicobject/rhizome/pkg/vault/version"
)

// installProviderAccess gives every provider construction in this process the
// person's team session (pkg/app/teamaccess).
func installProviderAccess() {
	if testing.Testing() {
		// Tests never read the person's real session or reach their gateway.
		return
	}
	provideraccess.Install(teamaccess.Factory(version.Development(), version.Version))
}
