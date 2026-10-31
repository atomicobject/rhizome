//go:build windows

package teamdev

import "os/exec"

func detach(*exec.Cmd) {}

func lock(string) (func(), error) { return nil, errUnsupported }
