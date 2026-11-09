package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
)

// PublicErrorViewsUntrusted answers a request for a repository view the person
// has not trusted.
const PublicErrorViewsUntrusted = "VIEWS_UNTRUSTED"

// indexGeneration returns the server's index generation (SPEC-0136 R4) for
// the Rhizome-Generation header; empty sends no header.
// ponytail: a stub until the generation accessor lands; integration points it
// at that accessor.
var indexGeneration = func(*Server) string { return "" }

// handlePublicViewInvocation serves GET /api/v1/views/{id}/invocation: the
// invocation a host posts to an experience frame or panel frame (SPEC-0136 R2,
// R6), with the view shell's lookup, trust, mount, and context checks.
func (s *Server) handlePublicViewInvocation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writePublicError(w, http.StatusMethodNotAllowed, PublicErrorMethodNotAllowed, errors.New("method not allowed"))
		return
	}
	viewID, _, err := parsePublicViewPath(path.Clean(r.URL.Path))
	if err != nil {
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
		return
	}
	// Read before building, so a change while building makes the
	// invocation look stale rather than current.
	generation := indexGeneration(s)
	def, err := s.findCustomView(r.Context(), viewID)
	if err != nil {
		if errors.Is(err, errCustomViewUnavailable) {
			writePublicError(w, http.StatusServiceUnavailable, PublicErrorSchemaUnavailable, err)
			return
		}
		writePublicError(w, http.StatusNotFound, PublicErrorNotFound, err)
		return
	}
	if def.Origin != viewconfig.OriginBundled {
		if reason := s.viewsUntrusted(); reason != "" {
			writePublicError(w, http.StatusForbidden, PublicErrorViewsUntrusted, errors.New(reason))
			return
		}
	}
	invocation, err := s.customViewInvocationForRequest(r, def)
	if err != nil {
		writePublicError(w, http.StatusBadRequest, PublicErrorBadRequest, err)
		return
	}
	if generation != "" {
		w.Header().Set("Rhizome-Generation", generation)
	}
	writeJSON(w, http.StatusOK, invocation)
}

// checkPanelMount passes a panel view exactly when R6 resolves the context's
// target to it. An owner descriptor follows the context's ref to its owning
// note and checks that note's target; for a note itself it means Folder.
func (s *Server) checkPanelMount(ctx context.Context, def viewconfig.ViewDefinition, c ViewContext, name string) error {
	service, err := s.configuredViewsService(ctx, nil)
	if err != nil {
		return fmt.Errorf("%w: %v", errCustomViewUnavailable, err)
	}
	panel, err := service.Panel(ctx, c.Kind, name)
	if err != nil {
		return err
	}
	if panel.Kind == viewconfig.PanelOwner && c.Ref != nil && !strings.EqualFold(string(c.Ref.Kind), string(ontology.NodeKindNote)) {
		owner, err := s.nodeWorkspace(ctx, ontology.NodeRef{NotePath: c.Ref.NotePath, Kind: ontology.NodeKindNote}, nodeWorkspaceIncludes{})
		if err != nil {
			return fmt.Errorf("cannot resolve the note that owns the panel's subject: %w", err)
		}
		if panel, err = service.Panel(ctx, viewconfig.MountKindNode, owner.Node.ResolvedType); err != nil {
			return err
		}
	}
	if panel.Kind != viewconfig.PanelView || panel.ViewID != def.ID {
		return fmt.Errorf("view %q is not the panel for %s %q", def.ID, c.Kind, name)
	}
	return nil
}
