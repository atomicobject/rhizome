package runtime

import (
	"testing"
	"time"

	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/stretchr/testify/require"
)

func TestAutostartResolution(t *testing.T) {
	off := false
	require.True(t, AutostartEnabled(nil))
	require.False(t, AutostartEnabled(&repoconfig.LocalConfig{Runtime: &repoconfig.LocalRuntimeConfig{Autostart: &off}}))
	t.Setenv(AutostartEnv, "0")
	require.False(t, AutostartEnabled(nil))
	t.Setenv(AutostartEnv, "true")
	require.True(t, AutostartEnabled(&repoconfig.LocalConfig{Runtime: &repoconfig.LocalRuntimeConfig{Autostart: &off}}), "env overrides config")
}

func TestIdleTimeoutResolution(t *testing.T) {
	d, err := IdleTimeout(nil)
	require.NoError(t, err)
	require.Equal(t, time.Hour, d)
	d, err = IdleTimeout(&repoconfig.LocalConfig{Runtime: &repoconfig.LocalRuntimeConfig{IdleTimeout: "15m"}})
	require.NoError(t, err)
	require.Equal(t, 15*time.Minute, d)
	_, err = IdleTimeout(&repoconfig.LocalConfig{Runtime: &repoconfig.LocalRuntimeConfig{IdleTimeout: "-1s"}})
	require.Error(t, err)
}
