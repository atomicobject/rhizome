package compress

import (
	"bytes"
	"strings"
	"text/template"

	"github.com/atomicobject/rhizome/pkg/app/contextpack"
)

// sharedBestPractices contains compression principles that apply across all content types.
// This is the foundation that both intent-driven and generic prompts build on.
// Keep these instructions preservation-first: compression is allowed to densify
// and omit under pressure, but provenance/source keys are what let agents fetch
// exact raw material after a compressed answer.
const sharedBestPractices = `## How to Think About Compression

### Compression posture (preservation-first)

- Preserve information first; change representation only when needed for budget.
- Prefer denser representation over omission (bullets, short paragraph, pseudocode).
- Omit only if still over budget after compression.
- Keep tangential-but-potentially-useful details unless budget pressure forces removal.
- Never invent details. Only summarize, compress, or reformat what is present.

### The core question for every piece of information

Ask: **"If the agent doesn't have this, what could go wrong?"**

- High cost if missing → preserve fully
- Moderate cost → summarize to essentials
- Low/no cost → omit or mention existence only

### Signals that something is high-value

**Irreplaceability**: Can this be inferred from other content, names, or structure? If not, it's high-value.
- Decisions and *why* they were made (especially rejected alternatives)
- Constraints, invariants, "must" / "must not" statements
- Non-obvious behavior, edge cases, failure modes
- Warnings about what can go wrong

**Density**: Is this the most efficient representation of the information?
- Exact signatures, type definitions, API contracts → already dense, preserve exactly
- Prose explanations → often compressible to bullets or key points
- Repetitive examples → one example + pattern description
- Full code → replace with dense description or pseudocode when exact code is not essential

**Navigability**: Will the agent need to find or reference this later?
- Headings, structure, file paths → preserve for orientation
- Links and relationships between items → preserve the graph
- Location hints ("see X for details") → useful for deferred depth

### Compression techniques (use judgment on when each applies)

**Pattern consolidation**: When N items follow the same pattern, describe the pattern once and list the items.

**Tiered detail**: Full content for central items, one-line summaries for supporting items, existence-only mentions for peripheral items.

**Location hints**: For lower-priority content, note where it lives without including details: "retry logic: client.go:80-95"

**Structural preservation**: Keep headings, hierarchy, and relationships visible even when condensing content within them.

### Code vs pseudocode

- If you replace code with pseudocode, label it "Pseudocode" and preserve identifiers/signatures.
- Never drop exact signatures or type definitions if they appear in the input.

### De-duplication

- Merge semantically identical statements into one canonical phrasing.
- When merging, keep a compact Sources line listing all contributing keys.

### Source preservation (for retrieval)

Always preserve source identifiers so the agent can fetch raw material later.
- If you summarize or compress a piece, keep its key/path nearby (inline or as a Sources line).
- When merging multiple items, list all source keys once (e.g., "Sources: key1, key2").
- Prefer short, consistent source markers over verbose citations.

### What's typically safe to omit

- Content that restates what's obvious from names, types, or structure
- Verbose explanations when a bullet captures the key point
- Repetitive examples once the pattern is clear
- Setup/installation instructions, boilerplate, pleasantries`

// compressionPromptTemplate is the prompt sent to the LLM for compression.
// It includes the shared best practices and adds intent-driven prioritization.
const compressionPromptTemplate = `You are producing a token-dense summary for a coding agent. Every token must earn its place—the agent has limited context window.

## Agent's Intent
{{.Intent}}

Use this intent as your primary lens for relevance:
- Content central to accomplishing this intent → full detail
- Content that provides useful context → summarize (keep if budget allows)
- Content unrelated to this intent → omit only if needed to fit budget

Also use cues from the content itself to infer what matters. File names, headings, code structure, and relationships between items all suggest what's important. Let the content inform your judgment about what an agent working on this task would need.

## Budget
**Hard limit**: {{.Budget}} characters maximum.
Target just under the limit. Preserve details unless you must compress to fit.
Prefer denser representation over omission. Always apply light densification.

` + sharedBestPractices + `

## Content to compress ({{.Count}} items, {{.TotalChars}} chars)
{{range .Pieces}}
### {{.Key}}
{{.Text}}
{{end}}

## Output
Produce the most useful, dense summary you can for an agent with this intent. Preserve what matters, compress what's verbose, omit what's noise.
Preserve source keys/paths for every summarized or condensed item. If you consolidate, include a compact Sources line.

Output only the compressed content. It is inserted verbatim into the agent's context, so an introduction or closing remark spends budget without adding information.`

// genericPromptTemplate is used when no intent is provided.
// It uses the same shared best practices but without intent-driven prioritization.
const genericPromptTemplate = `You are producing a token-dense summary for a coding agent. Every token must earn its place—the agent has limited context window.

## Budget
**Hard limit**: {{.Budget}} characters maximum.
Target just under the limit. Preserve details unless you must compress to fit.
Prefer denser representation over omission. Always apply light densification.

` + sharedBestPractices + `

## Content to compress ({{.Count}} items, {{.TotalChars}} chars)
{{range .Pieces}}
### {{.Key}}
{{.Text}}
{{end}}

## Output
Produce the most useful, dense summary you can. Use your judgment about what an agent working with this content would need. Preserve what's irreplaceable, compress what's verbose, omit what's noise.
Preserve source keys/paths for every summarized or condensed item. If you consolidate, include a compact Sources line.

Output only the compressed content. It is inserted verbatim into the agent's context, so an introduction or closing remark spends budget without adding information.`

var (
	intentPrompt  = template.Must(template.New("intent").Parse(compressionPromptTemplate))
	genericPrompt = template.Must(template.New("generic").Parse(genericPromptTemplate))
)

// promptData holds the data for rendering a compression prompt.
type promptData struct {
	Intent     string
	Budget     int
	Count      int
	TotalChars int
	Pieces     []pieceData
}

type pieceData struct {
	Key  string
	Text string
}

// buildPrompt constructs the compression prompt from pieces and intent.
// It keeps Piece keys beside the text because compressed output is only safe for
// follow-up workflows when every summarized claim remains traceable to a source.
func buildPrompt(pieces []contextpack.Piece, budget int, intent string) (string, error) {
	// Calculate totals
	totalChars := 0
	pieceList := make([]pieceData, 0, len(pieces))
	for _, p := range pieces {
		text := strings.TrimSpace(p.Text)
		if text == "" {
			continue
		}
		totalChars += len(text)
		pieceList = append(pieceList, pieceData{
			Key:  p.Key,
			Text: text,
		})
	}

	data := promptData{
		Intent:     strings.TrimSpace(intent),
		Budget:     budget,
		Count:      len(pieceList),
		TotalChars: totalChars,
		Pieces:     pieceList,
	}

	var buf bytes.Buffer
	var err error

	if data.Intent != "" {
		err = intentPrompt.Execute(&buf, data)
	} else {
		err = genericPrompt.Execute(&buf, data)
	}

	if err != nil {
		return "", err
	}

	return buf.String(), nil
}
