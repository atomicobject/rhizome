package claude

import (
	"context"
	"os"
	"strings"

	"github.com/atomicobject/rhizome/pkg/harness"
)

func (d *Driver) Status(ctx context.Context) (harness.Status, error) {
	status := harness.Status{Kind: harness.KindClaude, LoginHint: "claude auth login", Capabilities: harness.Capabilities{
		SupportsAllowedTools: true, SupportsAllowForSession: true,
		PermissionModes: []harness.PermissionMode{harness.PermissionAuto, harness.PermissionApprovalRequired, harness.PermissionAutoAcceptEdits, harness.PermissionFullAccess},
	}}
	path, err := d.runner.LookPath(d.binary)
	if err != nil {
		status.LastError = harness.Phase(harness.ErrNotInstalled, err).Error()
		return status, nil
	}
	status.Installed = true
	status.Version, err = d.checkedVersion(ctx, path)
	if err != nil {
		status.LastError = err.Error()
		return status, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return status, err
	}
	args := []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--strict-mcp-config", "--permission-prompt-tool", "stdio", "--permission-mode", "dontAsk", "--tools", "", "--settings", `{"disableAllHooks":true}`}
	connection, err := d.start(ctx, path, args, cwd, nil)
	if err != nil {
		status.LastError = err.Error()
		return status, nil
	}
	probe := d.newSession(connection, false)
	defer probe.Stop()
	response, err := probe.initialize(ctx, "status-init")
	if err != nil {
		status.LastError = err.Error()
		return status, nil
	}
	// Claude Code lists its models signed out too, so a person can choose one
	// before signing in. "default" points at a model rather than being one.
	defaultModel := ""
	for _, model := range response.Response.Models {
		if model.Value == "default" {
			defaultModel = model.ResolvedModel
		}
	}
	for _, model := range response.Response.Models {
		id := model.Value
		if id == "" {
			id = model.ResolvedModel
		}
		if id == "" || id == "default" {
			continue
		}
		displayName := model.DisplayName
		if displayName == "" {
			displayName = model.ResolvedModel
		}
		if displayName == "" {
			displayName = id
		}
		isDefault := model.IsDefault || defaultModel != "" && model.ResolvedModel == defaultModel
		if isDefault {
			defaultModel = ""
		}
		status.Models = append(status.Models, harness.ModelOption{ID: id, DisplayName: displayName, Efforts: append([]string(nil), model.SupportedEffortLevels...), Default: isDefault})
	}
	account := response.Response.Account
	if account == nil {
		return status, nil
	}
	// An API key (Claude Code 2.1.325 reports its apiKeySource) or a cloud
	// provider such as Bedrock signs in without an account email.
	provider, apiKey := account.APIProvider, account.APIKeySource
	if provider == "firstParty" {
		provider = ""
	}
	if apiKey == "none" {
		apiKey = ""
	}
	for _, label := range []string{account.Email, account.SubscriptionType, provider, apiKey} {
		if strings.TrimSpace(label) != "" {
			status.LoggedIn, status.Account = true, label
			return status, nil
		}
	}
	return status, nil
}
