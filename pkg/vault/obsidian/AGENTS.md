# Vault Obsidian tests

- Keep one primary test for each behavior at the observable vault, config, parser, or edit boundary. Carry unique cases into that test before deleting helper tests.
- For missing results, assert the complete positive set and reject unexpected findings; a lookup guarded by `if present` can pass when the required finding vanishes.
- Compare edited frontmatter as YAML and the remaining body byte for byte. Do not use emitted YAML indentation as the contract.
