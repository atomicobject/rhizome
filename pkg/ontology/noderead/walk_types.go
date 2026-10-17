package noderead

type WalkOptions struct {
	MaxDepth       int
	RelationFilter string
	IncludeAmbient bool
}

type WalkNode struct {
	Path        string
	TypeName    string
	Title       string
	Frontmatter map[string]interface{}
}

type WalkEdge struct {
	RelationName string
	Source       string
	Destination  string
	Provenance   string
	Structural   bool
}

type WalkResult struct {
	SeedType string
	Nodes    []WalkNode
	Edges    []WalkEdge
}
