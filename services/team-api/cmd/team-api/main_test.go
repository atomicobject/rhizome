package main

import (
	"testing"

	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/atomicobject/rhizome/services/team-api/internal/config"
)

func TestTrafficConfigEnforcesConfiguredConcurrency(t *testing.T) {
	cfg := config.Config{Concurrency: wire.Limits{VoyageConcurrency: 2, TypeSafeConcurrency: 3}}
	if got := trafficConfig(cfg, providerKeys{}).PersonSlots; got != 3 {
		t.Fatalf("PersonSlots = %d, want the larger configured concurrency, 3", got)
	}
}
