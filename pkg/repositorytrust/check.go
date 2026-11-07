package repositorytrust

// DenialReason explains why checkout may not run its what, or returns "".
// Callers pass the folder `rzm trust` records, never a Git-derived parent.
func DenialReason(checkout, what string, trusted func(string) (bool, error)) string {
	ok, err := trusted(checkout)
	if err != nil {
		return "repository trust could not be read: " + err.Error()
	}
	if !ok {
		return "this repository is not trusted; run `rzm trust` in " + checkout + " to let its " + what + " run"
	}
	return ""
}
