package cmd

import (
	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/app/bootstrap"
)

type codeAnchorServiceConfig = bootstrap.CodeAnchorServiceConfig

func newCodeAnchorService(cfg codeAnchorServiceConfig) (*codeanchor.Service, *codeanchor.PathTailIndex) {
	return bootstrap.NewCodeAnchorService(cfg)
}
