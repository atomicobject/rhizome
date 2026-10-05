package obsidian

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// StructuredLinkScanSnapshot is an opaque attestation of one complete source
// scan. SourceFingerprint is stable across processes; sealed proves this value
// and its ordered links came from ScanStructuredLinkSnapshot in this process.
type StructuredLinkScanSnapshot struct {
	Links             []StructuredLink `json:"links"`
	SourceFingerprint string           `json:"sourceFingerprint"`

	sealed    string
	protected []StructuredLinkSpan
}

// ScanStructuredLinkSnapshot scans content once and seals even an empty result.
func ScanStructuredLinkSnapshot(content string) StructuredLinkScanSnapshot {
	protected := markdownProtectedSpans(content)
	snapshot := StructuredLinkScanSnapshot{
		Links:             scanStructuredLinksWithProtected(content, protected),
		SourceFingerprint: StructuredLinkSourceFingerprint(content),
		protected:         protected,
	}
	snapshot.sealed = structuredLinkSnapshotSeal(snapshot)
	return snapshot
}

// StructuredLinkSourceFingerprint returns the stable identity of source bytes.
func StructuredLinkSourceFingerprint(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// Validate reports whether the snapshot and its ordered members are unchanged.
func (snapshot StructuredLinkScanSnapshot) Validate() error {
	if snapshot.SourceFingerprint == "" || snapshot.sealed == "" {
		return fmt.Errorf("structured link scan snapshot is not sealed")
	}
	for index, link := range snapshot.Links {
		if link.scanIndex != index || link.scanCount != len(snapshot.Links) || link.sourceSeal == "" {
			return fmt.Errorf("structured link scan snapshot members changed after scanning")
		}
	}
	if structuredLinkSnapshotSeal(snapshot) != snapshot.sealed {
		return fmt.Errorf("structured link scan snapshot changed after scanning")
	}
	return nil
}

// ValidatedSnapshot returns a detached copy after verifying the opaque seal.
func (snapshot StructuredLinkScanSnapshot) ValidatedSnapshot() (*StructuredLinkScanSnapshot, error) {
	if err := snapshot.Validate(); err != nil {
		return nil, err
	}
	out := snapshot
	out.Links = append([]StructuredLink(nil), snapshot.Links...)
	out.protected = append([]StructuredLinkSpan(nil), snapshot.protected...)
	return &out, nil
}

func structuredLinkSnapshotSeal(snapshot StructuredLinkScanSnapshot) string {
	type sealedLink struct {
		Link StructuredLink `json:"link"`
		Seal string         `json:"seal"`
	}
	links := make([]sealedLink, len(snapshot.Links))
	for index := range snapshot.Links {
		links[index] = sealedLink{Link: snapshot.Links[index], Seal: snapshot.Links[index].sourceSeal}
	}
	encoded, _ := json.Marshal(struct {
		SourceFingerprint string               `json:"sourceFingerprint"`
		Links             []sealedLink         `json:"links"`
		Protected         []StructuredLinkSpan `json:"protected"`
	}{SourceFingerprint: snapshot.SourceFingerprint, Links: links, Protected: snapshot.protected})
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
