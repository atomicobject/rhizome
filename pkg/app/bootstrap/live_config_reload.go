package bootstrap

import (
	"fmt"

	"github.com/atomicobject/rhizome/pkg/embeddings"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
)

func validateLiveResourceConfiguration(current, next repoconfig.Resolved) error {
	if current.Code.IndexPath != next.Code.IndexPath || current.NoteEmbeddings.IndexPath != next.NoteEmbeddings.IndexPath || current.CodeEmbeddings.IndexPath != next.CodeEmbeddings.IndexPath {
		return fmt.Errorf("indexPath changed; restart the vault runtime to use the new index")
	}
	sameProvider := func(a, b embeddings.Config) bool {
		return a.Enabled == b.Enabled && a.Provider == b.Provider && a.Model == b.Model && a.Endpoint == b.Endpoint && a.Dimensions == b.Dimensions
	}
	if !sameProvider(current.NoteEmbeddings, next.NoteEmbeddings) || !sameProvider(current.CodeEmbeddings, next.CodeEmbeddings) {
		return fmt.Errorf("embedding provider, model, endpoint, dimensions or enablement changed; restart the vault runtime to use the new provider configuration")
	}
	return nil
}
