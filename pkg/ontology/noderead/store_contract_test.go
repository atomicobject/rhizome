package noderead

import semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"

var _ CatalogStore = (*semdb.Store)(nil)
