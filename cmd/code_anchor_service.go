package cmd

import (
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/codeintel"
)

type codeAnchorServiceConfig = codeintel.CodeAnchorServiceConfig

func newCodeAnchorService(cfg codeAnchorServiceConfig) (*codeanchor.Service, *codeanchor.PathTailIndex) {
	return codeintel.NewCodeAnchorService(cfg)
}
