// Package retrieval contains the independently runnable evidence producers for
// the unified search pipeline.
//
// Retrievers should return Candidates with stable handles and typed Evidence.
// They may batch or degrade internally, but they should not rank globally,
// pack context, or assemble answer packets. The search service merges evidence
// by handle and the relevance package decides how much each evidence channel
// matters for the current intent.
package retrieval
