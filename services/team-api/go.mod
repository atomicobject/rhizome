module github.com/atomicobject/rhizome/services/team-api

go 1.24.0

toolchain go1.24.2

require (
	github.com/atomicobject/rhizome v0.0.0
	github.com/coreos/go-oidc/v3 v3.17.0
	github.com/go-jose/go-jose/v4 v4.1.3
	github.com/mattn/go-sqlite3 v1.14.48
	golang.org/x/oauth2 v0.35.0
)

require (
	golang.org/x/sys v0.36.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/atomicobject/rhizome => ../..
