// Package frontmatter filters note metadata down to the small set that is safe
// to surface in lightweight retrieval stubs.
//
// Full frontmatter remains owned by note parsing and ontology projection. This
// package is only for summary-like fields that help an agent decide whether to
// fetch the full note without mirroring every user-defined property.
package frontmatter
