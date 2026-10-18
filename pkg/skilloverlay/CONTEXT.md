## pkg/skilloverlay

Pure composition of loaded skills and YAML overlays. `Decode` validates one strict overlay document; `Render` applies ordered fragments to declared Markdown slots and returns rendered skill files plus structured entries/issues. `Report.Manifest` preserves the shared machine-readable report contract.

This package owns the skill/file/overlay/report model, slot parsing, default retention/suppression, deterministic operation order, and content hashes. Inputs are bytes and source facts; it does not discover files, inspect configuration, or import applications. Lexical Markdown scanning comes from `pkg/markdownsyntax`.

`pkg/app/cli/init` retains the embedded template sources, bundle loading, starter resolution, and effective skill selection. Validation receives its bundle validator through `RunContext.SkillOverlays`; bootstrap receives its onboarding asset loader through `CodexParams.OnboardInstructions`. Neither consumer chooses init assets itself.
