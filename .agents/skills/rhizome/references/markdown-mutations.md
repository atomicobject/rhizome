# Markdown mutations

Markdown paths and heading fragments participate in the graph. Do not use raw filesystem moves for notes or attachments, and do not plain-edit a heading that may have inbound references.

## Move or rename a note/attachment

Use the project launcher's `rzm note move` so backlinks can be rewritten with the path change. It accepts one source and target, or `--to-folder <folder> <sources...>` for multiple sources whose filenames should be preserved. This is a direct mutation, so confirm the source set, destination, collision behavior, backlink policy, and write authority before running it. Use the `note-move` entry in `rzm agent surface` for the current top-level usage and flags.

## Rename a linked heading

Use `rzm agent note-rename-heading <path> "<old>" "<new>"` for a read-only plan. The operation can rewrite inbound heading-fragment references and may upgrade them to a durable block target. When the plan is accepted and mutation is authorized, use the project launcher's top-level `rzm note rename-heading ... --apply` command.

## Durable links

- Prefer a whole-note path target with a readable display label for a note-level identifier. The display label is presentation only; it does not replace the real path target.
- Prefer canonical node refs or identifier-backed block targets for embedded nodes.
- Do not create new durable references to embedded nodes as fragile `note#Heading Text` links when a node/block target is available.
- Use `rzm agent node-link --target <path#fragment> --ensure plan` to resolve or propose a durable target. The read-only agent CLI refuses apply.

After a move, heading rename, or manual link surgery, run the narrow fragile-external or broken-link selector for the changed source/target. Rerun any type/identifier check affected by the mutation.
