package obsidian

const (
	ExecuteUriError                  = "Failed to execute Obsidian URI"
	NoteDoesNotExistError            = "Cannot find note in vault"
	VaultAccessError                 = "Failed to access vault directory"
	VaultReadError                   = "Failed to read notes in vault"
	VaultWriteError                  = "Failed to write to update notes in vault"
	RhizomeConfigReadError           = "Cannot find rhizome config; use set-default to set a default vault or --vault flag"
	RhizomeConfigParseError          = "Could not parse rhizome config file; use set-default to set default vault or --vault flag"
	RhizomeConfigDirWriteError       = "Failed to create rhizome config directory. Please ensure you have the correct permissions."
	RhizomeConfigGenerateError       = "Failed to generate rhizome config file. Please ensure vault name does not contain any special characters."
	RhizomeConfigWriteError          = "Failed to write rhizome config file. Please ensure you have correct permissions."
	ObsidianConfigReadError          = "Failed to read Obsidian config file. Please ensure vault has been set up in Obsidian."
	ObsidianConfigParseError         = "Failed to parse Obsidian config file. Please ensure vault has been set up in Obsidian."
	ObsidianConfigVaultNotFoundError = "Vault not found in Obsidian config file. Please ensure vault has been set up in Obsidian."
	RhizomeVaultExistsError          = "Vault already exists in rhizome preferences; use --force to overwrite"
	RhizomeVaultNotFoundError        = "Vault not found in rhizome preferences"
	RhizomeVaultPathInvalidError     = "Vault path must be an existing directory"
	RhizomeVaultNameRequiredError    = "Vault name is required"
)
