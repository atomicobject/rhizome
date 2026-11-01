//go:build !unix

package main

// lockDataDir is a no-op where flock is unavailable; the gateway is deployed
// on Linux and developed on macOS.
func lockDataDir(string) (func(), error) { return func() {}, nil }
