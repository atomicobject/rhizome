package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"time"

	"github.com/atomicobject/rhizome/pkg/app/teamdev"
	"github.com/atomicobject/rhizome/pkg/teamapi/login"
	"github.com/atomicobject/rhizome/pkg/teamapi/session"
	"github.com/atomicobject/rhizome/pkg/teamapi/wire"
	"github.com/atomicobject/rhizome/pkg/vault/version"
	"github.com/spf13/cobra"
)

// IsAuthInvocation keeps `rzm auth` on the invoked executable without
// repository dotenv: the session origin, store, and transport must not be
// influenced by the repository the command runs in.
func IsAuthInvocation(args []string) bool { return firstCommandArg(args) == "auth" }

const loginTimeout = 10 * time.Minute

// authDeps are the trusted, user-level inputs of the auth commands. Tests
// substitute every one; production derives them from the real OS user.
type authDeps struct {
	version      string
	development  bool
	getenv       func(string) string
	store        func() (session.Store, error)
	lockPath     func() (string, error)
	selectorPath func() (string, error)
	base         http.RoundTripper
	opener       func(string) error
}

func productionAuthDeps() authDeps {
	return authDeps{
		version: version.Version, development: version.Development(), getenv: os.Getenv, store: session.DefaultStore,
		lockPath: session.DefaultLockPath, selectorPath: session.DefaultSelectorPath, opener: openBrowser,
	}
}

func registerAuthFamily(rootCmd *cobra.Command) {
	rootCmd.AddCommand(newAuthCommand(productionAuthDeps()))
}

func newAuthCommand(deps authDeps) *cobra.Command {
	command := &cobra.Command{
		Use:   "auth",
		Short: "Sign in to the Atomic team API gateway for Voyage and Jev without provider keys",
	}
	command.PersistentFlags().String("server", "", "gateway origin, such as https://team.example.com (default: the origin you last signed in to)")
	command.PersistentFlags().Bool("json", false, "write JSON")
	command.AddCommand(newAuthLoginCommand(deps), newAuthStatusCommand(deps), newAuthLogoutCommand(deps), newAuthAdminCommand(deps))
	// Errors are reported once, as JSON with --json or by Execute otherwise,
	// including argument and flag errors that Cobra raises before RunE.
	var silence func(*cobra.Command)
	silence = func(c *cobra.Command) {
		c.SilenceUsage, c.SilenceErrors = true, true
		if args := c.Args; args != nil {
			c.Args = func(cmd *cobra.Command, a []string) error {
				if err := args(cmd, a); err != nil {
					return authFail(cmd, err)
				}
				return nil
			}
		}
		c.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error { return authFail(cmd, err) })
		for _, child := range c.Commands() {
			silence(child)
		}
	}
	silence(command)
	return command
}

// origin resolves --server, then the development override, the selector, and
// the build's default. "" means nothing applies.
func (deps authDeps) origin(cmd *cobra.Command) (string, error) {
	server, _ := cmd.Flags().GetString("server")
	store, _ := deps.store()
	selector, err := deps.selectorPath()
	if err != nil {
		return "", err
	}
	return session.ResolveOrigin(session.ResolveOptions{Server: server, Development: deps.development, Getenv: deps.getenv, Store: store, SelectorPath: selector})
}

func (deps authDeps) account(origin string) (*session.Account, error) {
	store, err := deps.store()
	if err != nil {
		return nil, err
	}
	lock, err := deps.lockPath()
	if err != nil {
		return nil, err
	}
	selector, err := deps.selectorPath()
	if err != nil {
		return nil, err
	}
	return session.Open(session.Options{Origin: origin, Store: store, LockPath: lock, SelectorPath: selector,
		BaseTransport: deps.base, UserAgent: "rzm/" + deps.version})
}

// requiredAccount opens the resolved account or explains how to choose one.
func (deps authDeps) requiredAccount(cmd *cobra.Command) (*session.Account, error) {
	origin, err := deps.origin(cmd)
	if err != nil {
		return nil, err
	}
	if origin == "" {
		return nil, errors.New("no team API origin is known: pass --server URL")
	}
	return deps.account(origin)
}

func newAuthLoginCommand(deps authDeps) *cobra.Command {
	var noBrowser bool
	var port int
	var deviceName string
	command := &cobra.Command{
		Use:   "login",
		Short: "Sign in with Google in the browser and store the session for this OS user",
		Long: `Sign in with Google in the browser and store the session for this OS user.

Over SSH, pass --no-browser --port <p>, forward the port (ssh -L p:127.0.0.1:p),
and open the printed URL in a local browser.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			asJSON, _ := cmd.Flags().GetBool("json")
			out := cmd.OutOrStdout()
			fail := func(err error) error {
				if asJSON {
					writeJSONLine(cmd, map[string]string{"event": "error", "code": authErrorCode(err), "message": err.Error()})
					return silentExitError{code: 1}
				}
				return err
			}
			if port < 0 || port > 65535 {
				return fail(fmt.Errorf("--port must be between 0 and 65535"))
			}
			account, err := deps.requiredAccount(cmd)
			if err != nil {
				return fail(err)
			}
			if deps.development && account.Origin() == session.DevelopmentOrigin {
				if err := teamdev.Ensure(cmd.Context(), account.Origin()); err != nil {
					return fail(err)
				}
			}
			if deviceName == "" {
				deviceName, _ = os.Hostname()
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), loginTimeout)
			defer cancel()
			status, err := login.Run(ctx, login.Options{
				Account: account, Opener: deps.opener, NoBrowser: noBrowser, Port: port, DeviceName: deviceName,
				HTTPClient: session.NewClient(deps.base), UserAgent: "rzm/" + deps.version,
				Out: func(e login.Event) {
					if asJSON {
						writeJSONLine(cmd, map[string]string{"event": e.Name, "url": e.URL})
					} else {
						fmt.Fprintf(out, "Open this URL to sign in:\n\n  %s\n\n", e.URL)
					}
				},
			})
			if err != nil {
				return fail(err)
			}
			if asJSON {
				writeJSONLine(cmd, map[string]string{"event": "signed_in", "email": status.Email, "origin": status.Origin,
					"session_expires_at": status.SessionExpiresAt.UTC().Format(time.RFC3339)})
				return nil
			}
			fmt.Fprintf(out, "Signed in to %s as %s. The session expires %s.\n", status.Origin, status.Email, status.SessionExpiresAt.Local().Format(time.RFC1123))
			return nil
		},
	}
	command.Flags().BoolVar(&noBrowser, "no-browser", false, "print the sign-in URL instead of opening a browser")
	command.Flags().IntVar(&port, "port", 0, "loopback port for the browser redirect (default: any free port)")
	command.Flags().StringVar(&deviceName, "device-name", "", "name for this device in the session list (default: host name)")
	return command
}

func newAuthStatusCommand(deps authDeps) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the signed-in team account without printing tokens",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			origin, err := deps.origin(cmd)
			if err != nil {
				return authFail(cmd, err)
			}
			status := session.Status{Origin: origin}
			if origin != "" {
				account, err := deps.account(origin)
				if err != nil {
					return authFail(cmd, err)
				}
				if status, err = account.Status(); err != nil {
					return authFail(cmd, err)
				}
			}
			if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
				if !status.SignedIn {
					return writeAgentPayload(cmd, map[string]any{"signed_in": false, "origin": status.Origin})
				}
				return writeAgentPayload(cmd, map[string]any{"signed_in": true, "email": status.Email, "origin": status.Origin,
					"session_expires_at": status.SessionExpiresAt.UTC().Format(time.RFC3339)})
			}
			switch {
			case status.SignedIn:
				fmt.Fprintf(cmd.OutOrStdout(), "Signed in to %s as %s. The session expires %s.\n", status.Origin, status.Email, status.SessionExpiresAt.Local().Format(time.RFC1123))
			case origin == "":
				fmt.Fprintln(cmd.OutOrStdout(), "Not signed in. Run `rzm auth login --server URL`.")
			default:
				fmt.Fprintf(cmd.OutOrStdout(), "Not signed in to %s. Run `rzm auth login`.\n", status.Origin)
			}
			return nil
		},
	}
}

func newAuthLogoutCommand(deps authDeps) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Revoke the team session on the gateway and remove it from this computer",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			origin, err := deps.origin(cmd)
			if err != nil {
				return authFail(cmd, err)
			}
			revoked := true
			if origin != "" {
				account, err := deps.account(origin)
				if err != nil {
					return authFail(cmd, err)
				}
				if revoked, err = account.Logout(cmd.Context()); err != nil {
					return authFail(cmd, err)
				}
			}
			if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
				return writeAgentPayload(cmd, map[string]any{"signed_out": true, "remote_revoked": revoked})
			}
			if revoked {
				fmt.Fprintln(cmd.OutOrStdout(), "Signed out.")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Signed out on this computer. The gateway could not be reached, so the session is not yet revoked there; it expires on its own, or an admin can revoke it.")
			}
			return nil
		},
	}
}

// authFail reports an error as {"error":{"code","message"}} on stdout with
// --json, or as an ordinary command error.
func authFail(cmd *cobra.Command, err error) error {
	// A flag error can stop parsing before --json is read, so the raw
	// arguments are checked too.
	asJSON, _ := cmd.Flags().GetBool("json")
	if asJSON || slices.Contains(os.Args[1:], "--json") || slices.Contains(os.Args[1:], "--json=true") {
		writeJSONLine(cmd, map[string]any{"error": map[string]string{"code": authErrorCode(err), "message": err.Error()}})
		return silentExitError{code: 1}
	}
	return err
}

func authErrorCode(err error) string {
	var gerr *wire.GatewayError
	switch {
	case errors.As(err, &gerr):
		return string(gerr.Code)
	case errors.Is(err, wire.ErrSignInRequired):
		return "sign_in_required"
	case errors.Is(err, session.ErrNewerRecord):
		return "newer_client_record"
	case errors.Is(err, session.ErrStoreUnavailable):
		return "secure_store_unavailable"
	case errors.Is(err, session.ErrSuperseded):
		return "superseded"
	case errors.Is(err, login.ErrDenied):
		return "denied"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timed_out"
	default:
		return "error"
	}
}

func writeJSONLine(cmd *cobra.Command, payload any) {
	encoded, _ := json.Marshal(payload)
	_, _ = cmd.OutOrStdout().Write(append(encoded, '\n'))
}
