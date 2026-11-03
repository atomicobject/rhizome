package traffic

import "github.com/atomicobject/rhizome/services/team-api/internal/providerbudget"

// charge uses a release-specific override when present. Other accepted Jev
// releases inherit the alias's conservative reservation and settlement rate.
func (p *provider) charge(model string) Charge {
	if charge, ok := p.models[model]; ok {
		return charge
	}
	if p.name == providerbudget.TypeSafe {
		return p.models[defaultTypeSafeModel]
	}
	return Charge{}
}
