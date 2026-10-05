package claude

import (
	"context"
	"os"
	"strings"

	"github.com/atomicobject/rhizome/pkg/harness"
	"github.com/atomicobject/rhizome/pkg/harness/internal/eventstream"
)

func (d *Driver) Status(ctx context.Context) (harness.Status, error) {
	status := harness.Status{Kind: harness.KindClaude, LoginHint: "claude auth login", Capabilities: harness.Capabilities{
		SupportsAllowedTools: true, SupportsAllowForSession: true,
		PermissionModes: []harness.PermissionMode{harness.PermissionApprovalRequired, harness.PermissionAutoAcceptEdits, harness.PermissionFullAccess},
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
	connection, err := d.start(ctx, path, args, cwd)
	if err != nil {
		status.LastError = err.Error()
		return status, nil
	}
	lifetime, cancel := context.WithCancel(context.Background())
	probe := &session{transport: connection, stream: eventstream.New(), lifetime: lifetime, cancel: cancel, shutdownTimeout: d.shutdownTimeout, responses: make(map[string]chan message), responseBacklog: make(map[string]message), pending: make(map[string]pendingApproval), items: make(map[string]harness.EventKind)}
	if probe.shutdownTimeout <= 0 {
		probe.shutdownTimeout = defaultShutdownTimeout
	}
	probe.reader.Add(1)
	go probe.readLoop()
	defer probe.Stop()
	response, err := probe.initialize(ctx, "status-init")
	if err != nil {
		status.LastError = err.Error()
		return status, nil
	}
	account := response.Response.Account
	if account == nil || strings.TrimSpace(account.Email) == "" && strings.TrimSpace(account.SubscriptionType) == "" {
		return status, nil
	}
	status.LoggedIn = true
	status.Account = account.Email
	if status.Account == "" {
		status.Account = account.SubscriptionType
	}
	for _, model := range response.Response.Models {
		id := model.Value
		if id == "" {
			id = model.ResolvedModel
		}
		if id == "" {
			continue
		}
		displayName := model.DisplayName
		if displayName == "" {
			displayName = model.ResolvedModel
		}
		if displayName == "" {
			displayName = id
		}
		status.Models = append(status.Models, harness.ModelOption{ID: id, DisplayName: displayName, Efforts: append([]string(nil), model.SupportedEffortLevels...), Default: model.IsDefault})
	}
	return status, nil
}
