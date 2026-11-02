package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProviderBudgetConfiguration(t *testing.T) {
	cfg, err := Parse([]byte(prodJSON))
	require.NoError(t, err)
	require.Zero(t, cfg.ProviderDailyNanodollars["voyage"])
	require.Zero(t, cfg.MaxProviderDailyNanodollars["typesafe"])
	for _, tc := range []struct {
		name, fields string
		invalid      bool
	}{
		{"explicit", `"provider_daily_nanodollars":{"voyage":12000000000},"max_provider_daily_nanodollars":{"voyage":12000000000},`, false},
		{"missing ceiling", `"provider_daily_nanodollars":{"voyage":1},`, true},
		{"negative", `"provider_daily_nanodollars":{"voyage":-1},`, true},
		{"unknown", `"provider_daily_nanodollars":{"jev":0},`, true},
		{"unknown ceiling", `"max_provider_daily_nanodollars":{"other":0},`, true},
		{"negative ceiling", `"max_provider_daily_nanodollars":{"voyage":-1},`, true},
		{"over ceiling", `"provider_daily_nanodollars":{"typesafe":2},"max_provider_daily_nanodollars":{"typesafe":1},`, true},
		{"fractional", `"provider_daily_nanodollars":{"voyage":1.1},`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(strings.Replace(prodJSON, `"mode"`, tc.fields+`"mode"`, 1)))
			if tc.invalid {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
