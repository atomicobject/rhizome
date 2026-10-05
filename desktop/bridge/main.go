package main

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"os/signal"

	"github.com/atomicobject/rhizome/pkg/app/desktop"
)

func main() {
	stateDir := flag.String("state-dir", "", "absolute desktop application data directory")
	flag.Parse()
	service, err := desktop.New(*stateDir)
	if err != nil {
		_ = json.NewEncoder(os.Stdout).Encode(desktop.Response{Protocol: desktop.Protocol, Error: &desktop.Problem{Code: "invalid_request", Message: err.Error()}})
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := desktop.Serve(ctx, service, os.Stdin, os.Stdout); err != nil {
		os.Exit(1)
	}
}
