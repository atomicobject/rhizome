package main

import (
	"fmt"
	"os"

	"github.com/atomicobject/rhizome/cmd"
	"github.com/atomicobject/rhizome/pkg/app/credentials"
	"github.com/atomicobject/rhizome/pkg/vault/config"
)

func main() {
	if handled, code := credentials.Capture(os.Args[1:], os.LookupEnv, os.Stdout); handled {
		os.Exit(code)
	}
	if cwd, err := os.Getwd(); err == nil && !cmd.IsOfflineDiagnosticsInvocation(os.Args[1:]) && !cmd.IsHookInvocation(os.Args[1:]) {
		if _, err := config.LoadDotEnvUpwards(cwd); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to load .env: %v\n", err)
		}
	}
	os.Exit(cmd.Execute())
}
