package diag

// Warning describes a runtime or planning diagnostic shared across surfaces.
type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
}
