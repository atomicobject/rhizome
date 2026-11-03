package jsonstdio

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSessionEnvironmentDoesNotLeak(t *testing.T) {
	t.Setenv("RZM_SYNTHETIC_INHERITED", "parent")
	t.Setenv("RZM_SYNTHETIC_OVERRIDE", "parent")
	binary, err := os.Executable()
	require.NoError(t, err)
	for _, override := range [][]string{{"RZM_SYNTHETIC_OVERRIDE=child"}, nil} {
		child, err := Start[map[string]string](context.Background(), "fixture", binary, []string{"-test.run=^TestJSONStdioEnvironmentHelper$", "--", "environment-helper"}, t.TempDir(), override)
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		value, err := child.Recv(ctx)
		require.NoError(t, err)
		require.Equal(t, "parent", value["inherited"])
		expected := "parent"
		if override != nil {
			expected = "child"
		}
		require.Equal(t, expected, value["override"])
		require.NoError(t, child.Close(ctx))
		cancel()
		require.Equal(t, "parent", os.Getenv("RZM_SYNTHETIC_OVERRIDE"))
	}
}

func TestJSONStdioEnvironmentHelper(t *testing.T) {
	if !slices.Contains(os.Args, "environment-helper") {
		return
	}
	data, _ := json.Marshal(map[string]string{"inherited": os.Getenv("RZM_SYNTHETIC_INHERITED"), "override": os.Getenv("RZM_SYNTHETIC_OVERRIDE")})
	fmt.Println(string(data))
	os.Exit(0)
}
