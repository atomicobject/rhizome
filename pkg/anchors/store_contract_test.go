package codeanchor_test

import (
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	semdb "github.com/atomicobject/rhizome/pkg/store"
)

var _ codeanchor.AnchorNoteStore = (*semdb.Store)(nil)
