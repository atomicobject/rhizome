package codex

import (
	"context"
	"os"

	"github.com/atomicobject/rhizome/pkg/harness"
)

func (d *Driver) Status(ctx context.Context) (harness.Status, error) {
	status := harness.Status{Kind: harness.KindCodex, LoginHint: "codex login", Capabilities: harness.Capabilities{
		SupportsAllowedTools: false, SupportsAllowForSession: true,
		PermissionModes: []harness.PermissionMode{harness.PermissionApprovalRequired, harness.PermissionAutoAcceptEdits, harness.PermissionFullAccess},
	}}
	path, err := d.runner.LookPath(d.binary)
	if err != nil {
		status.LastError = harness.Phase(harness.ErrNotInstalled, err).Error()
		return status, nil
	}
	status.Installed = true
	version, err := d.checkedVersion(ctx, path)
	status.Version = version
	if err != nil {
		status.LastError = err.Error()
		return status, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return status, err
	}
	connection, err := d.start(ctx, path, []string{"app-server"}, cwd)
	if err != nil {
		status.LastError = err.Error()
		return status, nil
	}
	probe := newSession(connection, harness.SessionOptions{}, cwd, turnCancelTimeout, d.shutdownTimeout, false)
	defer probe.Stop()
	client := probe.client
	_, err = client.initialize(ctx)
	if err != nil {
		status.LastError = err.Error()
		return status, nil
	}
	account, err := client.account(ctx)
	if err != nil {
		status.LastError = err.Error()
		return status, nil
	}
	if account.Account == nil {
		return status, nil
	}
	status.LoggedIn = true
	status.Account = account.Account.Email
	if status.Account == "" {
		status.Account = account.Account.Type
	}
	models, err := client.models(ctx)
	if err != nil {
		status.LastError = err.Error()
		return status, nil
	}
	for _, model := range models.Data {
		id := model.Model
		if id == "" {
			id = model.ID
		}
		displayName := model.DisplayName
		if displayName == "" {
			displayName = id
		}
		option := harness.ModelOption{ID: id, DisplayName: displayName, Default: model.IsDefault}
		for _, effort := range model.SupportedReasoningEfforts {
			if effort.ReasoningEffort != "" {
				option.Efforts = append(option.Efforts, effort.ReasoningEffort)
			}
		}
		status.Models = append(status.Models, option)
	}
	return status, nil
}
