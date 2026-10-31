package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/atomicobject/rhizome/pkg/teamapi/session"
	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/spf13/cobra"
)

func newAuthAdminCommand(deps authDeps) *cobra.Command {
	command := &cobra.Command{
		Use:   "admin",
		Short: "Manage team API usage, people, sessions, and switches (gateway admins only)",
	}
	run := func(build func(cmd *cobra.Command, args []string) (method, path string, body any, err error)) func(*cobra.Command, []string) error {
		return func(cmd *cobra.Command, args []string) error {
			method, path, body, err := build(cmd, args)
			if err != nil {
				return authFail(cmd, err)
			}
			account, err := deps.requiredAccount(cmd)
			if err != nil {
				return authFail(cmd, err)
			}
			var data []byte
			if body != nil {
				if data, err = json.Marshal(body); err != nil {
					return authFail(cmd, err)
				}
			}
			var raw json.RawMessage
			err = session.Call(cmd.Context(), account.HTTPClient(), method, account.Origin()+path, "", "rzm/"+deps.version, data, &raw)
			if err != nil {
				return authFail(cmd, err)
			}
			if len(raw) == 0 { // the gateway confirmed with an empty body
				raw = json.RawMessage(`{"ok":true}`)
			}
			if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
				var compact bytes.Buffer
				if json.Compact(&compact, raw) == nil {
					raw = compact.Bytes()
				}
				_, err = cmd.OutOrStdout().Write(append(raw, '\n'))
				return err
			}
			var indented bytes.Buffer
			if json.Indent(&indented, raw, "", "  ") != nil {
				indented.Write(raw)
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), indented.String())
			return err
		}
	}
	withQuery := func(path string, values url.Values) string {
		if encoded := values.Encode(); encoded != "" {
			return path + "?" + encoded
		}
		return path
	}
	sub := func(route, value string) string { return strings.Replace(route, "{sub}", url.PathEscape(value), 1) }

	usage := &cobra.Command{Use: "usage", Short: "Show a UTC day's global and per-person usage against limits", Args: cobra.NoArgs,
		RunE: run(func(cmd *cobra.Command, _ []string) (string, string, any, error) {
			date, _ := cmd.Flags().GetString("date")
			values := url.Values{}
			if date != "" {
				values.Set("date", date)
			}
			return http.MethodGet, withQuery(wire.RouteAdminUsage, values), nil, nil
		})}
	usage.Flags().String("date", "", "UTC day as YYYY-MM-DD (default: today)")

	people := &cobra.Command{Use: "people", Short: "List people known to the gateway", Args: cobra.NoArgs,
		RunE: run(func(*cobra.Command, []string) (string, string, any, error) {
			return http.MethodGet, wire.RouteAdminPeople, nil, nil
		})}

	person := &cobra.Command{Use: "person <sub>", Short: "Change a person's status or daily limit override", Args: cobra.ExactArgs(1),
		RunE: run(func(cmd *cobra.Command, args []string) (string, string, any, error) {
			var update wire.AdminPersonUpdate
			if status, _ := cmd.Flags().GetString("status"); status != "" {
				if status != wire.PersonActive && status != wire.PersonDisabled {
					return "", "", nil, fmt.Errorf("--status must be %s or %s", wire.PersonActive, wire.PersonDisabled)
				}
				update.Status = &status
			}
			update.ClearLimits, _ = cmd.Flags().GetBool("clear-limits")
			if raw, _ := cmd.Flags().GetString("limits"); raw != "" {
				if update.ClearLimits {
					return "", "", nil, errors.New("use --limits or --clear-limits, not both")
				}
				limits, err := parseDailyLimits(raw)
				if err != nil {
					return "", "", nil, fmt.Errorf("--limits: %w", err)
				}
				update.Limits = &limits
			}
			if update.Status == nil && update.Limits == nil && !update.ClearLimits {
				return "", "", nil, errors.New("nothing to change: pass --status, --limits, or --clear-limits")
			}
			return http.MethodPatch, sub(wire.RouteAdminPerson, args[0]), update, nil
		})}
	person.Flags().String("status", "", "active or disabled")
	person.Flags().String("limits", "", "daily override as requests,input_bytes,charge_credits")
	person.Flags().Bool("clear-limits", false, "remove the override so the defaults apply")

	sessions := &cobra.Command{Use: "sessions", Short: "List sessions, optionally for one person", Args: cobra.NoArgs,
		RunE: run(func(cmd *cobra.Command, _ []string) (string, string, any, error) {
			values := url.Values{}
			if s, _ := cmd.Flags().GetString("sub"); s != "" {
				values.Set("sub", s)
			}
			return http.MethodGet, withQuery(wire.RouteAdminSessions, values), nil, nil
		})}
	sessions.Flags().String("sub", "", "only this person's sessions")

	revoke := &cobra.Command{Use: "revoke <session-id>", Short: "Revoke one session", Args: cobra.ExactArgs(1),
		RunE: run(func(_ *cobra.Command, args []string) (string, string, any, error) {
			return http.MethodPost, strings.Replace(wire.RouteAdminRevoke, "{id}", url.PathEscape(args[0]), 1), nil, nil
		})}

	revokeAll := &cobra.Command{Use: "revoke-all <sub>", Short: "Revoke every session of one person", Args: cobra.ExactArgs(1),
		RunE: run(func(_ *cobra.Command, args []string) (string, string, any, error) {
			return http.MethodPost, sub(wire.RouteAdminRevokeAll, args[0]), nil, nil
		})}

	config := &cobra.Command{Use: "config", Short: "Show the runtime configuration, or change it with flags", Args: cobra.NoArgs,
		RunE: run(func(cmd *cobra.Command, _ []string) (string, string, any, error) {
			update, changed, err := adminConfigUpdate(cmd)
			if err != nil || !changed {
				return http.MethodGet, wire.RouteAdminConfig, nil, err
			}
			return http.MethodPatch, wire.RouteAdminConfig, update, nil
		})}
	config.Flags().String("paid-calls", "", "on or off: the kill switch for every paid call")
	config.Flags().String("voyage", "", "on or off")
	config.Flags().String("typesafe", "", "on or off")
	config.Flags().String("eligibility", "", "all or allowlist")
	config.Flags().StringSlice("allow", nil, "replace the allowlist with these emails")
	config.Flags().String("person-limits", "", "default daily per-person limits as requests,input_bytes,charge_credits")
	config.Flags().String("global-limits", "", "daily global limits as requests,input_bytes,charge_credits")

	audit := &cobra.Command{Use: "audit", Short: "Show recent admin changes", Args: cobra.NoArgs,
		RunE: run(func(cmd *cobra.Command, _ []string) (string, string, any, error) {
			values := url.Values{}
			if limit, _ := cmd.Flags().GetInt("limit"); limit > 0 {
				values.Set("limit", strconv.Itoa(limit))
			}
			return http.MethodGet, withQuery(wire.RouteAdminAudit, values), nil, nil
		})}
	audit.Flags().Int("limit", 0, "most recent entries to show (default: the gateway's)")

	command.AddCommand(usage, people, person, sessions, revoke, revokeAll, config, audit)
	return command
}

func adminConfigUpdate(cmd *cobra.Command) (wire.AdminConfigUpdate, bool, error) {
	var update wire.AdminConfigUpdate
	changed := false
	for flag, target := range map[string]**bool{"paid-calls": &update.PaidCallsEnabled, "voyage": &update.VoyageEnabled, "typesafe": &update.TypeSafeEnabled} {
		value, _ := cmd.Flags().GetString(flag)
		switch value {
		case "":
			continue
		case "on", "off":
			enabled := value == "on"
			*target, changed = &enabled, true
		default:
			return update, false, fmt.Errorf("--%s must be on or off", flag)
		}
	}
	if mode, _ := cmd.Flags().GetString("eligibility"); mode != "" {
		if mode != wire.EligibilityAll && mode != wire.EligibilityAllowlist {
			return update, false, fmt.Errorf("--eligibility must be %s or %s", wire.EligibilityAll, wire.EligibilityAllowlist)
		}
		update.EligibilityMode, changed = &mode, true
	}
	if cmd.Flags().Changed("allow") {
		allow, _ := cmd.Flags().GetStringSlice("allow")
		update.Allowlist, changed = &allow, true
	}
	for flag, target := range map[string]**wire.DailyLimits{"person-limits": &update.PersonLimits, "global-limits": &update.GlobalLimits} {
		if raw, _ := cmd.Flags().GetString(flag); raw != "" {
			limits, err := parseDailyLimits(raw)
			if err != nil {
				return update, false, fmt.Errorf("--%s: %w", flag, err)
			}
			*target, changed = &limits, true
		}
	}
	return update, changed, nil
}

func parseDailyLimits(raw string) (wire.DailyLimits, error) {
	parts := strings.Split(raw, ",")
	if len(parts) != 3 {
		return wire.DailyLimits{}, errors.New("want requests,input_bytes,charge_credits")
	}
	var values [3]int64
	for i, part := range parts {
		value, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil || value < 0 {
			return wire.DailyLimits{}, fmt.Errorf("%q is not a non-negative integer", part)
		}
		values[i] = value
	}
	return wire.DailyLimits{Requests: values[0], InputBytes: values[1], ChargeCredits: values[2]}, nil
}
