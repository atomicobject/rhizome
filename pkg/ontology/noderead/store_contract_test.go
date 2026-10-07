package noderead

import (
	semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
)

var _ CatalogStore = (*semdb.Store)(nil)

var _ readmodel.ShapeStore = (*semdb.Store)(nil)
var _ readmodel.RecentStore = (*semdb.Store)(nil)
