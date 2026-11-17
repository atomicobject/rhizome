package schema

import (
	"path/filepath"
	"sync"

	"github.com/atomicobject/rhizome/pkg/paths"
)

// SchemaCache retains one immutable compiled schema per repository root and
// source generation.
type SchemaCache struct {
	mu     sync.Mutex
	byRoot map[string]*Schema
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
	authored, sourceHash, err := readAuthoredSchemaFiles(vaultPath)
	if err != nil {
		return nil, err
	}
	root, err := filepath.Abs(vaultPath)
	if err != nil {
		return nil, err
	}
	pathCase := paths.CasePolicyFor(root)
	hash := schemaHash(sourceHash, pathCase)
	c.mu.Lock()
	defer c.mu.Unlock()
	if schema := c.byRoot[root]; schema != nil && schema.Hash == hash {
		return schema, nil
	}
	schema, err := compileSchema(vaultPath, authored, sourceHash, pathCase)
	if err != nil {
		return nil, err
	}
	if c.byRoot == nil {
		c.byRoot = make(map[string]*Schema)
	}
	c.byRoot[root] = schema
	return schema, nil
}
