package cmd

// IsOfflineDiagnosticsInvocation keeps recovery reads independent of repository
// configuration, dotenv files, and the executable selected by that repository.
func IsOfflineDiagnosticsInvocation(args []string) bool {
	return firstCommandArg(args) == "diagnostics"
}
