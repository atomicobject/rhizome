// Candidate ingestion provides bounded parallel reads of preclassified file
// candidates. Ownership discovery supplies selection; a real worker queue
// bounds intake and records queue pressure without an independent root walk.
package codeintel
