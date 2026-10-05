package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	actions "github.com/atomicobject/rhizome/pkg/app/cli"
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/mark3labs/mcp-go/mcp"
)

// CommunityListTool returns community summaries only.
func CommunityListTool(config Config) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()

		note := withProjectedNoteFacts(ctx, config, resolveNoteReader(config))
		analysis, err := actions.DocGraphAnalysis(ctx, config.Vault, note, actions.DocGraphAnalysisParams{
			UseConfig:             true,
			IncludeTags:           true,
			ExcludePatterns:       nil,
			IncludePatterns:       nil,
			IntelStore:            config.GetIntelStore(),
			MetadataStoreFallback: metadataStoreFallbackPolicy(config),
			WikilinkOptions:       obsidian.DefaultWikilinkOptions,
		})
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("error computing communities: %s", err)), nil
		}

		reverseNeighbors := buildReverseNeighbors(analysis.Nodes)
		communityLookup := obsidian.CommunityMembershipLookup(analysis.Communities)
		bridgeCounts := crossCommunityEdgeCounts(analysis.Nodes, reverseNeighbors, communityLookup)

		maxCommunities := 20
		if v, ok := args["maxCommunities"].(float64); ok && int(v) > 0 {
			maxCommunities = int(v)
		}
		maxTopNotes := 5
		if v, ok := args["maxTopNotes"].(float64); ok && int(v) > 0 {
			maxTopNotes = int(v)
		}

		var comms []GraphCommunityPayload
		for idx, comm := range analysis.Communities {
			if idx >= maxCommunities {
				break
			}
			size := len(comm.Nodes)
			topAuthorityPayload := authorityScoresToPayload(comm.TopAuthority, maxTopNotes, config, note)
			bucketPayload := authorityBucketsToPayload(comm.AuthorityBuckets)
			statsPayload := authorityStatsToPayload(comm.AuthorityStats)
			recencyPayload := recencyToPayload(comm.Recency)
			comms = append(comms, GraphCommunityPayload{
				ID:               comm.ID,
				Size:             size,
				FractionOfVault:  fractionOfVault(size, analysis.Stats.NodeCount),
				Nodes:            nil, // omit members from list response
				TopTags:          comm.TopTags,
				TopAuthority:     topAuthorityPayload,
				AuthorityBuckets: bucketPayload,
				AuthorityStats:   statsPayload,
				Recency:          recencyPayload,
				Anchor:           comm.Anchor,
				Density:          comm.Density,
				Bridges:          comm.Bridges,
				BridgesDetailed:  bridgePayloads(comm.Bridges, bridgeCounts),
			})
		}

		resp := CommunityListResponse{
			Communities: comms,
			Stats:       analysis.Stats,
			OrphanCount: len(analysis.Orphans),
			Orphans:     analysis.Orphans,
			Components:  componentSummariesFromWeak(analysis.WeakComponents, analysis.Nodes, analysis.Stats.NodeCount),
		}
		encoded, err := json.Marshal(resp)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("error marshaling community list: %s", err)), nil
		}
		return mcp.NewToolResultText(string(encoded)), nil
	}
}
