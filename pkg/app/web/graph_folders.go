package web

import (
	"context"
	"net/http"
	"strings"

	"github.com/atomicobject/rhizome/pkg/app/graphpresentation"
)

func (s *Server) handleGraphFolders(w http.ResponseWriter, r *http.Request) {
	graph, err := s.buildGlobalGraphMode(r.Context(), 200000, defaultModuleDepth, false, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	overview := graphpresentation.ProjectFolderOverview(graph)
	overview.Titles = s.folderNoteTitles(r.Context(), overview.Files)
	writeJSON(w, http.StatusOK, overview)
}

// folderNoteTitles reads the indexed titles of the overview's notes, so the
// Notes sidebar's Folder panel can label notes by title (SPEC-0136 R6). The
// graph's own labels stay file names, which Explorer shows.
func (s *Server) folderNoteTitles(ctx context.Context, files []graphpresentation.GraphNode) map[string]string {
	store := s.runtime.Intel()
	if store == nil {
		return nil
	}
	notePaths := make([]string, 0, len(files))
	for _, file := range files {
		if file.Kind == "note" && file.NotePath != "" {
			notePaths = append(notePaths, file.NotePath)
		}
	}
	if len(notePaths) == 0 {
		return nil
	}
	rows, err := store.CurrentNoteMetadataRowsByPaths(ctx, notePaths)
	if err != nil {
		return nil
	}
	titles := make(map[string]string, len(rows))
	for path, row := range rows {
		if title := strings.TrimSpace(row.Title); title != "" {
			titles[path] = title
		}
	}
	return titles
}
