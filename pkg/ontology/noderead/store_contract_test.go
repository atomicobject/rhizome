package noderead

import (
	"github.com/atomicobject/rhizome/pkg/ontology/readmodel"
	semdb "github.com/atomicobject/rhizome/pkg/store"
)

var _ ReadStore = (*semdb.Store)(nil)

var _ readmodel.ShapeStore = (*semdb.Store)(nil)
var _ readmodel.RecentStore = (*semdb.Store)(nil)
