package presentation

import (
	"context"
	ontoschema "github.com/atomicobject/rhizome/pkg/ontology/schema"
	"testing"

	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/stretchr/testify/require"
)

func TestOntologyPackerRequiresExplicitReadDependencies(t *testing.T) {
	packer := &DefaultPacker{OntologySchema: &ontoschema.Schema{}, NoteReader: &obsidian.Note{}}

	_, err := packer.Pack(context.Background(), search.QuerySpec{}, nil)
	require.EqualError(t, err, "ontology context packing requires a note read store")

	packer.NoteStore = newPresentationOntologyIntel(t)
	_, err = packer.Pack(context.Background(), search.QuerySpec{}, nil)
	require.EqualError(t, err, "ontology context packing requires a note format runtime")
}
