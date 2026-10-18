package schema

import (
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
)

func TestEnumMembershipReadsDoNotMutateSchema(t *testing.T) {
	schema := &Schema{EnumTypes: map[string]*EnumType{"Status": {Values: []*EnumValue{{Name: "ACTIVE"}, {Name: "DONE"}}}}}
	var workers sync.WaitGroup
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range 100 {
				values := EnumValuesSet(schema, "Status")
				delete(values, "ACTIVE")
				if !schema.EnumTypes["Status"].HasValue("ACTIVE") {
					t.Error("reading enum membership mutated the schema")
				}
			}
		}()
	}
	workers.Wait()
	require.Nil(t, schema.EnumTypes["Status"].ByName)
	require.Equal(t, map[string]struct{}{"ACTIVE": {}, "DONE": {}}, EnumValuesSet(schema, "Status"))
}
