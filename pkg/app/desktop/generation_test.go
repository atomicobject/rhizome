package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBridgeAcceptsAnInstallationGenerationOnlyForCliRemoval(t *testing.T) {
	for _, operation := range []string{"inspect", "cli-link-status", "cli-link-install", "global-status"} {
		response := (&Service{}).Handle(context.Background(), Request{
			Protocol: Protocol, Operation: operation, ExpectedGeneration: 4,
		})
		require.NotNil(t, response.Error, operation)
		require.Equal(t, "invalid_request", response.Error.Code)
	}
	var output bytes.Buffer
	require.Error(t, Serve(context.Background(), &Service{}, strings.NewReader(`{"protocol":1,"operation":"cli-link-remove","expectedGeneration":-1}`), &output))
	var response Response
	require.NoError(t, json.Unmarshal(output.Bytes(), &response))
	require.Equal(t, "invalid_request", response.Error.Code)
}

func TestGuardedCliRemovalRequiresAPortableOwner(t *testing.T) {
	cliLinkFixture(t)
	_, err := (&Service{}).CLILinkRemoveExpected(context.Background(), 4)
	code(t, err, "invalid_request")
}
