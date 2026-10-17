package cmd

import "github.com/spf13/cobra"

func registerAgentFamily(rootCmd *cobra.Command) {
	commandOptions := &vaultCommandOptions{root: rootCmd}
	agentCmd := commandOptions.makeAgentCmd()
	commandOptions.registerAgentCommands(agentCmd, rootCmd)
}

func registerCodeFamily(rootCmd *cobra.Command) {
	commandOptions := &codeInvocationOptions{vaultCommandOptions: &vaultCommandOptions{root: rootCmd}}
	codeCmd := commandOptions.makeCodeCmd()
	codeContextCmd := commandOptions.makeCodeContextCmd()
	codeValidateCmd := commandOptions.makeCodeValidateCmd()
	codeStatusCmd := commandOptions.makeCodeStatusCmd()
	codeEnableCmd := commandOptions.makeCodeEnableCmd()
	codeDisableCmd := commandOptions.makeCodeDisableCmd()
	codeSymbolsCmd := commandOptions.makeCodeSymbolsCmd()
	codeDocCoverageCmd := commandOptions.makeCodeDocCoverageCmd()
	codeHotspotsCmd := commandOptions.makeCodeHotspotsCmd()
	codeComplexityCmd := commandOptions.makeCodeComplexityCmd()
	codeAnchorsCmd := commandOptions.makeCodeAnchorsCmd()
	codeAnchorsExplainCmd := commandOptions.makeCodeAnchorsExplainCmd()
	codeAnchorsListCmd := commandOptions.makeCodeAnchorsListCmd()
	codeAnchorsMatchCmd := commandOptions.makeCodeAnchorsMatchCmd()
	codeAnchorsValidateCmd := commandOptions.makeCodeAnchorsValidateCmd()
	codeOverviewCmd := commandOptions.makeCodeOverviewCmd()
	codeRationaleCmd := commandOptions.makeCodeRationaleCmd()
	codeRelatednessCmd := commandOptions.makeCodeRelatednessCmd()
	codeStatsCmd := commandOptions.makeCodeStatsCmd()
	commandOptions.registerCodeCommands(codeCmd, codeContextCmd, codeValidateCmd, codeAnchorsCmd, codeAnchorsListCmd, codeAnchorsExplainCmd, codeAnchorsMatchCmd, codeAnchorsValidateCmd, codeStatusCmd, codeStatsCmd, codeEnableCmd, codeDisableCmd, codeSymbolsCmd, codeDocCoverageCmd, codeHotspotsCmd, codeComplexityCmd, rootCmd)
	commandOptions.registerCodeOverviewCommands(codeOverviewCmd, codeCmd)
	commandOptions.registerCodeRationaleCommands(codeRationaleCmd, codeCmd)
	commandOptions.registerCodeRelatednessCommands(codeRelatednessCmd, codeCmd)
}

func registerGraphFamily(rootCmd *cobra.Command) {
	commandOptions := &graphInvocationOptions{vaultCommandOptions: &vaultCommandOptions{root: rootCmd}}
	graphCmd := commandOptions.makeGraphCmd()
	brokenLinksCmd := commandOptions.makeBrokenLinksCmd()
	graphClustersCmd := commandOptions.makeGraphClustersCmd()
	graphCommunitiesCmd := commandOptions.makeGraphCommunitiesCmd()
	graphCommunityCmd := commandOptions.makeGraphCommunityCmd()
	deadEndsCmd := commandOptions.makeDeadEndsCmd()
	graphFileContextCmd := commandOptions.makeGraphFileContextCmd()
	graphIgnoreCmd := commandOptions.makeGraphIgnoreCmd()
	graphPathCmd := commandOptions.makeGraphPathCmd()
	staleCmd := commandOptions.makeStaleCmd()
	graphStatsCmd := commandOptions.makeGraphStatsCmd()
	graphSurprisesCmd := commandOptions.makeGraphSurprisesCmd()
	graphVaultContextCmd := commandOptions.makeGraphVaultContextCmd()
	graphWebCmd := commandOptions.makeGraphWebCmd()
	orphansCmd := commandOptions.makeOrphansCmd()
	commandOptions.registerGraphCommands(graphCmd, rootCmd)
	commandOptions.registerGraphBrokenLinksCommands(brokenLinksCmd, graphCmd)
	commandOptions.registerGraphClustersCommands(graphClustersCmd, graphCmd)
	commandOptions.registerGraphCommunitiesCommands(graphCmd, graphCommunitiesCmd)
	commandOptions.registerGraphCommunityCommands(graphCommunityCmd, graphCmd)
	commandOptions.registerGraphDeadEndsCommands(deadEndsCmd, graphCmd)
	commandOptions.registerGraphFileContextCommands(graphFileContextCmd, graphCmd)
	commandOptions.registerGraphIgnoreCommands(graphIgnoreCmd, graphCmd)
	commandOptions.registerGraphPathCommands(graphPathCmd, graphCmd)
	commandOptions.registerGraphStaleCommands(staleCmd, graphCmd)
	commandOptions.registerGraphStatsCommands(graphStatsCmd, graphCmd)
	commandOptions.registerGraphSurprisesCommands(graphSurprisesCmd, graphCmd)
	commandOptions.registerGraphVaultContextCommands(graphVaultContextCmd, graphCmd)
	commandOptions.registerGraphWebCommands(graphCmd, graphWebCmd)
	commandOptions.registerOrphansCommands(orphansCmd, graphCmd)
}

func registerNoteFamily(rootCmd *cobra.Command) {
	commandOptions := &noteInvocationOptions{vaultCommandOptions: &vaultCommandOptions{root: rootCmd}, moveUpdateBacklinks: true, propertiesMerge: true}
	createNoteCmd := commandOptions.makeCreateNoteCmd()
	noteDailyCmd := commandOptions.makeNoteDailyCmd()
	deleteCmd := commandOptions.makeDeleteCmd()
	infoCmd := commandOptions.makeInfoCmd()
	moveCmd := commandOptions.makeMoveCmd()
	noteCmd := commandOptions.makeNoteCmd()
	renameHeadingCmd := commandOptions.makeRenameHeadingCmd()
	OpenVaultCmd := commandOptions.makeOpenVaultCmd()
	printCmd := commandOptions.makePrintCmd()
	renameCmd := commandOptions.makeRenameCmd()
	noteFindCmd := commandOptions.makeNoteFindCmd()
	commandOptions.registerCreateCommands(createNoteCmd, noteCmd)
	commandOptions.registerDailyCommands(noteDailyCmd, noteCmd)
	commandOptions.registerDeleteCommands(deleteCmd, noteCmd)
	commandOptions.registerInfoCommands(infoCmd, noteCmd)
	commandOptions.registerMoveCommands(moveCmd, noteCmd)
	commandOptions.registerNoteCommands(rootCmd, noteCmd)
	commandOptions.registerNoteRenameHeadingCommands(renameHeadingCmd, noteCmd)
	commandOptions.registerOpenCommands(OpenVaultCmd, noteCmd)
	commandOptions.registerPrintCommands(printCmd, noteCmd)
	commandOptions.registerPropertiesCommands(noteCmd)
	commandOptions.registerRenameCommands(renameCmd, noteCmd)
	commandOptions.registerSearchCommands(noteFindCmd, noteCmd)
	commandOptions.registerTagsCommands(noteCmd)
}

func registerIndexFamily(rootCmd *cobra.Command) {
	commandOptions := newIndexInvocationOptions(rootCmd)
	indexCmd := commandOptions.makeIndexCmd()
	indexScopeCmd := commandOptions.makeIndexScopeCmd()
	newWorktreeCmd := commandOptions.makeNewWorktreeCmd()
	commandOptions.registerIndexCommands(indexCmd, rootCmd)
	commandOptions.registerIndexScopeCommands(indexScopeCmd, indexCmd)
	commandOptions.registerNewWorktreeCommands(rootCmd, newWorktreeCmd)
}

func registerVaultFamily(rootCmd *cobra.Command) {
	commandOptions := &vaultInvocationOptions{vaultCommandOptions: &vaultCommandOptions{root: rootCmd}}
	addVaultCmd := commandOptions.makeAddVaultCmd()
	installIgnoreCmd := commandOptions.makeInstallIgnoreCmd()
	listVaultsCmd := commandOptions.makeListVaultsCmd()
	printDefaultCmd := commandOptions.makePrintDefaultCmd()
	removeVaultCmd := commandOptions.makeRemoveVaultCmd()
	setDefaultCmd := commandOptions.makeSetDefaultCmd()
	vaultCmd := commandOptions.makeVaultCmd()
	commandOptions.registerAddVaultCommands(addVaultCmd, vaultCmd)
	commandOptions.registerIgnoreCommands(installIgnoreCmd, vaultCmd)
	commandOptions.registerListVaultsCommands(vaultCmd, listVaultsCmd)
	commandOptions.registerPrintDefaultCommands(vaultCmd, printDefaultCmd)
	commandOptions.registerRemoveVaultCommands(vaultCmd, removeVaultCmd)
	commandOptions.registerSetDefaultCommands(vaultCmd, setDefaultCmd)
	commandOptions.registerVaultCommands(rootCmd, vaultCmd)
}

func registerInitFamily(rootCmd *cobra.Command) {
	commandOptions := &initInvocationOptions{indexInvocationOptions: newIndexInvocationOptions(rootCmd)}
	initCmd := commandOptions.makeInitCmd()
	commandOptions.registerInitCommands(initCmd, rootCmd)
}

func registerListFamily(rootCmd *cobra.Command) {
	commandOptions := &vaultCommandOptions{root: rootCmd}
	listCmd := commandOptions.makeListCmd()
	commandOptions.registerListCommands(listCmd, rootCmd)
}

func registerPromptFamily(rootCmd *cobra.Command) {
	commandOptions := &promptInvocationOptions{vaultCommandOptions: &vaultCommandOptions{root: rootCmd}}
	promptCmd := commandOptions.makePromptCmd()
	commandOptions.registerPromptCommands(promptCmd, rootCmd)
}

func registerOntologyFamily(rootCmd *cobra.Command) {
	commandOptions := &ontologyInvocationOptions{vaultCommandOptions: &vaultCommandOptions{root: rootCmd}}
	ontologyCmd := commandOptions.makeOntologyCmd()
	ontologyValidateCmd := commandOptions.makeOntologyValidateCmd()
	ontologyWalkCmd := commandOptions.makeOntologyWalkCmd()
	commandOptions.registerOntologyCommands(ontologyWalkCmd, ontologyCmd, ontologyValidateCmd, rootCmd)
	commandOptions.registerOntologyQueryCommands(ontologyCmd)
}

func registerSearchFamily(rootCmd *cobra.Command) {
	commandOptions := &searchInvocationOptions{vaultCommandOptions: &vaultCommandOptions{root: rootCmd}}
	codeSearchCmd := commandOptions.makeCodeSearchCmd()
	commandOptions.registerCodeSearchCommands(codeSearchCmd, rootCmd)
}

func registerServeFamily(rootCmd *cobra.Command) {
	commandOptions := &vaultCommandOptions{root: rootCmd}
	serveCmd := commandOptions.makeServeCmd()
	startCmd := commandOptions.makeStartCmd()
	commandOptions.registerServeCommands(rootCmd, serveCmd, startCmd)
}

func registerStopFamily(rootCmd *cobra.Command) {
	commandOptions := &stopInvocationOptions{vaultCommandOptions: &vaultCommandOptions{root: rootCmd}}
	stopCmd := commandOptions.makeStopCmd()
	commandOptions.registerStopCommands(stopCmd, rootCmd)
}

func registerUpdateFamily(rootCmd *cobra.Command) {
	commandOptions := &updateInvocationOptions{vaultCommandOptions: &vaultCommandOptions{root: rootCmd}}
	updateCmd := commandOptions.makeUpdateCmd()
	commandOptions.registerUpdateCommands(updateCmd, rootCmd)
}

func registerClaudeFamily(rootCmd *cobra.Command) {
	commandOptions := &vaultCommandOptions{root: rootCmd}
	claudeCmd := commandOptions.makeClaudeCmd()
	commandOptions.registerClaudeCommands(rootCmd, claudeCmd)
}

func registerCodexFamily(rootCmd *cobra.Command) {
	commandOptions := &vaultCommandOptions{root: rootCmd}
	codexCmd := commandOptions.makeCodexCmd()
	commandOptions.registerCodexCommands(rootCmd, codexCmd)
}

func registerConfigFamily(rootCmd *cobra.Command) {
	commandOptions := &vaultCommandOptions{root: rootCmd}
	configCmd := commandOptions.makeConfigCmd()
	configSetEnvCmd := commandOptions.makeConfigSetEnvCmd()
	commandOptions.registerConfigCommands(configCmd, configSetEnvCmd, rootCmd)
}

func registerCredentialsFamily(rootCmd *cobra.Command) {
	commandOptions := &vaultCommandOptions{root: rootCmd}
	credentialsCmd := commandOptions.makeCredentialsCmd()
	commandOptions.registerCredentialsCommands(rootCmd, credentialsCmd)
}

func registerDesktopFamily(rootCmd *cobra.Command) {
	commandOptions := &vaultCommandOptions{root: rootCmd}
	desktopCmd := commandOptions.makeDesktopCmd()
	commandOptions.registerDesktopCommands(rootCmd, desktopCmd)
}

func registerChangelogFamily(rootCmd *cobra.Command) {
	commandOptions := &vaultCommandOptions{root: rootCmd}
	changelogCmd := commandOptions.makeChangelogCmd()
	commandOptions.registerChangelogCommands(rootCmd, changelogCmd)
}

func registerHarnessFamily(rootCmd *cobra.Command) {
	commandOptions := &vaultCommandOptions{root: rootCmd}
	harnessCmd := commandOptions.makeHarnessCmd()
	commandOptions.registerHarnessCommands(rootCmd, harnessCmd)
}

func registerMcpFamily(rootCmd *cobra.Command) {
	commandOptions := &vaultCommandOptions{root: rootCmd}
	mcpCmd := commandOptions.makeMcpCmd()
	rootCmd.AddCommand(mcpCmd)
}

func registerDiagnosticsFamily(rootCmd *cobra.Command) {
	commandOptions := &vaultCommandOptions{root: rootCmd}
	commandOptions.registerDiagnosticsCommands(rootCmd)
}

func registerRepositoryFamily(rootCmd *cobra.Command) {
	commandOptions := &vaultCommandOptions{root: rootCmd}
	commandOptions.registerRepositoryTrustCommands(rootCmd)
}

func registerValidationFamily(rootCmd *cobra.Command) {
	commandOptions := &vaultCommandOptions{root: rootCmd}
	commandOptions.registerValidationProductRunnerCommands(rootCmd)
}

func registerQueryRecipeFamily(rootCmd *cobra.Command) {
	commandOptions := &vaultCommandOptions{root: rootCmd}
	commandOptions.registerQueryRecipeCommands(rootCmd)
}

func registerViewFamily(rootCmd *cobra.Command) {
	commandOptions := &vaultCommandOptions{root: rootCmd}
	commandOptions.registerViewCommands(rootCmd)
}

func newIndexInvocationOptions(root *cobra.Command) *indexInvocationOptions {
	options := &indexInvocationOptions{vaultCommandOptions: &vaultCommandOptions{root: root}}
	options.newWorktreeRunIndex = options.runNewWorktreeIndexThroughRuntime
	return options
}
