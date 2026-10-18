package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"strings"

	"github.com/atomicobject/rhizome/pkg/app/contextpack"
	actions "github.com/atomicobject/rhizome/pkg/app/operations"
	"github.com/atomicobject/rhizome/pkg/indexstore"
	"github.com/atomicobject/rhizome/pkg/markdownsyntax"

	"github.com/atomicobject/rhizome/pkg/paths"

	"github.com/atomicobject/rhizome/pkg/vault/frontmatter"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/atomicobject/rhizome/pkg/vault/repoconfig"
	"github.com/mark3labs/mcp-go/mcp"
)

// filesCompressIntent is the default intent for files tool compression.
// This should describe the agent's goal, not compression strategies (those are in the shared prompt).
const filesCompressIntent = "Understand the structure, APIs, and key patterns in these files. " +
	"Focus on what an agent needs to work with or extend this code."

func readMCPNoteFileContent(vaultPaths paths.VaultPaths, vaultDef obsidian.VaultDefinition, note obsidian.NoteReader, path string, primaryAuthoringPath bool) (string, error) {
	if primaryAuthoringPath {
		return note.GetContents(vaultDef, path)
	}
	rel, err := paths.CleanRelPath(path)
	if err != nil || rel == "" || vaultPaths.Root() == "" {
		return "", paths.ErrOutsideVault
	}
	abs, err := vaultPaths.Abs(rel)
	if err != nil || abs == "" {
		return "", err
	}
	content, err := os.ReadFile(abs.String())
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// FilesTool implements the files MCP tool (paths + optional content/frontmatter as JSON).
func FilesTool(config Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()
		tracker, sessionID := resolveSessionTracker(ctx, args, config, false)

		rawToken, _ := args["continuationToken"].(string)
		limit := 0
		if v, ok := args["limit"].(float64); ok && int(v) > 0 {
			limit = int(v)
		} else if v, ok := args["limit"].(int); ok && v > 0 {
			limit = v
		}

		var (
			inputs             []string
			offset             int
			maxDepth           int
			skipAnchors        bool
			skipEmbeds         bool
			includeContent     bool
			compressContent    bool
			intent             string
			includeFrontmatter bool
			absolutePaths      bool
			includeBacklinks   bool
			dedupe             bool
			budgetChars        int
			suppressTags       []string
			noSuppress         bool
		)

		if strings.TrimSpace(rawToken) != "" {
			cur, err := decodeFilesCursor(rawToken)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("invalid continuationToken: %v", err)), nil
			}
			inputs = cur.Inputs
			offset = cur.Offset
			maxDepth = cur.MaxDepth
			skipAnchors = cur.SkipAnchors
			skipEmbeds = cur.SkipEmbeds
			includeContent = cur.IncludeContent
			compressContent = cur.CompressContent
			intent = cur.Intent
			includeFrontmatter = cur.IncludeFrontmatter
			absolutePaths = cur.AbsolutePaths
			includeBacklinks = cur.IncludeBacklinks
			budgetChars = cur.BudgetChars
			suppressTags = cur.SuppressTags
			noSuppress = cur.NoSuppress
			limit = cur.Limit
			dedupe = true
			if cur.Dedupe != nil {
				dedupe = *cur.Dedupe
			}
		} else {
			rawInputs, ok := args["inputs"].([]interface{})
			if !ok {
				return mcp.NewToolResultError("inputs parameter is required and must be an array"), nil
			}

			inputs = make([]string, len(rawInputs))
			for i, v := range rawInputs {
				s, ok := v.(string)
				if !ok {
					return mcp.NewToolResultError("all inputs must be strings"), nil
				}
				inputs[i] = s
			}

			maxDepthFloat, _ := args["maxDepth"].(float64)
			maxDepth = int(maxDepthFloat)
			skipAnchors, _ = args["skipAnchors"].(bool)
			skipEmbeds, _ = args["skipEmbeds"].(bool)

			// Parse includeContent: true (default), false, or "compress"
			includeContent = true
			compressContent = false
			if v, ok := args["includeContent"].(bool); ok {
				includeContent = v
			} else if v, ok := args["includeContent"].(string); ok {
				switch strings.ToLower(v) {
				case "compress":
					includeContent = true
					compressContent = true
				case "true":
					includeContent = true
				case "false":
					includeContent = false
				}
			}
			intent, _ = args["intent"].(string)
			includeFrontmatter, _ = args["includeFrontmatter"].(bool)
			absolutePaths, _ = args["absolutePaths"].(bool)
			includeBacklinks, _ = args["includeBacklinks"].(bool)
			dedupe = true
			if v, ok := args["dedupe"].(bool); ok {
				dedupe = v
			}
			if v, ok := args["budgetChars"].(float64); ok && int(v) > 0 {
				budgetChars = int(v)
			} else if v, ok := args["budgetChars"].(int); ok && v > 0 {
				budgetChars = v
			}

			suppressTagsRaw, _ := args["suppressTags"].([]interface{})
			noSuppress, _ = args["noSuppress"].(bool)
			for _, v := range suppressTagsRaw {
				if s, ok := v.(string); ok {
					suppressTags = append(suppressTags, s)
				}
			}
		}

		if limit <= 0 {
			if includeContent {
				limit = 25
			} else {
				limit = 500
			}
		}

		baseSuppressed := config.SuppressedTags
		suppressedTags := make([]string, len(baseSuppressed))
		copy(suppressedTags, baseSuppressed)
		if noSuppress {
			suppressedTags = []string{}
		} else if len(suppressTags) > 0 {
			suppressedTags = append(suppressedTags, suppressTags...)
		}

		if config.Debug {
			log.Printf("MCP files args: inputs=%v maxDepth=%d includeContent=%v includeFrontmatter=%v", inputs, maxDepth, includeContent, includeFrontmatter)
		}

		parsedInputs, expr, err := actions.ParseInputsWithExpression(inputs)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error parsing inputs: %s", err)), nil
		}

		note := withProjectedNoteFacts(ctx, config, resolveNoteReader(config))

		store := config.GetIntelStore()
		var cleanup func()
		if store == nil && config.IntelStorePolicy == IntelStoreFallbackAllowed {
			store, cleanup, err = indexstore.Open(config.VaultPath, indexstore.Options{Access: indexstore.ReadOnly, RequireEnabled: true})
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("file index unavailable: %v", err)), nil
			}
		}
		if cleanup != nil {
			defer cleanup()
		}
		useUnifiedGraph := store != nil && maxDepth > 0

		vaultDef := config.VaultDef
		if vaultDef.Path == "" && config.Vault != nil {
			if def, err := config.Vault.Definition(); err == nil {
				vaultDef = def
			}
		}
		vaultPaths, _ := paths.NewVaultPaths(vaultDef.BasePath())

		unique := make(map[string]bool)
		order := make([]string, 0)
		fileTypes := make(map[string]string)

		addPath := func(path string, fileType string) {
			if path == "" {
				return
			}
			if !unique[path] {
				unique[path] = true
				order = append(order, path)
			}
			if _, ok := fileTypes[path]; !ok {
				fileTypes[path] = fileType
			}
		}

		listMaxDepth := maxDepth
		if useUnifiedGraph {
			listMaxDepth = 0
		}

		params := actions.ListParams{
			Inputs:                parsedInputs,
			MaxDepth:              listMaxDepth,
			SkipAnchors:           skipAnchors,
			SkipEmbeds:            skipEmbeds,
			AbsolutePaths:         false,
			Expression:            expr,
			SuppressedTags:        suppressedTags,
			SessionStore:          config.GetIntelStore(),
			MetadataStoreFallback: metadataStoreFallbackPolicy(config),
			OnMatch: func(file string) {
				addPath(file, fileTypeNote)
			},
		}

		var backlinks map[string][]obsidian.Backlink
		if includeBacklinks {
			params.IncludeBacklinks = true
			params.Backlinks = &backlinks
		}

		var primaryMatches []string
		params.PrimaryMatches = &primaryMatches

		_, err = actions.ListFiles(config.Vault, note, params)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error listing files: %s", err)), nil
		}

		seedPaths := make([]string, 0, len(primaryMatches))
		for _, p := range primaryMatches {
			normalized := normalizeDocPath(vaultPaths, p)
			if normalized == "" {
				continue
			}
			seedPaths = append(seedPaths, normalized)
		}

		if store != nil && filesHasPathInputs(parsedInputs) {
			info := actions.AnalyzeExpression(expr)
			if !info.HasOr && !info.HasNot && (info.HasTag || info.HasProperty) {
				// AND-only expression that includes tag/property: code matches are impossible.
			} else {
				var codePaths []string
				if !info.HasNot && !info.HasFind && !info.HasTag && !info.HasProperty && len(info.FileInputs) > 0 {
					codePaths, err = indexedFilePathsForPrefixes(ctx, store, info.FileInputs)
				} else {
					codePaths, err = store.IndexedFilePaths(ctx)
				}
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("Error listing code paths: %s", err)), nil
				}
				for _, codePath := range codePaths {
					normalized := string(paths.NormalizeCode(codePath))
					if normalized == "" {
						continue
					}
					if actions.MatchesExpressionForPath(expr, vaultDef, note, normalized, false) {
						addPath(normalized, fileTypeCode)
						seedPaths = append(seedPaths, normalizeDocPath(vaultPaths, normalized))
					}
				}
			}
		}

		if useUnifiedGraph {
			if err := expandFilesWithGraph(ctx, store, vaultPaths, vaultDef, note, markdownsyntax.WikilinkOptions{
				SkipAnchors: skipAnchors,
				SkipEmbeds:  skipEmbeds,
			}, seedPaths, maxDepth, unique, &order, fileTypes); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error expanding doc graph: %s", err)), nil
			}
		}
		// Primary list results come from the explicit Markdown authoring adapter.
		// Graph-provided note endpoints instead require an executable,
		// source-readable provider from the configured note-format runtime.
		primaryAuthoringPaths := make(map[string]struct{}, len(primaryMatches))
		for _, path := range primaryMatches {
			if normalized := normalizeDocPath(vaultPaths, path); normalized != "" {
				primaryAuthoringPaths[normalized] = struct{}{}
			}
		}
		filteredOwned := make([]string, 0, len(order))
		for _, path := range order {
			fileType := fileTypes[path]
			_, isPrimaryAuthoringPath := primaryAuthoringPaths[path]
			if fileType == "" || (fileType == fileTypeNote && !isPrimaryAuthoringPath && !supportsMCPProjectedNoteFileSurface(config, path)) {
				continue
			}
			filteredOwned = append(filteredOwned, path)
		}
		order = filteredOwned

		if useUnifiedGraph && len(suppressedTags) > 0 {
			notePaths := make([]string, 0)
			for _, path := range order {
				if fileTypes[path] == fileTypeNote {
					notePaths = append(notePaths, path)
				}
			}
			debugOutput := io.Discard
			if config.Debug {
				debugOutput = os.Stderr
			}
			allowedNotes := actions.FilterSuppressedFiles(notePaths, note, suppressedTags, debugOutput)
			allowedSet := make(map[string]struct{}, len(allowedNotes))
			for _, p := range allowedNotes {
				allowedSet[p] = struct{}{}
			}
			filtered := make([]string, 0, len(order))
			for _, path := range order {
				if fileTypes[path] == fileTypeNote {
					if _, ok := allowedSet[path]; !ok {
						continue
					}
				}
				filtered = append(filtered, path)
			}
			order = filtered
		}

		total := len(order)
		if offset < 0 {
			offset = 0
		}
		if offset > total {
			offset = total
		}
		end := total
		if limit > 0 && offset+limit < end {
			end = offset + limit
		}
		pageOrder := order[offset:end]

		response := FilesResponse{
			SessionID: sessionID,
			Vault:     config.Vault.Name,
			Offset:    offset,
			Total:     total,
			Files:     make([]FileEntry, 0, len(pageOrder)),
		}

		vaultPath := config.VaultPath

		remainingBudget := -1
		if includeContent && budgetChars > 0 {
			remainingBudget = budgetChars
		}
		useDedupe := dedupe && tracker != nil

		primarySet := make(map[string]struct{})
		for _, p := range primaryMatches {
			primarySet[string(paths.NormalizeNotePath(p))] = struct{}{}
		}

		for _, file := range pageOrder {
			fileType := fileTypes[file]
			if fileType == "" {
				continue
			}
			_, isPrimaryAuthoringPath := primaryAuthoringPaths[file]
			if fileType == fileTypeNote && !isPrimaryAuthoringPath && !supportsMCPProjectedNoteFileSurface(config, file) {
				// This path is still note-owned. Its provider does not make it
				// available through this content surface.
				continue
			}

			entry := FileEntry{
				Path:     file,
				FileType: fileType,
			}
			entryBudget := remainingBudget
			stopForBudget := false

			switch fileType {
			case fileTypeNote:
				if isPrimaryAuthoringPath {
					info, err := actions.GetFileInfo(config.Vault, note, file)
					if err != nil {
						if config.Debug {
							log.Printf("Unable to get info for %s: %v", file, err)
						}
					} else {
						entry.Tags = info.Tags

						if info.Frontmatter != nil {
							// Default behavior: when returning file lists without content (or when
							// traversing links), include only blessed summary-like frontmatter to
							// keep payloads small while still providing awareness.
							//
							// If the caller explicitly requests frontmatter, include it verbatim.
							if includeFrontmatter {
								entry.Frontmatter = info.Frontmatter
							} else if !includeContent || maxDepth > 0 || includeBacklinks {
								entry.Frontmatter = frontmatter.FilterBlessed(info.Frontmatter)
							}
						}
					}
				}

				if includeContent {
					content, err := readMCPNoteFileContent(vaultPaths, vaultDef, note, file, isPrimaryAuthoringPath)
					if err != nil {
						if config.Debug {
							log.Printf("Unable to read file %s: %v", file, err)
						}
						entry.ContentOmittedReason = contentOmittedReadError
					} else {
						candidate := content
						truncated := false
						if entryBudget >= 0 && len(candidate) > entryBudget && len(response.Files) > 0 {
							stopForBudget = true
							break
						}
						if entryBudget >= 0 && len(candidate) > entryBudget {
							candidate = contextpack.TrimToBudget(candidate, entryBudget)
							truncated = len(candidate) < len(content)
						}
						if candidate == "" {
							if len(content) > 0 && entryBudget >= 0 {
								entry.ContentOmittedReason = contentOmittedBudget
							}
						} else {
							key := "note:" + string(paths.NormalizeNotePath(file))
							fp := fingerprintText(candidate)
							if useDedupe {
								if tracker.Allow(key, fp) {
									entry.Content = candidate
									entry.ContentTruncated = truncated
									if remainingBudget >= 0 {
										remainingBudget -= len(candidate)
									}
									tracker.MarkSent(key, fp)
								} else {
									entry.ContentOmittedReason = contentOmittedDeduped
								}
							} else {
								entry.Content = candidate
								entry.ContentTruncated = truncated
								if remainingBudget >= 0 {
									remainingBudget -= len(candidate)
								}
								if tracker != nil {
									tracker.MarkSent(key, fp)
								}
							}
						}
					}
				}
			case fileTypeCode:
				if includeContent && vaultPaths.Root() != "" {
					_, abs, err := paths.ResolveCodeInputWithVaultPaths(vaultPaths, file)
					if err != nil {
						if config.Debug {
							log.Printf("Unable to resolve code path %s: %v", file, err)
						}
						entry.ContentOmittedReason = contentOmittedReadError
					} else if body, err := os.ReadFile(abs.String()); err != nil {
						if config.Debug {
							log.Printf("Unable to read code file %s: %v", file, err)
						}
						entry.ContentOmittedReason = contentOmittedReadError
					} else {
						raw := string(body)
						candidate := raw
						truncated := false
						if entryBudget >= 0 && len(candidate) > entryBudget && len(response.Files) > 0 {
							stopForBudget = true
							break
						}
						if entryBudget >= 0 && len(candidate) > entryBudget {
							candidate = contextpack.TrimToBudget(candidate, entryBudget)
							truncated = len(candidate) < len(raw)
						}
						if candidate == "" {
							if len(raw) > 0 && entryBudget >= 0 {
								entry.ContentOmittedReason = contentOmittedBudget
							}
						} else {
							key := "code:" + string(paths.NormalizeCode(file))
							fp := fingerprintText(candidate)
							if useDedupe {
								if tracker.Allow(key, fp) {
									entry.Content = candidate
									entry.ContentTruncated = truncated
									if remainingBudget >= 0 {
										remainingBudget -= len(candidate)
									}
									tracker.MarkSent(key, fp)
								} else {
									entry.ContentOmittedReason = contentOmittedDeduped
								}
							} else {
								entry.Content = candidate
								entry.ContentTruncated = truncated
								if remainingBudget >= 0 {
									remainingBudget -= len(candidate)
								}
								if tracker != nil {
									tracker.MarkSent(key, fp)
								}
							}
						}
					}
				}
			}

			if stopForBudget {
				break
			}

			if absolutePaths {
				if vaultPaths.Root() != "" {
					switch fileType {
					case fileTypeNote:
						if abs, err := vaultPaths.AbsNotePath(paths.NormalizeNotePath(file)); err == nil && abs != "" {
							entry.AbsolutePath = abs.String()
						}
					case fileTypeCode:
						if abs, err := vaultPaths.AbsCode(paths.NormalizeCode(file)); err == nil && abs != "" {
							entry.AbsolutePath = abs.String()
						}
					}
				}
				if entry.AbsolutePath == "" {
					entry.AbsolutePath = filepath.Join(vaultPath, file)
				}
			}

			if includeBacklinks && fileType == fileTypeNote {
				key := string(paths.NormalizeNotePath(file))
				if _, ok := primarySet[key]; ok {
					if backs, ok := backlinks[key]; ok && len(backs) > 0 {
						entry.Backlinks = backs
					} else if ok {
						entry.Backlinks = []obsidian.Backlink{}
					}

					// Include code links if available
					if config.Cache != nil {
						if codeRefsByNote := config.Cache.CodeRefsByNote(); codeRefsByNote != nil {
							if refs, ok := codeRefsByNote[key]; ok && len(refs) > 0 {
								entry.CodeLinks = buildCodeLinkPayloads(refs, config.IncludeCodeRefSnippets)
							}
						}
					}
				}
			}

			response.Files = append(response.Files, entry)

			if includeContent && remainingBudget == 0 {
				break
			}
		}

		nextOffset := offset + len(response.Files)
		response.Returned = len(response.Files)
		response.Count = response.Returned
		response.Total = total
		response.Remaining = total - nextOffset
		if nextOffset < total {
			cur := filesCursor{
				Inputs:             inputs,
				Offset:             nextOffset,
				MaxDepth:           maxDepth,
				SkipAnchors:        skipAnchors,
				SkipEmbeds:         skipEmbeds,
				IncludeContent:     includeContent,
				CompressContent:    compressContent,
				Intent:             intent,
				IncludeFrontmatter: includeFrontmatter,
				IncludeBacklinks:   includeBacklinks,
				AbsolutePaths:      absolutePaths,
				Dedupe:             &dedupe,
				SuppressTags:       suppressTags,
				NoSuppress:         noSuppress,
				BudgetChars:        budgetChars,
				Limit:              limit,
			}
			if tok, err := encodeFilesCursor(cur); err == nil {
				response.ContinuationToken = tok
			}
		}
		if tracker != nil {
			response.DedupeHits = tracker.DedupeHits()
		}

		// Handle compression when requested.
		if compressContent && config.Compressor != nil && len(response.Files) > 0 {
			// Preserve file order by encoding it as Score. contextpack sorts by
			// priority/score/key before compression, so equal-priority file payloads
			// remain stable without teaching the compressor about cursor order.
			var pieces []contextpack.Piece
			for i, entry := range response.Files {
				if entry.Content == "" {
					continue
				}
				pieces = append(pieces, contextpack.Piece{
					Key:      entry.Path,
					Priority: 100,                              // All files have equal priority
					Score:    float64(len(response.Files) - i), // Preserve order
					Text:     entry.Content,
				})
			}

			if len(pieces) > 0 {
				// Use provided intent or default
				compressIntent := intent
				if compressIntent == "" {
					compressIntent = filesCompressIntent
				}

				// Determine budget for compression
				compressBudget := budgetChars
				if compressBudget <= 0 {
					compressBudget = repoconfig.DefaultBudgetChars
				}

				// Compression is an output-format boundary for include-content=compress:
				// the files response is already selected and paged, and individual
				// contents are cleared only after a successful compressed replacement.
				text, meta := contextpack.PackWithIntent(pieces, compressBudget, contextpack.PackOptions{
					Intent:         compressIntent,
					Compressor:     config.Compressor,
					Ctx:            ctx,
					AlwaysCompress: true,
				})

				if meta.Compressed && text != "" {
					response.Text = text
					response.Compressed = true
					// Clear individual file contents since we have compressed text
					for i := range response.Files {
						response.Files[i].Content = ""
						response.Files[i].ContentTruncated = false
					}
				}
			}
		}

		encoded, err := json.Marshal(response)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error marshaling response: %s", err)), nil
		}

		return mcp.NewToolResultText(string(encoded)), nil
	}
}
