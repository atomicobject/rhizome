package jsonstdio

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strconv"
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

func TestSessionEnvironmentDropsRepoDelegationBypass(t *testing.T) {
	t.Setenv("RZM_SKIP_REPO_DELEGATE", "1")
	t.Setenv("RZM_REPO_DELEGATED", "1")
	binary, err := os.Executable()
	require.NoError(t, err)
	child, err := Start[map[string]string](context.Background(), "fixture", binary, []string{"-test.run=^TestJSONStdioEnvironmentHelper$", "--", "environment-helper"}, t.TempDir())
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	value, err := child.Recv(ctx)
	require.NoError(t, err)
	require.NoError(t, child.Close(ctx))
	require.Equal(t, "false", value["skip"], "RZM_SKIP_REPO_DELEGATE reached the harness child")
	require.Equal(t, "false", value["delegated"], "RZM_REPO_DELEGATED reached the harness child")
}

func TestJSONStdioEnvironmentHelper(t *testing.T) {
	if !slices.Contains(os.Args, "environment-helper") {
		return
	}
	_, skip := os.LookupEnv("RZM_SKIP_REPO_DELEGATE")
	_, delegated := os.LookupEnv("RZM_REPO_DELEGATED")
	data, _ := json.Marshal(map[string]string{
		"inherited": os.Getenv("RZM_SYNTHETIC_INHERITED"), "override": os.Getenv("RZM_SYNTHETIC_OVERRIDE"),
		"skip": strconv.FormatBool(skip), "delegated": strconv.FormatBool(delegated),
	})
	fmt.Println(string(data))
	os.Exit(0)
}
