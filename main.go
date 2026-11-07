package main

import (
	"fmt"
	"os"

	"github.com/atomicobject/rhizome/cmd"
	"github.com/atomicobject/rhizome/pkg/app/appbundle"
	"github.com/atomicobject/rhizome/pkg/app/credentials"
	"github.com/atomicobject/rhizome/pkg/app/repoexec"
	"github.com/atomicobject/rhizome/pkg/teamapi/session"
	"github.com/atomicobject/rhizome/pkg/vault/config"
)

func main() {
	if handled, code := appbundle.DispatchEntry(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); handled {
		os.Exit(code)
	}
	if handled, code := credentials.Capture(os.Args[1:], os.LookupEnv, os.Stdout); handled {
		os.Exit(code)
	}
	if cwd, err := os.Getwd(); err == nil && !cmd.IsOfflineDiagnosticsInvocation(os.Args[1:]) && !cmd.IsHookInvocation(os.Args[1:]) && !cmd.IsAuthInvocation(os.Args[1:]) && !cmd.IsDesktopInvocation(os.Args[1:]) {
		// A repository's dotenv must not choose the team API origin or the
		// trust-exempt Rhizome Dev executable (its override, or the home that
		// locates it): only the person's own environment may set them.
		if _, err := config.LoadDotEnvUpwardsKeeping(cwd, session.OriginEnv, repoexec.DevAppEnv, "HOME"); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to load .env: %v\n", err)
		}
	}
	os.Exit(cmd.Execute())
}
