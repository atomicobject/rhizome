package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"regexp"
	"strings"
	"unicode"

	"github.com/atomicobject/rhizome/pkg/ontology"
	"github.com/atomicobject/rhizome/pkg/ontology/viewconfig"
	xhtml "golang.org/x/net/html"
)

// ViewContext is the public subject shared by built-in and authored views.
// Workspace edit sessions are transported separately and never put in URLs.
type ViewContext struct {
	Kind      viewconfig.MountKind `json:"kind"`
	Type      string               `json:"type,omitempty"`
	Interface string               `json:"interface,omitempty"`
	Group     string               `json:"group,omitempty"`
	Ref       *ontology.NodeRef    `json:"ref,omitempty"`
}

type customViewInvocation struct {
	View           customViewIdentity `json:"view"`
	Context        ViewContext        `json:"context"`
	Configuration  map[string]any     `json:"configuration,omitempty"`
	VaultKey       string             `json:"vaultKey,omitempty"`
	PreferenceSlot string             `json:"preferenceSlot,omitempty"`
}
type customViewIdentity struct {
	ID     string            `json:"id"`
	Name   string            `json:"name"`
	Origin viewconfig.Origin `json:"origin"`
}

func (s *Server) customViewInvocation(ctx context.Context, def viewconfig.ViewDefinition, raw string, hosted bool) (customViewInvocation, error) {
	c := ViewContext{Kind: def.Mount.Kind, Type: def.Mount.Type, Interface: def.Mount.Interface}
	if c.Kind == viewconfig.MountKindGroup {
		c.Group = def.Mount.Group
	}
	if c.Kind == viewconfig.MountKindStandalone || c.Kind == viewconfig.MountKindWorkspace {
		c = ViewContext{Kind: c.Kind}
	}
	if raw != "" {
		c = ViewContext{}
		decoder := json.NewDecoder(strings.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&c); err != nil {
			return customViewInvocation{}, fmt.Errorf("invalid view context: %w", err)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return customViewInvocation{}, fmt.Errorf("invalid view context: expected one JSON value")
		}
	}
	name := ""
	switch c.Kind {
	case viewconfig.MountKindStandalone, viewconfig.MountKindWorkspace:
		if c.Type != "" || c.Interface != "" || c.Group != "" || c.Ref != nil {
			return customViewInvocation{}, fmt.Errorf("%s context cannot contain a subject", c.Kind)
		}
	case viewconfig.MountKindType:
		name = c.Type
		if name == "" || c.Interface != "" || c.Group != "" || c.Ref != nil {
			return customViewInvocation{}, fmt.Errorf("type context requires only type")
		}
		if name == "*" {
			return customViewInvocation{}, fmt.Errorf("type view requires a concrete type context")
		}
	case viewconfig.MountKindInterface:
		name = c.Interface
		if name == "" || c.Type != "" || c.Group != "" || c.Ref != nil {
			return customViewInvocation{}, fmt.Errorf("interface context requires only interface")
		}
		if name == "*" {
			return customViewInvocation{}, fmt.Errorf("interface view requires a concrete interface context")
		}
	case viewconfig.MountKindGroup:
		name = c.Group
		if name == "" || name == "*" || c.Type != "" || c.Interface != "" || c.Ref != nil {
			return customViewInvocation{}, fmt.Errorf("group view requires a concrete group context")
		}
	case viewconfig.MountKindNode:
		if c.Ref == nil || !vaultRelativeNotePath(c.Ref.NotePath) || strings.TrimSpace(string(c.Ref.Kind)) == "" || strings.TrimSpace(c.Type) == "" || c.Interface != "" || c.Group != "" {
			return customViewInvocation{}, fmt.Errorf("node view requires a canonical node ref context")
		}
		if c.Ref.TypeName != "" && c.Ref.TypeName != c.Type {
			return customViewInvocation{}, fmt.Errorf("node ref type %q does not match context type %q", c.Ref.TypeName, c.Type)
		}
		// The context is caller-supplied in both modes. Keep public identity and
		// drop byte offsets and parent ids so a copied ref resolves ordinarily
		// and never reaches the view as server-asserted position.
		c.Ref = publicNodeRef(*c.Ref)
		// hosted is a lifecycle hint, never authority. The workspace already
		// resolved this identity through its edit overlay; resolving committed
		// disk again would reject staged renames and newly introduced nodes.
		// Standalone launches resolve current committed identity here instead.
		if !hosted {
			workspace, err := s.nodeWorkspace(ctx, *c.Ref, nodeWorkspaceIncludes{})
			if err != nil {
				return customViewInvocation{}, fmt.Errorf("cannot resolve view node: %w", err)
			}
			if c.Type != "" && c.Type != workspace.Node.ResolvedType {
				return customViewInvocation{}, fmt.Errorf("node context type %q does not match resolved type %q", c.Type, workspace.Node.ResolvedType)
			}
			c.Type = workspace.Node.ResolvedType
			c.Ref = publicNodeRef(workspace.Node.Ref)
		}
		if c.Type == "" {
			return customViewInvocation{}, fmt.Errorf("node context requires a resolved type")
		}
		name = c.Type
	default:
		return customViewInvocation{}, fmt.Errorf("invalid view context kind %q", c.Kind)
	}
	if !viewconfig.MatchesMount(def.Mount, c.Kind, name) {
		return customViewInvocation{}, fmt.Errorf("view %q is not available for %s %q", def.ID, c.Kind, name)
	}
	if c.Kind != viewconfig.MountKindStandalone && c.Kind != viewconfig.MountKindWorkspace {
		defs, err := s.ontologyDefinitions()
		if err != nil {
			return customViewInvocation{}, err
		}
		if defs == nil || defs.schema == nil {
			return customViewInvocation{}, fmt.Errorf("view subject ontology is unavailable")
		}
		found := false
		switch c.Kind {
		case viewconfig.MountKindType, viewconfig.MountKindNode:
			_, found = defs.schema.Types[name]
		case viewconfig.MountKindInterface:
			_, found = defs.schema.Interfaces[name]
		case viewconfig.MountKindGroup:
			_, found = viewconfig.DisplayGroups(defs.schema)[name]
		}
		if !found {
			return customViewInvocation{}, fmt.Errorf("view subject %s %q does not exist", c.Kind, name)
		}
	}
	return customViewInvocation{View: customViewIdentity{ID: def.ID, Name: def.Name, Origin: def.Origin}, Context: c, Configuration: def.Configuration, VaultKey: s.cfg.VaultPath}, nil
}

func (s *Server) customViewInvocationForRequest(r *http.Request, def viewconfig.ViewDefinition) (customViewInvocation, error) {
	invocation, err := s.customViewInvocation(r.Context(), def, r.URL.Query().Get("context"), r.URL.Query().Get("hosted") == "1")
	if err != nil {
		return customViewInvocation{}, err
	}
	slot := r.URL.Query().Get("preferenceSlot")
	if len(slot) > 256 || strings.TrimSpace(slot) != slot || strings.ContainsFunc(slot, unicode.IsControl) {
		return customViewInvocation{}, fmt.Errorf("invalid preferenceSlot")
	}
	invocation.PreferenceSlot = slot
	return invocation, nil
}

// vaultRelativeNotePath rejects traversal, absolute paths, backslashes, and
// Windows drive prefixes. Colons elsewhere stay legal (macOS filenames).
func vaultRelativeNotePath(notePath string) bool {
	return notePath != "." && fs.ValidPath(notePath) && !strings.Contains(notePath, `\`) && !windowsDrivePrefix.MatchString(notePath)
}

var windowsDrivePrefix = regexp.MustCompile(`^[A-Za-z]:(/|$)`)

func publicNodeRef(ref ontology.NodeRef) *ontology.NodeRef {
	ref.StartByte, ref.EndByte, ref.ParentID = 0, 0, ""
	return &ref
}

func invocationScript(invocation customViewInvocation) []byte {
	data, _ := json.Marshal(invocation)
	return []byte(`<script type="application/json" id="rhizome-view-invocation">` + string(data) + `</script>`)
}

func (s *Server) injectCustomHTMLContext(w http.ResponseWriter, r *http.Request, content []byte) ([]byte, bool) {
	id := r.URL.Query().Get("viewId")
	if id == "" {
		return content, true
	}
	def, err := s.findCustomView(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), customViewLookupStatus(err, http.StatusBadRequest))
		return nil, false
	}
	if viewconfig.IsScriptEntry(def.SourceSpec.Entry) || escapeURLPath(r.URL.Path) != customViewEntryURL(s, def) {
		http.Error(w, "view entry does not match viewId", http.StatusBadRequest)
		return nil, false
	}
	invocation, err := s.customViewInvocationForRequest(r, def)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return nil, false
	}
	script := invocationScript(invocation)
	// Tokenize the existing bytes so comments, attributes, and inert templates
	// cannot swallow the invocation; preserve the author's document verbatim.
	tokenizer := xhtml.NewTokenizer(bytes.NewReader(content))
	offset, templates := 0, 0
	for {
		kind := tokenizer.Next()
		if kind == xhtml.ErrorToken {
			return append(content, script...), true
		}
		token := tokenizer.Token()
		length := len(tokenizer.Raw())
		if kind == xhtml.StartTagToken && token.Data == "template" {
			templates++
		}
		if kind == xhtml.EndTagToken && token.Data == "template" && templates > 0 {
			templates--
		}
		if kind == xhtml.StartTagToken && templates == 0 && (token.Data == "head" || token.Data == "script") {
			at := offset
			if token.Data == "head" {
				at += length
			}
			return append(append(append([]byte{}, content[:at]...), script...), content[at:]...), true
		}
		offset += length
	}
}
