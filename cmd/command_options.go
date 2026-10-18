package cmd

import (
	"github.com/atomicobject/rhizome/pkg/vault/obsidian"
	"github.com/spf13/cobra"
	"time"
)

type vaultCommandOptions struct {
	root             *cobra.Command
	agentSessionID   string
	maxDepth         int
	skipAnchors      bool
	skipEmbeds       bool
	absolutePaths    bool
	debug            bool
	includeBacklinks bool
	vaultName        string
	suppressTags     []string
	noSuppress       bool
}

type codeInvocationOptions struct {
	*vaultCommandOptions
	codePythonRoots                  []string
	codeIndexPath                    string
	codeExplainJSON                  bool
	codeDocCoverageLimit             int
	codeDocCoverageQueries           []string
	codeDocCoverageSearchLimit       int
	codeDocCoverageLangs             []string
	codeDocCoverageFallbackThreshold float64
	codeHotspotsLimit                int
	codeHotspotsLangs                []string
	codeHotspotsFallbackThreshold    float64
	codeComplexityLimit              int
	codeComplexityMinLines           int
	codeAnchorsListJSON              bool
	codeAnchorsListLimit             int
	codeAnchorsMatchJSON             bool
	codeAnchorsMatchLimit            int
	codeAnchorsMatchKind             string
	codeAnchorsMatchValue            string
	codeAnchorsValidateJSON          bool
	codeOverviewBudgetChars          int
	codeOverviewLimit                int
	codeOverviewIncludeTests         bool
	codeRationaleKinds               []string
	codeRelatednessJSON              bool
	codeRelatednessMaxRelated        int
	codeRelatednessClusterThreshold  int
	codeRelatednessIncludeTests      bool
	codeRelatednessCriticalLimit     int
	codeRelatednessHotFilesLimit     int
	codeRelatednessCriticalVerbose   bool
	codeStatsText                    bool
	codeStatsUnresolvedTop           int
	codeStatsExcludeFrom             string
}

type graphInvocationOptions struct {
	*vaultCommandOptions
	graphExcludePatterns           []string
	graphIncludePatterns           []string
	graphLimit                     int
	graphShowAll                   bool
	graphNoColor                   bool
	graphMinDegree                 int
	graphMutualOnly                bool
	graphRecencyCascade            bool
	graphTimings                   bool
	brokenLinksSkipAnchors         bool
	brokenLinksSkipEmbeds          bool
	brokenLinksIncludeImages       bool
	brokenLinksLimit               int
	brokenLinksAll                 bool
	clustersSkipAnchors            bool
	clustersSkipEmbeds             bool
	graphCommunityIncludeNeighbors bool
	graphCommunityIncludeTags      bool
	deadEndsSkipAnchors            bool
	deadEndsSkipEmbeds             bool
	deadEndsLimit                  int
	deadEndsAll                    bool
	fcBudgetChars                  int
	fcProfile                      string
	fcEnsureLinks                  string
	graphPathMaxHops               int
	staleDays                      int
	staleLimit                     int
	staleAll                       bool
	graphSkipAnchors               bool
	graphSkipEmbeds                bool
	graphSurprisesLimit            int
	graphSurprisesMinScore         float64
	vaultContextBudgetChars        int
	vaultContextProfile            string
	vaultContextGraphSummary       bool
	graphWebPath                   string
	graphWebModule                 string
	graphWebDepth                  int
	graphWebJSON                   bool
	orphansSkipAnchors             bool
	orphansSkipEmbeds              bool
}

type noteInvocationOptions struct {
	*vaultCommandOptions
	shouldAppend           bool
	shouldOverwrite        bool
	content                string
	shouldOpen             bool
	moveOverwrite          bool
	moveUpdateBacklinks    bool
	moveToFolder           string
	renameHeadingApply     bool
	renameHeadingUpgrade   string
	renameHeadingFallback  string
	propertiesMutationJSON bool
	propertiesMutationMD   bool
	propertiesDryRun       bool
	propertiesWorkers      int
	propertiesInputs       []string
	propertiesValue        string
	propertiesOverwrite    bool
	propertiesRenameTarget string
	propertiesMerge        bool
	renameNoBacklinks      bool
	renameOverwrite        bool
	tagsJSON               bool
	tagsMarkdown           bool
	tagsMatch              []string
	tagsMutationJSON       bool
	tagsMutationMD         bool
	tagsDryRun             bool
	tagsWorkers            int
	tagsInputs             []string
	tagsRenameTarget       string
}

type indexInvocationOptions struct {
	*vaultCommandOptions
	indexRebuild        bool
	indexStatus         bool
	indexVacuum         bool
	indexTimings        bool
	indexInProcess      bool
	indexExplainPath    string
	indexScopeJSON      bool
	indexScopeSkip      []string
	indexScopeRemove    []string
	indexScopeInclude   []string
	newWorktreeRunIndex func(*cobra.Command, string, obsidian.VaultDefinition) error
}

type vaultInvocationOptions struct {
	*vaultCommandOptions
	addVaultForce    bool
	addVaultRoot     string
	addVaultIncludes []string
	addVaultExcludes []string
	addVaultLinks    string
}

type initInvocationOptions struct {
	*indexInvocationOptions
	initPath           string
	initWorkflow       string
	initAgents         string
	initSearch         string
	initRefreshDocs    bool
	initIncludeIgnored []string
	initBinaryManager  string
	initEject          string
	initRestore        string
	initCheck          bool
	initAccept         bool
	initJSON           bool
	initAddons         string
	initSkip           []string
	initKeepIndexed    []string
	initSearchKeyStdin bool
}

type promptInvocationOptions struct {
	*vaultCommandOptions
	promptCompress bool
	promptBudget   int
	promptIntent   string
}

type ontologyInvocationOptions struct {
	*vaultCommandOptions
	ontologyWalkDepth          int
	ontologyWalkRelation       string
	ontologyWalkIncludeAmbient bool
}

type searchInvocationOptions struct {
	*vaultCommandOptions
	unifiedIntent       string
	unifiedSeeds        []string
	unifiedQueries      []string
	unifiedFiles        []string
	unifiedLimit        int
	unifiedPack         bool
	unifiedBudgetChars  int
	unifiedMaxPerOwner  int
	unifiedSeedLimit    int
	unifiedFast         bool
	unifiedTimeout      time.Duration
	unifiedTimings      bool
	unifiedRaw          bool
	unifiedJSON         bool
	unifiedContinuation string
}

type stopInvocationOptions struct {
	*vaultCommandOptions
	stopAll bool
}

type updateInvocationOptions struct {
	*vaultCommandOptions
	updatePinned      bool
	updateLatest      bool
	updateSetVersion  string
	updateManifestURL string
	updateYes         bool
}
