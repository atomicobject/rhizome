// Package structuralfixture supplies syntax facts without a parser or mutation engine.
package structuralfixture

import (
	"fmt"
	"sync/atomic"

	"github.com/atomicobject/rhizome/pkg/noteformat"
)

type Provider struct {
	Facts *noteformat.StructureFacts
	Links []noteformat.UnresolvedAuthoredLinkFact
	Calls atomic.Int64
}

func New(input any) (*Provider, error) {
	value, err := noteformat.NewMetadataValue(input)
	if err != nil {
		return nil, err
	}
	facts := &noteformat.StructureFacts{Root: "root"}
	id := facts.AddMetadataValue(value)
	var walk func(noteformat.ValueID, noteformat.OwnerKey, string, int)
	walk = func(id noteformat.ValueID, parent noteformat.OwnerKey, address string, ordinal int) {
		value := facts.Values[id-1]
		key := noteformat.OwnerKey("value:" + address)
		shape := noteformat.OwnerScalar
		if value.Kind == noteformat.MetadataObject {
			shape = noteformat.OwnerMapping
		}
		if value.Kind == noteformat.MetadataArray {
			shape = noteformat.OwnerSequence
		}
		if parent == "" {
			key, shape = "root", noteformat.OwnerDocument
		}
		facts.Owners = append(facts.Owners, noteformat.OwnerFact{Key: key, Parent: parent, Collection: "values", Ordinal: ordinal, Order: len(facts.Owners), Shape: shape, Address: address, Value: id})
		for i, entry := range value.Entries {
			walk(entry.Value, key, address+"/"+fmt.Sprint(entry.Key.Export()), i)
		}
		for i, item := range value.Items {
			walk(item, key, fmt.Sprintf("%s/%d", address, i), i)
		}
	}
	walk(id, "", "", 0)
	if err := noteformat.ValidateStructureFacts(facts, -1); err != nil {
		return nil, err
	}
	return &Provider{Facts: facts}, nil
}

func (*Provider) Descriptor() noteformat.Descriptor {
	return noteformat.Descriptor{ID: "synthetic", Extensions: []string{".native"}, ProviderVersion: "test-v1", ProjectionVersion: "test-v1", OwnershipPolicy: noteformat.OwnershipDefault, Capabilities: noteformat.MustCapabilities(noteformat.CapabilitySourceReading, noteformat.CapabilityStructuralReading)}
}
func (p *Provider) Project(noteformat.AuthoredSource) (noteformat.Projection, error) {
	p.Calls.Add(1)
	d := p.Descriptor()
	return noteformat.NewProjectionWithFacts(d.ProviderVersion, d.ProjectionVersion, noteformat.ProjectionStatusCurrent, nil, d.Capabilities, noteformat.ProjectionFacts{Title: &noteformat.TitleFact{Value: "Native host"}, Structure: p.Facts.Copy(), Links: p.Links})
}
