module github.com/atomicobject/rhizome/services/team-api

go 1.24.0

toolchain go1.24.2

require (
	github.com/atomicobject/rhizome v0.0.0
	github.com/coreos/go-oidc/v3 v3.17.0
	github.com/go-jose/go-jose/v4 v4.1.3
	golang.org/x/oauth2 v0.35.0
)

replace github.com/atomicobject/rhizome => ../..
