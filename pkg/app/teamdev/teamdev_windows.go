//go:build windows

package teamdev

import (
	"errors"
	"os/exec"
)

var errUnsupported = errors.New("starting the local team API gateway is supported on macOS and Linux only")

func detach(*exec.Cmd) {}

func lock(string) (func(), error) { return nil, errUnsupported }
