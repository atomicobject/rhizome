package notemeta

import semdb "github.com/atomicobject/rhizome/pkg/anchors/sqlite"

var _ Store = (*semdb.Store)(nil)
