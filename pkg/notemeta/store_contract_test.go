package notemeta

import semdb "github.com/atomicobject/rhizome/pkg/store"

var _ Store = (*semdb.Store)(nil)
