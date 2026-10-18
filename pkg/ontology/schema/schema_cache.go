package schema

import (
	"path/filepath"
	"sync"
)

// SchemaCache retains one immutable compiled schema per vault source generation.
type SchemaCache struct {
	mu          sync.Mutex
	byDirectory map[string]*Schema
}

var compiledSchemas SchemaCache

// LoadSchema compiles all ontology SDL files into Rhizome's runtime model.
//
// The schema hash is based on authored files, not generated prelude text, so
// snapshot dirtiness tracks user-controlled ontology changes. Treat
// ErrNoOntologyFiles as "ontology disabled"; other errors mean committed config
// is invalid and should be surfaced to callers.
// The returned compiled schema is immutable and shared until its authored hash changes.
func LoadSchema(vaultPath string) (*Schema, error) { return compiledSchemas.Load(vaultPath) }

func (c *SchemaCache) Load(vaultPath string) (*Schema, error) {
	authored, hash, err := readAuthoredSchemaFiles(vaultPath)
	if err != nil {
		return nil, err
	}
	directory, err := filepath.Abs(OntologyDir(vaultPath))
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if schema := c.byDirectory[directory]; schema != nil && schema.Hash == hash {
		return schema, nil
	}
	schema, err := compileSchema(vaultPath, authored, hash)
	if err != nil {
		return nil, err
	}
	if c.byDirectory == nil {
		c.byDirectory = make(map[string]*Schema)
	}
	c.byDirectory[directory] = schema
	return schema, nil
}
