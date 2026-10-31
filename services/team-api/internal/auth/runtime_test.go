package auth

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/atomicobject/rhizome/services/team-api/internal/core"
	"github.com/atomicobject/rhizome/services/team-api/internal/store"
)

func TestEligibleRequiresAnAcceptedDomain(t *testing.T) {
	rt := core.Runtime{EligibilityMode: wire.EligibilityAll, AcceptedDomains: []string{"atomicobject.com"}}
	member := store.Person{Email: "a@atomicobject.com", HostedDomain: "atomicobject.com", Status: wire.PersonActive}
	if !eligible(member, rt) {
		t.Fatal("active member of an accepted domain is not eligible")
	}
	// The domain was removed from the configuration after this person signed in.
	rt.AcceptedDomains = []string{"example.org"}
	if eligible(member, rt) {
		t.Fatal("a person whose domain is no longer accepted stays eligible")
	}
}
