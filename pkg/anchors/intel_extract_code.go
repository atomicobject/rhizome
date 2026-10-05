package codeanchor

import (
	"path/filepath"
	"strings"
)

// extractIntelCodeFromSummary produces minimal intel anchors + edges + FTS rows for a code file.
//
// V1: spans/doc comments/signatures are best-effort and may be empty; callers should treat
// these as addressable but not yet “precisely located” within the file.
func extractIntelCodeFromSummary(intelPath string, lang Lang, summary FileSummary) ([]IntelAnchor, []IntelEdge, []IntelFTSRow) {
	fileBase := filepath.Base(intelPath)
	moduleID := intelAnchorID(lang, "module", intelPath, "module")

	// Include package-level doc comment in module anchor if available.
	moduleDoc := strings.TrimSpace(summary.PackageDoc)
	moduleBody := intelPath
	if moduleDoc != "" {
		moduleBody = intelPath + "\n\n" + moduleDoc
	}

	anchors := []IntelAnchor{{
		AnchorID:    moduleID,
		Lang:        lang,
		Kind:        "module",
		Path:        intelPath,
		Symbol:      fileBase,
		DocComment:  moduleDoc,
		Fingerprint: summary.Hash,
		StartByte:   0,
		EndByte:     0,
		StartLine:   1,
		EndLine:     1,
	}}
	edges := []IntelEdge{}
	fts := []IntelFTSRow{{
		ItemType: "anchor",
		ItemID:   moduleID,
		Path:     intelPath,
		Title:    fileBase,
		Body:     moduleBody,
	}}

	for _, sym := range summary.Symbols {
		kind := intelKindFromSymbol(sym.Kind)
		if kind == "" {
			continue
		}
		fqn := sym.NormalizeFQN()
		if fqn == "" {
			fqn = sym.Name
		}
		anchorID := intelAnchorID(lang, kind, intelPath, fqn)
		intelAnchor := IntelAnchor{
			AnchorID:    anchorID,
			Lang:        lang,
			Kind:        kind,
			Path:        intelPath,
			Symbol:      sym.Name,
			FQN:         fqn,
			Signature:   sym.Signature,
			DocComment:  sym.DocComment,
			Fingerprint: sha256HexParts(summary.Hash, fqn),
			StartByte:   sym.StartByte,
			EndByte:     sym.EndByte,
			StartLine:   sym.StartLine,
			EndLine:     sym.EndLine,
		}
		if intelAnchor.StartLine <= 0 {
			intelAnchor.StartLine = 1
		}
		if intelAnchor.EndLine <= 0 {
			intelAnchor.EndLine = intelAnchor.StartLine
		}
		anchors = append(anchors, intelAnchor)
		edges = append(edges, IntelEdge{
			SrcID: moduleID,
			DstID: anchorID,
			Kind:  "defines",
		})
		body := fqn
		if sym.Signature != "" {
			body += "\n" + sym.Signature
		}
		if sym.DocComment != "" {
			body += "\n\n" + sym.DocComment
		}
		fts = append(fts, IntelFTSRow{
			ItemType: "anchor",
			ItemID:   anchorID,
			Path:     intelPath,
			Title:    sym.Name,
			Body:     body,
		})
	}

	return anchors, edges, fts
}

func intelKindFromSymbol(kind SymbolKind) string {
	switch kind {
	case SymFunc:
		return "function"
	case SymMethod:
		return "method"
	case SymInterface:
		return "interface"
	case SymClass:
		return "class"
	case SymField:
		return "field"
	case SymStruct, SymType, SymEnum:
		return "type"
	default:
		return ""
	}
}
