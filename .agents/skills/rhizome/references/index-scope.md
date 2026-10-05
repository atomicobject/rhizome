# Index scope

Use this reference to see what Rhizome indexes and to change it when the suggestions from `rzm init` are not enough: search returns third-party or generated noise, a file is missing, or code is left out. Report every rule you add or remove.

## How Rhizome decides

Hidden files and folders (names starting with `.`) are never indexed. Every other path goes through these layers in order, and the last matching rule wins, including `!` negations.

| Order | Layer | What to know |
| --- | --- | --- |
| 1 | Built-in list: `node_modules/`, `vendor/`, `dist/`, `build/`, `bin/`, `target/`, and similar | Used only while `.rhizome/ignore` has no rules. The first `rzm init` copies the list into `.rhizome/ignore`, so in a set-up repository these lines live there and can be edited. |
| 2 | `.gitignore` files, root and nested | A nested file applies inside its folder. |
| 3 | `.rhizome/ignore` | Rules for Rhizome only. A legacy `.obsidianignore` is read only when `.rhizome/ignore` has no rules. |
| 4 | Config excludes: `notes.excludes` in `.rhizome/config.yml` | `rzm index --explain` reports these as `config excludes`. |

- Ignore rules remove a path from indexing and search: notes, code, and embeddings. Links to an ignored file still resolve.
- A file inside an ignored folder cannot come back through a `!path/file` rule, because the folder is already out. Include the folder with a `!/path/` boundary instead, then ignore the parts you do not want.
- All Markdown counts as notes by default; `rzm init` writes no notes folders. `notes.includes` limits notes to the listed folders and `notes.excludes` removes paths from that selection; neither makes an ignored file visible. Prefer `.rhizome/ignore` to keep things out, so new content is indexed by default.
- An exact `CONTEXT.md` file is always a note, even when `notes.includes` misses it or `notes.excludes` matches it. Built-in infrastructure folders, `.gitignore`, and `.rhizome/ignore` still keep it out, so use `.rhizome/ignore` when a `CONTEXT.md` must stay out.
- Code indexing covers every path that is not ignored, and each file's language comes from its extension. To leave a whole language out, list it in `code.disabledLanguages` (`python`, `go`, `ts`, `cs`, `php`). When `.rhizome/config.yml` names code folders (`code.<language>.roots`), code indexing is limited to the union of those folders.

## What to skip and what to keep

Skip content that is checked in on purpose but does not help anyone understand this system:

- checked-in dependencies and vendored libraries (`third_party/`, `external/`, `Pods/`)
- generated code and API clients (`*.pb.go`, `*_pb2.py`, `generated/`)
- minified or bundled assets (`*.min.js`, `*.bundle.js`)
- snapshots, large fixtures, and data exports
- HTML or Markdown exported from other tools
- copies of upstream documentation

Keep source the team maintains, docs that describe this system, and tests that explain behavior. When you cannot tell, keep the path and ask the user: a wrong skip hides content without any warning.

## Find candidates

- `rzm init --check` lists the skips init would propose: vendored or generated folders, minified files, generated code, and indexable files over 1 MB. Applying them means running `rzm init`, which needs the user's consent like any init run. You can also write the same rules by hand.
- Folders with the most tracked files, and the largest tracked files in KB:

  ```bash
  git ls-files | cut -d/ -f1-2 | sort | uniq -c | sort -rn | head -20
  git ls-files -z | xargs -0 du -k 2>/dev/null | sort -rn | head -20
  ```

- `rzm index --status` shows whether semantic search is on and which code folders the index covers.
- `rzm index --explain <path>` shows whether a candidate is already ignored; an ignored path needs no new rule.

## Write rules

Rules use gitignore syntax. Put Rhizome-only rules in `.rhizome/ignore`; change `.gitignore` only when Git should also ignore the path.

```gitignore
# Generated API client, rebuilt from openapi.yaml
/web/src/api/generated/

# Exports from the analytics tool
/data/exports/
```

- A trailing `/` matches folders only: `/third_party/`.
- A leading `/` anchors the rule at the repository root. Without it, `generated/` matches a folder with that name at any depth.
- Start each group with one comment line that says why, so the next reader can judge the rule.
- After the first run, init writes only to its own sections, `# rhizome: suggested skips` and `# rhizome: included subtrees`. A blank line ends the suggested skips section, so keep team rules in their own group after a blank line.
- `# rhizome: keep indexed <path>` anywhere in the file stops init from proposing that path. To index a path init skipped, delete its rule and the reason line above it, then add the keep line; without it, the next rerun proposes the skip again.
- `!/path/` under `# rhizome: included subtrees` indexes a folder Git ignores. It is an include boundary: the folder's own `.gitignore`, the built-in rules, and later rules still apply inside it. `rzm init --include-ignored <path>` writes the same line. Avoid `!path/**`, which also brings back everything the folder's own `.gitignore` excludes.

## Verify

1. Run `rzm index --explain <path>` on one path the change should drop and one nearby path it should keep. It names the deciding layer, file, line, and pattern, and for a code file it says whether the file is indexed as code.
2. `--explain` does not check `notes.includes`. A Markdown file it reports as indexed is a note only when `notes.includes` matches it or it is `CONTEXT.md`.
3. Run `rzm index`. An edit to any ignore file forces a full resync on that run, so it takes longer than an incremental update and needs no `--rebuild`.
4. Rerun the search that exposed the problem and confirm the result changed.
