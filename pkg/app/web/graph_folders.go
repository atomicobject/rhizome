package web

import (
	"github.com/atomicobject/rhizome/pkg/app/graphpresentation"
	"net/http"
)

func (s *Server) handleGraphFolders(w http.ResponseWriter, r *http.Request) {
	graph, err := s.buildGlobalGraphMode(r.Context(), 200000, defaultModuleDepth, false, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, graphpresentation.ProjectFolderOverview(graph))
}
