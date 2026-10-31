package version

// Version is the current rhizome version. Goreleaser overrides it at build
// time via -X so tagged releases reflect the cut version.
var Version = "v0.50.5"

// Channel is "release" in builds made by the release tooling (Goreleaser and
// make build-release set it with -X) and "dev" in every other build, such as
// make build. Development builds use the local team API gateway.
var Channel = "dev"

// Development reports whether this binary is a development build.
func Development() bool { return Channel != "release" }
