// Package noderead provides request-scoped read APIs for ontology NodeRefs.
//
// A Service owns the stable vault, schema, and store dependencies. Each caller
// creates a Scope for one request, background job, or bounded worker batch, then
// uses that scope to hydrate nodes, resolve locators, traverse relations, or
// assemble graph facts without re-reading the same catalog/projection data.
package noderead
