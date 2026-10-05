package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	codeanchor "github.com/atomicobject/rhizome/pkg/anchors"
	"github.com/atomicobject/rhizome/pkg/paths"
	"github.com/atomicobject/rhizome/pkg/search"
	"github.com/atomicobject/rhizome/pkg/search/embeddings"
	"github.com/atomicobject/rhizome/pkg/search/semantic"
)

func printUnifiedResult(w io.Writer, useColor bool, termWidth int, vaultPath string, idx int, res search.RankedResult, noteChunks []search.RankedResult, anchor *codeanchor.IntelAnchor, moduleExports []string, chunkCache map[string][]embeddings.ChunkInput) {
	label := firstNonEmpty(res.Title, res.Symbol, res.FQN, res.NoteID, res.Path, res.Handle.String())
	location := res.Path
	if res.Type == "note" && strings.TrimSpace(res.NoteID) != "" {
		location = res.NoteID
	} else if vaultPath != "" && res.Path != "" && filepath.IsAbs(res.Path) {
		location = relToVault(vaultPath, res.Path)
	} else if vaultPath != "" && res.Path != "" {
		location = relToVault(vaultPath, filepath.Join(vaultPath, res.Path))
	}
	matchLabel := reverseBreadcrumb(firstNonEmpty(res.Breadcrumb, res.Heading))

	scoreStr := fmt.Sprintf("%.1f%%", res.FinalScore*100)
	typeStr := fmt.Sprintf("[%s]", res.Type)
	typeColor := colorGray
	labelColor := colorCyan
	switch strings.ToLower(res.Type) {
	case "note", "doc_section":
		typeColor = colorCyan
		labelColor = colorCyan
	case "code":
		typeColor = colorGreen
		labelColor = colorGreen
	case "anchor":
		typeColor = colorBlue
		labelColor = colorGreen
	default:
		typeColor = colorGray
	}

	if useColor {
		scoreStr = colorYellow + scoreStr + colorReset
		typeStr = typeColor + typeStr + colorReset
		label = labelColor + label + colorReset
	}

	header := fmt.Sprintf("%2d. %s %s %s", idx, scoreStr, typeStr, label)
	if strings.TrimSpace(matchLabel) != "" {
		if useColor {
			header += " " + colorGreen + matchLabel + colorReset
		} else {
			header += " " + matchLabel
		}
	}
	if strings.TrimSpace(location) != "" {
		locPart := "(" + location + ")"
		if useColor {
			locPart = colorGray + locPart + colorReset
		}
		header += " " + locPart
	}
	fmt.Fprintln(w, header)

	printedChunk := false
	if res.Type == "doc_section" {
		chunk := res
		if chunk.ChunkIndex < 0 {
			chunk.ChunkIndex = 0
		}
		noteChunks = append([]search.RankedResult{chunk}, noteChunks...)
	}
	if (res.Type == "note" || res.Type == "doc_section") && len(noteChunks) > 0 {
		noteID := res.NoteID
		if noteID == "" {
			noteID = res.Path
		}
		printedChunk = printUnifiedNoteChunkMatches(w, useColor, vaultPath, noteID, noteChunks, chunkCache, termWidth)
	} else if (res.Type == "anchor" || (res.Type == "code" && res.AnchorID != "")) && anchor != nil && vaultPath != "" {
		if printUnifiedAnchorCode(w, useColor, vaultPath, *anchor, moduleExports) {
			return
		}
	}

	if snippet := bestSnippet(res.Evidence); snippet != "" && !printedChunk {
		printUnifiedSnippet(w, useColor, snippet)
	}
}

func printUnifiedNoteChunkMatches(w io.Writer, useColor bool, vaultPath, noteID string, chunks []search.RankedResult, cache map[string][]embeddings.ChunkInput, termWidth int) bool {
	printed := false
	for _, r := range chunks {
		scoreStr := fmt.Sprintf("%.1f%%", r.FinalScore*100)
		where := reverseBreadcrumb(firstNonEmpty(r.Breadcrumb, r.Heading))
		where = strings.TrimSpace(where)
		chunkLabel := ""
		if r.ChunkIndex >= 0 {
			chunkLabel = fmt.Sprintf("chunk %d", r.ChunkIndex)
		}

		line := "  " + scoreStr
		if chunkLabel != "" {
			line += " " + chunkLabel
		}
		if where != "" {
			line += " - " + where
		}
		if useColor {
			line = "  " + colorYellow + strings.TrimSpace(scoreStr) + colorReset
			if chunkLabel != "" {
				line += " " + colorGray + chunkLabel + colorReset
			}
			if where != "" {
				line += " " + colorGreen + where + colorReset
			}
		}
		fmt.Fprintln(w, line)

		chunkText := ""
		if vaultPath != "" && noteID != "" && r.ChunkIndex >= 0 {
			chunkText = embeddings.CoreChunkBody(embeddings.ChunkTextForPath(vaultPath, noteID, r.ChunkIndex, cache))
		}
		if chunkText == "" {
			chunkText = bestSnippet(r.Evidence)
		}
		if chunkText != "" {
			printIndentedText(w, chunkText, "      ", termWidth-6)
			printed = true
		}
	}
	return printed
}

func bestSnippet(evidence []search.Evidence) string {
	type scoredSnippet struct {
		snippet  string
		priority int
		score    float64
		typ      string
	}
	best := scoredSnippet{}

	priorityForType := func(t string) int {
		switch t {
		case "intel_fts_match":
			return 5
		case "intel_doc_match":
			return 4
		case "code_ref":
			return 3
		case "doc_link":
			return 2
		default:
			return 1
		}
	}

	for _, ev := range evidence {
		if ev.Details == nil {
			continue
		}
		snip := strings.TrimSpace(ev.Details["snippet"])
		if snip == "" {
			continue
		}
		p := priorityForType(strings.TrimSpace(ev.Type))
		cand := scoredSnippet{snippet: snip, priority: p, score: ev.RawScore, typ: ev.Type}

		if best.snippet == "" {
			best = cand
			continue
		}
		if cand.priority != best.priority {
			if cand.priority > best.priority {
				best = cand
			}
			continue
		}
		if cand.score != best.score {
			if cand.score > best.score {
				best = cand
			}
			continue
		}
		if cand.typ < best.typ {
			best = cand
		}
	}
	return best.snippet
}

func printUnifiedSnippet(w io.Writer, useColor bool, snippet string) {
	snippet = strings.TrimSpace(snippet)
	if snippet == "" {
		return
	}
	snippet = strings.ReplaceAll(snippet, "\r\n", "\n")
	snippet = strings.ReplaceAll(snippet, "\n", " ")
	snippet = strings.Join(strings.Fields(snippet), " ")

	indent := "      "
	maxWidth := getTerminalWidth()
	available := maxWidth - len(indent)
	if available < 30 {
		available = 30
	}
	if len([]rune(snippet)) > available {
		r := []rune(snippet)
		snippet = strings.TrimSpace(string(r[:available-1])) + "…"
	}

	prefix := indent + "↳ "
	if useColor {
		fmt.Fprintln(w, dimWhite+prefix+snippet+reset)
		return
	}
	fmt.Fprintln(w, prefix+snippet)
}

func printUnifiedAnchorCode(w io.Writer, useColor bool, vaultPath string, a codeanchor.IntelAnchor, exports []string) bool {
	if strings.TrimSpace(a.Path) == "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(a.Kind), "module") {
		if len(exports) == 0 {
			return false
		}
		const maxShow = 25
		more := 0
		if len(exports) > maxShow {
			more = len(exports) - maxShow
			exports = exports[:maxShow]
		}
		line := "Exports: " + strings.Join(exports, ", ")
		if more > 0 {
			line += fmt.Sprintf(" … (+%d more)", more)
		}
		printUnifiedSnippet(w, useColor, line)
		return true
	}

	vaultPaths, _ := paths.NewVaultPaths(vaultPath)
	if vaultPaths.Root() == "" {
		return false
	}
	absPath, err := vaultPaths.AbsCode(paths.NormalizeCode(a.Path))
	if err != nil || absPath == "" {
		return false
	}
	full := absPath.String()

	data, err := os.ReadFile(full)
	if err != nil {
		return false
	}
	code := strings.TrimSpace(semantic.ExtractSpan(data, a))
	if code == "" {
		return false
	}

	printUnifiedCodeBlock(w, useColor, code, int(a.StartLine), 12)
	return true
}

func printUnifiedCodeBlock(w io.Writer, useColor bool, code string, startLine int, maxLines int) {
	code = strings.ReplaceAll(code, "\r\n", "\n")
	lines := strings.Split(code, "\n")

	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
		startLine++
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return
	}

	truncated := false
	if maxLines > 0 && len(lines) > maxLines {
		lines = lines[:maxLines]
		truncated = true
	}

	indent := "      "
	termWidth := getTerminalWidth()
	prefixWidth := len(indent) + 2
	lineNoWidth := 0
	if startLine > 0 {
		lineNoWidth = 5 + 3
	}
	avail := termWidth - prefixWidth - lineNoWidth
	if avail < 20 {
		avail = 20
	}

	for i, line := range lines {
		r := []rune(line)
		if len(r) > avail {
			line = strings.TrimRight(string(r[:avail-1]), " \t") + "…"
		}
		gutter := "│ "
		if startLine > 0 {
			gutter = fmt.Sprintf("%4d │ ", startLine+i)
		}
		if useColor {
			fmt.Fprintln(w, dimWhite+indent+gutter+reset+line)
		} else {
			fmt.Fprintln(w, indent+gutter+line)
		}
	}
	if truncated {
		if useColor {
			fmt.Fprintln(w, dimWhite+indent+"…"+reset)
		} else {
			fmt.Fprintln(w, indent+"…")
		}
	}
	fmt.Fprintln(w)
}

func formatUnifiedEntryLine(useColor bool, idx int, score float64, typ, label, location, evidence string) string {
	scoreStr := fmt.Sprintf("%.1f%%", score*100)
	if strings.TrimSpace(evidence) == "" {
		evidence = ""
	} else {
		evidence = " " + evidence
	}

	typColor := ""
	switch strings.ToLower(strings.TrimSpace(typ)) {
	case "anchor":
		typColor = orange
	case "code":
		typColor = green
	case "note", "doc_section":
		typColor = teal
	case "file":
		typColor = dimWhite
	default:
		typColor = dimWhite
	}

	if !useColor {
		return fmt.Sprintf("%2d. %s [%s] %s (%s)%s", idx, scoreStr, typ, label, location, evidence)
	}

	scoreStr = lightGreen + scoreStr + reset
	typeStr := typColor + "[" + typ + "]" + reset
	locStr := dimWhite + "(" + location + ")" + reset
	evStr := ""
	if evidence != "" {
		evStr = " " + dimWhite + strings.TrimSpace(evidence) + reset
	}
	return fmt.Sprintf("%2d. %s %s %s %s%s", idx, scoreStr, typeStr, label, locStr, evStr)
}
