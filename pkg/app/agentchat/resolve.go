package agentchat

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/atomicobject/rhizome/pkg/harness"
)

// ResolveHarness returns the harness the caller named, else the user's
// agent.harness setting, else the only harness installed and signed in.
func ResolveHarness(ctx context.Context, requested string, drivers map[harness.Kind]harness.Harness, configured func() (harness.Kind, error)) (harness.Kind, error) {
	var choices []string
	for kind := range drivers {
		choices = append(choices, string(kind))
	}
	slices.Sort(choices)
	if requested != "" {
		if drivers[harness.Kind(requested)] == nil {
			return "", fmt.Errorf("provider must be one of %s; got %q", strings.Join(choices, ", "), requested)
		}
		return harness.Kind(requested), nil
	}
	kind, err := configured()
	if err != nil {
		return "", fmt.Errorf("read the agent.harness setting: %w", err)
	}
	if drivers[kind] != nil {
		return kind, nil
	}
	var ready []string
	for _, choice := range choices {
		status, err := drivers[harness.Kind(choice)].Status(ctx)
		if err == nil && status.Installed && status.LoggedIn {
			ready = append(ready, choice)
		}
	}
	if len(ready) == 1 {
		return harness.Kind(ready[0]), nil
	}
	found := "no provider is installed and signed in"
	if len(ready) > 1 {
		found = strings.Join(ready, ", ") + " are installed and signed in"
	}
	return "", fmt.Errorf("name a provider (%s) or set agent.harness: it is unset and %s", strings.Join(choices, ", "), found)
}
