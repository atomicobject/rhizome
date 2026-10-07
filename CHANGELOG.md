# Changelog

## [Unreleased]

- Fix duplicated note paths when opening containing sections from action items in untyped notes.
- Search and folder tabs scroll smoothly after switching away and back, and restore their saved position after reload without replaying older scroll updates.
- A schema can mark one `Int` or `Float` field per type as its rank with `@display(role: RANK)`. Generated views sort by it, and grouping or laning by a link to that type lists its targets in rank order instead of by title or size, so IP ideas grouped by opportunity follow the opportunities' rank. Unranked targets follow by title.
- All notes opens on a Briefing (what needs attention, what is in motion, and what changed across the vault) instead of a graph of every note, so switching to it no longer freezes the workspace. A new Overview for All notes and every display group shows how groups and types connect on a type-level map with a matrix alternative, a member table with stages, gaps, linked share, and issues, untyped notes by folder, and a group's unused relations, outside notes, guide, and views. Each block loads on its own. Views can mount on All notes with the new `workspace` mount kind, read type-level counts from `GET /api/v1/ontology/shape`, and open a folder-filtered search with the kit's `openSearch`. Explorer keeps the note graph.
- New Rhizome logo and app icon: a Merriweather r growing out of a rhizome runner that ends in an AO-red node. Small icon sizes use a simplified mark so the desktop app stays legible in the Dock, Finder, and favicon.
- The desktop app passes drag and drop through to the page, so configured tables, cards, and boards reorder in the app as they do in a browser. A repository's context menu can open its runtime in the default browser, reveal it in Finder, and stop it. A stopped worktree shows as sleeping and starts again when you open it. Repositories reorder by dragging or with Alt+Up and Alt+Down. File > Close Tab (Cmd+W) closes the active note tab and Close Window moves to Shift+Cmd+W; Edit > Copy Page URL (Shift+Cmd+C) copies the page's address. Reopening a running worktree no longer flashes a progress screen.
- Right after a runtime starts, the type list, type colors, and type links work while the first index runs; counts stay hidden until they are known. A save refreshes open views at most twice instead of once per index event, and the "Refreshing view" label appears only for refreshes longer than 400 ms.
- Configured tables show the reorder grip as a compact handle beside each row's checkbox instead of a dotted strip that spanned the whole table. A row grouped under several values of a list field, such as an idea linked to two opportunities, can be reordered by drag or Alt+Arrow within any of its groups.
- Remember personal view settings and expansion choices per concrete view instance in ignored repository-local SQLite, independently of the index; provide reset, visible retryable failures, explicit shared YAML saving, and matching React and HTML kit APIs for custom views.
- Restore builds after the live indexing and code-mode runtime changes merge together. Typed runtime reads synchronize disk edits through the current watcher while background embeddings continue.
- Persist bounded local diagnostic logs and indexing reports, including queue and lock waits, full-scan reasons, embedding reuse, and provider attempts. Inspect recent work offline with `rzm diagnostics`; agent guidance explains how to read the evidence.
- Publish live note, code, and ontology updates before embeddings finish, keep warm web navigation available, recover failed background indexing after restarts, and refresh open panes after external edits. Batch and live indexing share processing and preserve incoming links during scoped updates.
- `rzm desktop [path]` opens the current folder in the Rhizome desktop app, adding its repository and switching the most recently focused window to that worktree. It runs on your own `rzm` even in pinned repositories and explains when the app is not installed.
- `rzm new-worktree` clones the source database and its WAL with a copy-on-write filesystem clone on APFS and reflink-capable Linux filesystems, holding the source's SQLite write lock for the moment of the clone so a live runtime keeps working. Other filesystems, cross-volume copies, and a busy source fall back to the previous `VACUUM INTO` snapshot. The command reports which copy it made. It also seeds personal view settings once, preserves existing destination settings, and keeps later preference changes independent.
- An open Rhizome UI keeps its headless runtime alive: a runtime is never idle while a UI event stream is open, so a quiet browser tab or desktop window no longer loses its backend after the idle timeout.
- Add a macOS Tauri desktop app. Each window has a collapsible sidebar of saved repositories and a toolbar for switching between a repository's Git worktrees, which show whether their runtime is running and mark ones not yet opened. Opening a worktree starts its configured Rhizome runtime with visible progress, first seeding a missing index from the primary worktree through `rzm new-worktree`. A displayed worktree's runtime is restarted if it exits. Windows, layouts, and selections are restored after relaunch, and global installation controls are built in. Repository content runs in a separate webview with no native command permissions.

- Report required watcher reconciliation failures consistently in job results while retaining their inputs for retry and preserving interactive cancellation.

- Route explicit node-locator edits through the live writer and preserve canceled node reads as retryable errors instead of cached missing results.
- Complete projection refresh after attachment moves and moves outside note selection, preserving full affected-path coverage and cleanup while reading only selected note sources.

- Release optional embedding-cache connections when searches, indexing runs, and runtimes finish or setup fails, while preserving caller ownership of injected providers.

- Preserve indexed notes and report discovery errors when code-index or code-anchor indexing cannot read a vault directory.
- Reject invalid or overlapping note move endpoints before changing any file, preserve targets and the Git index on failed overwrites, and keep case-only renames and authored alias links intact.
- Recover committed ontology edits after later note changes or deletion while preserving receipts and refusing cleanup of replaced recovery artifacts.
- Reject Section-derived link and neighbor targets consistently regardless of type names or declaration order, while preserving note links and section containment.
- Add recoverable native note namespace publication for complete required moves, overwrites, backlinks, and admitted Git staging, with separate current and recovered outcomes and before-write fencing for ordinary editors.
- Detect identifier collisions across both declared alias fields and remove every collided or retired authored value during reviewed repairs, preserving unrelated aliases and the required preferred-identifier mirror.
- Compose derived identifier alias repairs after preferred rekeys and strategy migrations, preserving exact child ownership and current file paths until governed renames while retiring values from every child alias field.
- Every type and interface offers a Briefing as its first view, in place of Overview, which remains for display groups. It shows what needs attention (validation issues, warning and risk values, stale active records, empty key fields with their policy reasons, and reverse links most records have but some lack), records in an active stage, recent changes with same-minute bursts collapsed, how records spread across each lifecycle, ordered, and category field, the primary date by month, how often each people, relation, and reverse field is filled and its most common targets, notes of other types that link in, and the companion guide. Signals open the collection's Table. Defaults are unchanged: a collection still opens its Table or Board unless the repository sets a default, and a remembered Overview choice for a type opens that default. View mounts accept `type: "*"` and `interface: "*"` for every type or interface; an exact default outranks a generic one, and a wildcard mount cannot replace a generated view. The Briefing ships in the group views' folder, so `rzm view eject` copies both.
- Custom views read the server's type profile as `profile` on each document `useTypeDocs()` returns: the type's shape, its lifecycle, summary, and primary date fields, and its ordered, category, people, key text, relation, reverse, and gap fields. `StatusMark`, `isTerminalValue`, and `statusPosition` now decide terminal values by stage: a value in the `done` or `dropped` stage is terminal whatever its tone, and an enum without stages has no terminal values. Values from type documentation already carry declared or inferred stages, so views that pass them through see no change, but a view that builds its own `EnumValueDoc` values must set `stage` where it relied on a `success` or `muted` tone or `collapsed` to mark a value finished. The bundled group views (Briefing, Trace, Sections) likewise read each member's lifecycle field, value stages, and gap fields from type documentation instead of guessing from field names and tones: a lifecycle field with any name counts, a record is in motion only in an `active` stage, and gaps are the profile's gap fields.
- GraphQL list `@reverse` and `@neighbors` fields take an optional `first` that caps the targets read per record, beside their `<field>Count`, and typed roots accept `sort: [{field: "updatedAt", direction: desc}]` to read the most recently changed records first. The group and type Briefings use both: each reads a type's 500 most recently changed records, and a type Briefing reads reverse-field counts with a capped sample of targets and at most 50 notes linking in per record.
- Display groups open generic group views built from schema metadata: Briefing (what needs attention, what is in motion, recent changes, outside links, member types, and their connections), Trace (records of one type as rows with everything they connect to as columns), and Sections (each member type as a compact table). A group with two or more member roots opens Briefing and a group with one opens Sections, unless the repository sets a default; Trace appears when member roots link to each other, and Overview stays selectable. `@display(role: PARENT)` on one single-valued `@link` field that targets the declaring type or an interface it implements lets a type's records form a tree that the views nest and roll up, and validation reports parent loops as `parent_cycle`. The views are ordinary custom views, and the platform gains what they need: `rzm view eject <id>` copies a bundled view folder into `.rhizome/views/` to customize; views import sibling `.css` files, are served from a transform cache with ETag revalidation, and refresh from host-forwarded events without polling in the workspace; `GET /api/v1/display-groups` and richer type documentation (enum `@view` metadata, `@requiresWhen`, summary and parent fields) back new kit hooks, navigation helpers, and components; GraphQL records expose `updatedAt` and `issueCount`; and `rzm validate views` reports unresolved relative imports, bare imports outside the import map, and invalid literal GraphQL documents. The Agentic Engineering starter gives feature areas a `parent`, stops styling active specs as in progress, and links efforts to the specs they carry out through `governingSpecs`.
- A native view mounted on a type or interface can set `mount.replaceGenerated: true` to become that collection's standard Table, Cards, and Board in place of the generated view, and its default unless another view sets `mount.default`. View choices now list the standard layouts first, then other authored views marked `custom: true`; a remembered generated layout moves to the replacing view. `rzm validate views` warns when the field is ignored or several views replace one target. Agent guidance now mounts a view about one type on that type rather than as a separate sidebar entry.
- Group configured views by link fields, including lists, from the Group menu; groups, board columns, and link filter values show each target note's title instead of the raw wiki link, links to the same note share one group, and link groups sort by title. Ontology field capabilities now report link fields as groupable. Configured views over note types read link targets from the index in one batched read instead of resolving each note's links, and reverse-link count columns no longer list every vault file.

- Keep displayed edits through save and view refresh, notify other views when committed data is readable, and reduce save delays from background validation and whole-vault postcheck reads.

- Unify Overview, Table, Cards, Kanban, and custom HTML/TSX views under one runtime contract; configure defaults for note types, specific or generic display groups, and individual node presentations, with contextual links and shared workspace edits. Type and interface collections now open their Table by default instead of the Overview graph.
- Two view definitions that both claim the default for one target now produce a `duplicate_mount_default` warning and both remain available; the first by order is used. Generated type and interface views are always listed beside authored ones, and an authored `generated: true` field is ignored.
- View validation now reports node mounts on section types and a `group` field on type, interface, or node mounts (ignored there, as before). Opening a custom view directly applies the same validation as the catalog.

- Reorder configured views by dragging rows, cards, and board cards, or with Alt+Arrow keys, whenever the view is sorted by an editable number, enum, or boolean field. A drop stages the field values that place the item there, such as a rank between its neighbors or a new priority, and Save commits them with other staged edits.
- Keep missing field values last in descending sorted views while edits are staged, matching the committed order and page boundaries.

- Note properties, the identity strip, and hover previews show a link field's target note title, as a clickable link with a preview, instead of its link text or path; unresolved links show their text without brackets or alias syntax, and an embedded node's Parent property shows the parent's title. GraphQL `NodeFieldState.links` returns each relation value with its resolved `ref` and `title`, read from the indexed field values that configured views use, and `NodeWorkspaceProjection.parentTitle` titles the parent.

- Show clickable relation values and structured note previews consistently across views, navigation, and note sections; preserve section targets, keyboard focus, and HTML note navigation, and surface view warnings and field capabilities.

- `rzm init` creates new files without asking and updates generated files nobody edited without asking; it asks only about files someone edited (take the update, keep my version, or show the diff), once per file across `.agents/skills` and `.claude/skills`. Runs without a terminal keep edited files and list them. A tracked `.rhizome/generated-files.yml` records what Rhizome wrote and replaces `.rhizome/template-rejections.yml`, starter family update policies, source fingerprints, and the `.rhizome-managed` files in every skill folder; the next `rzm init` migrates them. Removed `--skip-rejected`, `--reject-all`, and `--clear-rejections`.
- `rzm init` reruns show the current setup and a change list (settings changes, code and doc folders detection finds but config leaves out, generated-file updates, and edited files) and ask once: `Apply? [Y/n/s for settings]`. The settings menu has four sections: what gets indexed, semantic search, agents, and workflow; moving away from a workflow asks whether to remove its files or eject them. Turning on semantic search when a key appears and a workflow's new default addon are suggestions that only a terminal run applies. `rzm init --check` prints the same report, writes nothing, and exits 1 when init would change something. The "Found existing config. Reconfigure it?" question and the old per-setting prompts are gone.
- `rzm init` in a terminal ends by offering to build the search index (default yes), and reruns that applied changes offer to update it, so a new setup is searchable without a second command. Runs without a terminal list `rzm index` in Next instead.
- `rzm init` reruns group what they would do into Changes, Needs a decision, and Suggestions in every mode, report results as outcomes, and end with one Next block. `--accept-suggestions` lets an agent apply suggestions, such as removing folder limits, after the person agrees.
- The installer's suggested init command is plain `init` for project pins after v0.50.5; older pins keep `init --agentsmd on --agent-skills on --yes`, which those releases still accept. The generated-surfaces quality gate is now `rzm init` followed by `rzm init --check`.
- Code indexing covers the whole repository when `.rhizome/config.yml` turns code on without naming code folders, and each file's language comes from its extension, so code added after setup (or in a new language) indexes on the next `rzm index` without rerunning init. `rzm init` now writes `code: {enabled: true}` instead of per-language folders, even before a repository has code, and writes no notes folders, so all Markdown is indexed. `.rhizome/ignore` is how content stays out. Configs that name folders keep their limits; a rerun suggests removing them when Markdown or code sits outside them, and suggests turning code on for older docs-only setups, applying either only after a person confirms. `rzm index --explain <file>` now says whether a code file is indexed as code and, if not, which setting keeps it out.
- `rzm init` proposes skipping tracked content that bloats the index without helping search: vendored, generated, or test-fixture folders (`third_party/`, `generated/`, `testdata/`, and similar), minified or bundled files, generated code, and files over 1 MB. Accepted skips go under `# rhizome: suggested skips` in `.rhizome/ignore` with a reason each; `# rhizome: keep indexed <path>` stops a path from being proposed, and "What gets indexed" in settings can skip a path or index it again.

- Replace the README command catalog with Agentic Engineering onboarding and focused setup, skill, and customization guides.

- Run ordinary CI without private Git history; keep the legacy fingerprint history audit opt-in for the private archive.
- Keep temporary internal release tooling available during public migration while rejecting public compiler overlays and checking built binaries and installers for private release content before upload.
- Recover stale runtime and indexing locks after a local reboot when the recorded hostname matches exactly, the lock predates the current boot, and its PID is dead.
- Let Rhizome start with a warning when a completed repair journal conflicts with later note edits; preserve those edits and keep conflicting repair writes blocked.
- Remove rename sources before publishing repair destinations so failed or interrupted repairs recover regardless of filename order, including interruptions during reverse rollback.
- Check HTML effort workspaces in `frozen-scope-drift` through their `governing-specs`, reading acknowledgements from the linked work log, and return individual acceptance criteria with their own links from `story-acceptance-pack`.
- Add custom views: a view definition with `source.kind: custom` names a TSX or HTML entry under `.rhizome/views/`, which `rzm` serves at `/views/<id>` with an embedded UI kit (`/kit/v1/`: React, TanStack Query, shadcn-compatible components, Tailwind, and GraphQL, write, and navigation helpers) and a per-file TypeScript transform, so no Node toolchain is needed. Custom views mount in the Notes rail, open standalone at the same URL, run as trusted same-origin repository code, and are checked by `rzm validate views`; `rzm init` installs a `custom-views` agent skill.
- Keep custom-view source checks to one active read with a ten-second deadline for fetch and body consumption, cancel them on cleanup, and recover missed edits after a stalled read or an overlapping hosted reconnect.
- Publish complete lock metadata atomically so an exiting startup process cannot leave an empty lock blocking runtime startup and shutdown. Replace revalidated stale records atomically so concurrent Windows polling cannot fail during a delete/open gap; preserve the old record if publication fails.
- Preserve every active indexing priority waiter when another waiter completes or cancels. Each waiting writer checks and renews its own request even while a peer remains live; interactive indexing retries nonfatal priority publication failures. Priority publication and joined cleanup proceed during scanner contention; checks preserve unrelated files and reject a symlinked priority directory. Stop all vault processes before upgrading from the former single priority file.

- Serialize runtime startup-record reads with updates so Windows `rzm stop` cannot fail when polling overlaps cancellation.
- Serialize SQLite schema initialization across database-file symlinks so alias paths cannot bypass the migration lock.
- Share short-lived command execution between Claude and Codex, retaining cancellation, completed-exit diagnostics, and each driver's generation and version behavior.
- Preserve cancellation and deadlines in Codex and Claude one-shot commands so generation reports a timeout and an interrupted version check does not block healthy retries for ten minutes.
- Refresh the web ontology cache after authored schema changes so note workspaces can load against the current index.
- Add opt-in `@reverse(field:)` relationships derived from a named authored link, including embedded sources, query/view counts, and schema guidance, without treating prose mentions as structural links.
- Expose per-root GraphQL coverage in `extensions.typedRoots` and support `offset` continuation while preserving existing result arrays, saved recipes, and code-mode output.
- Diagnose unsupported link-filter operators and unresolved targets; resolve `eq`/`in` consistently across indexed and selector queries while retaining scalar `contains`.
- Compress semantic continuation tokens losslessly to reduce compact response overhead; continue accepting existing uncompressed v2 tokens.
- Share query vectors across concurrent same-text facets so continuation pages keep the same ranked window, and preserve the original provider outage when canceling sibling embedding work.
- Reduce semantic result hydration by reading ordered section IDs instead of full sibling section bodies.
- Keep detailed semantic search bodies eligible after budget or JSON trimming removes them. Publish session fingerprints only for final encoded bodies, group representations sharing one source, and suppress concurrent duplicates consistently in body fields and packed text, including legacy non-UTF-8 source comments.
- Open one-shot code readers before optional session writers so concurrent WAL initialization cannot hide available indexed evidence.
- Keep budget-trimmed or compressed file and vault context available for larger session retries, publish dedupe history only for complete final content, and pass cancellation into context compression.

- Keep one-shot indexed reads prompt under SQLite contention while retaining writable runtime startup recovery.

- Retry transient SQLite contention during code-index startup before publishing readiness or a terminal failure.
- Publish live code updates through the same atomic SQLite batch as full indexing, preserve retries after failed or canceled writes, and retain TypeScript external evidence. Normal reindexing refreshes older incomplete live publications. Retire file context cached during publication and prevent older in-flight reads from repopulating it after invalidation. Avoid rebuilding path hints twice per live update.

- Make managed SQLite readers return lock contention immediately instead of silently waiting up to 30 seconds, while preserving writer and session timeouts.

- Keep newly created agent chats and current selection when an earlier session deletion finishes.
- Preserve managed index read errors instead of misreporting them as schema drift that requires a rebuild.
- Refresh global graphs when a later index event supersedes a fetch during the same coalescing window.
- Refresh global graphs once per index-event burst even when an older read finishes after the event, and cancel pending first loads before the corrective read.
- Keep nested files live after moving a directory tree into a vault when using fsnotify, and resync files written while its watches are being installed.
- Reconnect agent chat events after HTTP failures during a runtime restart, and cancel pending reconnections when leaving the chat.
- End active agent turns and retire approvals promptly when the vendor emits broken JSON or closes stdout, even if its process keeps stderr open; share process cleanup between the Codex and Claude session transports.
- Accept empty keys inside nested Markdown and HTML metadata objects, and avoid duplicate deep copies during metadata conversion.
- Keep metadata snapshot hashes and rows on one source capture, avoid duplicate reads during full rebuilds, and rebuild older inconsistent generations.
- Keep top-level search result metadata stable when concurrent retrievers finish in a different order.
- Apply exact symbol and test filters before lexical and vector retrieval limits, preserve filtered symbols through module rollup, and retain provider note regions in scoped note searches.
- Keep semantic search from returning unembedded replacement chunks or vectors from a previous embedding dimension.
- Retire stale vector evidence when chunk content changes, so failed embedding requests remain retryable and reverted ontology content cannot reuse an absent vector. Existing stale vectors can be regenerated with `rzm index --rebuild`.
- Reduce SQLite identity lookups during embedding publication by resolving chunk IDs once per write batch while preserving atomic vector and generation updates.
- Keep nested search-result metadata stable when retrieval lanes finish in different orders.

- Keep completed indexes readable after SQLite maintenance and restart replacement runtimes only after fallback indexing finishes.

- Leave runtime startup intact when its stop request has already been canceled.
- Reject indexing jobs when the database was replaced while they waited for the writer lock.
- Preserve shutdown diagnostics and remove startup records when a vault reached through a symlink disappears.
- Remove the unused no-op compressor and skip discarded compression preparation when file reads have no configured compressor.
- Keep configured-view scalar filters consistent with indexed text, numbers, and dates, and stop collecting facets after their display limit.
- Keep typed GraphQL scalar filters and sorts consistent across indexed roots and selectors: compare numbers, Boolean values, and dates by their declared types, preserve exact large integers, and place missing or invalid typed sort values last. Sorting retains authored blank text values and source order for embedded records with equal keys.
- Keep staged scalar filtering, sorting, and page boundaries consistent with indexed queries, and prepare merged sort keys once instead of extracting fields during every comparison.
- Preserve canonical path casing and indexed page order when typed queries use find/property selectors or residual sorts.
- Validate staged typed selections against current values: return null for invalid optional fields, report required field errors, and preserve authored values for editing.
- Use the refreshed ontology schema for next-ID allocation and authoring-guide suggestions so newly added sibling types reserve their existing identifiers. Keep guide field instructions and skeletons consistent with the refreshed identifier contract.
- Require a real, exact historical wikilink before treating a renamed file as an exact broken-link repair candidate.
- Reduce structured-link scan allocations by sealing each link once after source ordering.
- Keep code references resolvable through successive note renames, including dollar signs, wiki delimiters, parentheses, Unicode, first-segment colons, and literal percent escapes. Apply original spans once per mapping and keep embedded `@` text inside parsed links. Use canonical wiki or URL-encoded Markdown destinations when a mention no longer fits; unrepresentable wiki embeds become normal links, while authored Markdown images stay unchanged. Explicit Markdown links inside extracted comment code examples now scan alongside wiki links. Markdown destinations use URL percent semantics and resolve to the unique cached note path. Code indexer v1.15.0 refreshes unchanged sources to regenerate persisted references. Batch renames reuse unchanged comment scans across mappings and apply each edited block in one pass. Broken-link checks use the canonical encoded destination for indexed and unindexed targets, so literal percent escapes and hashes check the correct headings and blocks. Workspace same-file Markdown anchors retain their canonical source identity when filenames contain those characters.
- Skip oversized code files before reading them during note rename and move reference updates, reducing memory use while retaining the 2 MiB limit.
- Speed up batched code-reference renames by skipping mention regexes whose old name is absent from the current comment.
- Keep saved-query input diagnostics in a stable order.
- Validate saved query recipes in skill and template sources, report malformed, unreadable, or broken-symlink source findings, and skip recipe discovery for unrelated checks while leaving empty roots and unrelated skills absent.
- Refresh typed-link sources after alias collisions, target-type changes, and target deletion even when metadata already includes them as unchanged candidates. Ontology materialization v13 repairs older stale projections during ordinary freshness refresh while preserving direct unchanged-source skips.
- Accept acyclic metadata slices that share storage while still rejecting actual cycles.
- Refresh subscribed note panes after browser-managed event-stream reconnects.
- Keep deleted note panes cleared when an earlier refresh finishes, and retry failed embedded-node reads with their original structural fingerprint or node ID.
- Preserve cancellation while draining non-JSON API responses and hydrating the final search result.

- Wait for active vault writers during restart recovery, let stop and stop-all cancel pending startup, release runtime ownership after startup failures, and report shutdown only after cleanup finishes.
- Keep live writer locks through sleep or paused processes, recover guard locks after crashes, and serialize direct note mutations and worktree database replacement with background work.
- Keep code-mode initialization responsive while the runtime starts, bound interrupted index waits, and preserve supervised process ownership when delegating to a pinned binary on Unix.
- Release discarded completed code-mode operation payloads while retaining unobserved failures and waiting for accepted writes before cleanup.

- Let note saves wait for background indexing and request priority, including work in the same process; preserve retry receipts and source-conflict checks.
- Let configuration publication replace files held by cooperative Windows readers, preserve complete reader snapshots, and remove temporary files after failed atomic writes.
- Publish heading renames and all inbound link rewrites together, preserve original files on failure, and recover interrupted applies before retry.
- Publish note renames, batch moves, and required backlinks in one recoverable transaction; preserve committed outcomes through command errors and code-mode output limits.
- Update fragment-only self-links when renaming a heading, preserving aliases, protected code, and local links in other notes, including requests that use another case spelling of the same file.
- Prepare native note-move Git staging in owned scratch with standalone rollback and candidate indexes, preserving staged versus dirty source content and mixed tracked/untracked results for the transaction owner.
- Refresh validation after watched file and configuration changes, coordinate refreshes with background indexing, and add a Refresh validation button to Problems.

- Restrict title typo fallback to single-word, one-edit matches and stop query-interpretation notices from claiming search sources are unavailable.

- Hide internal fallback type labels in the web UI while preserving section navigation.

- Import provider credentials once with `rzm credentials import --from 1password`. Public builds never bundle team credentials; internal releases bundle rotated Voyage and TypeSafe keys, unlocked by `ATOMIC_RHIZOME_KEY`, until the public cutover.
- Require explicit user-local trust before running a checkout-selected Rhizome binary, and reject untrusted application HTTP hosts before API routing.
- Publish GitHub Releases and Homebrew for every release. Internal releases also publish to a private mirror, so existing installs keep updating with no manual step.
- Remove the project KB starter and its bundled AO reference library. Add contributor and security guidance, dependency attribution, and a private migration handoff.
- The Rhizome repository no longer installs the complex-domain starter for its own agent workflow; the starter remains available to other projects.
- Search retains the same evidence when equally scored facts compete at the evidence limit.

- Remove unused graph-analysis prewarming while retaining asynchronous note-cache warmup.

- Directory include rules with trailing spaces now re-include the entire ignored subtree.

- Preserve cancellation errors when API response body reads are aborted, so canceled relation lookups do not appear as JSON failures.

- Configured view filters preserve the selected comparison operator when choosing a value.

- Configured-view range filters match individual repeated field values when evaluated in memory, consistent with indexed filtering.

- Dotenv values preserve escaped double quotes and quoted hash characters while removing trailing comments.

- Node-read traversals honor the tighter edge and node budget when explicit traversal limits are also supplied.
- Preserve source groups when expanding embedded catalog nodes by node ID, and apply total edge limits to shared-frontier evidence emitted for multiple sources.

- Database failures during schema validation preserve indexed Intel data instead of treating the failure as schema drift and resetting it.

- Search preserves distinct runtime warnings when their fields contain pipe characters.

- Failed runtime manifest replacements preserve the published discovery file so a readiness update cannot remove a running runtime from discovery.

- Configured views preserve indexed ordering for repeated text, Date, and DateTime values when a secondary sort runs in memory.

- Configured views preserve indexed numeric ordering, including repeated values, and missing values last when a secondary sort runs in memory.

- GraphQL introspection reports inherited interfaces for interface types, including the universal NoteNode interface.

- Identifier allocation includes authored embedded owners in shared pools, preserving each node's identity and datetime alias reservations.

- Configured views preserve indexed Date and DateTime ordering, including malformed and missing values last, when a secondary sort runs in memory.

- Grouped configured views allocate less memory when resolving row fields.
- Runtime shutdown waits for initialization, boot catch-up, session cleanup, and note-cache warmup to finish before closing stores and releasing vault ownership.
- Windows indexing metadata readers allow priority cleanup and lock removal while polling is active.
- Share index-lock metadata reads with the cooperative file reader while retaining lock ownership, Windows deletion sharing, and long-path behavior.
- Runtime discovery polling on Windows no longer blocks the owner from removing its manifest during shutdown.

- Configured-view sorting and grouping now order mixed values consistently regardless of source order: numbers, then dates, then text, preserving existing empty-value placement. Indexed text and enum fields retain normalized lexical ordering when a secondary sort runs in memory.
- Superseded validation refreshes consistently report supersession, and a replacement that fails to start leaves the active refresh running.
- Opening an index retries schema validation when a concurrent writer invalidates its snapshot, avoiding transient database-lock failures.
- Watcher shutdown discards pending events and drains active callbacks before closing its backend, preventing delivery into disposed runtime services.
- Failed watcher reconciliation now cancels earlier queued validation and fences running results, retaining stale diagnostics until a later refresh succeeds.

- Browser note projections no longer restore stale content when in-flight loads or warming finish after cache invalidation.

- Cache shutdown now waits for background recrawls to finish and prevents new recrawls from starting after close.

- Global event streams opened during server shutdown now close promptly instead of retaining an inactive subscription.

- Context trimming preserves complete UTF-8 characters when a byte budget cuts through accented text, CJK characters, or emoji.

- Context packing reports truncation and omitted source keys when the first piece exceeds its budget, allowing configured compression to account for the dropped context, including a sole piece that exceeds its budget by at least 25% of its source length.

- Clearing browser preferences in another tab now resets the per-type Home/Table choices in open tabs.

- Notes containing malformed percent escapes in authored Rhizome links now render and open their literal targets without crashing.

- Restore saved note tabs even when browser storage rejects the legacy tab migration write or cleanup.

- New indexing requests no longer join cancelled queued runtime jobs, allowing retries and pending watcher work to run.

- Configured-view search avoids unnecessary field conversion and temporary allocations when matching rows.

- Field-level `@display(importance:)` now orders note properties, folds DETAIL properties in read mode, selects implicit configured-view columns and card fields, and groups the column picker while keeping authored view field lists unchanged.

- Configured ontology views and GraphQL expose batched numeric counts for `@neighbors` list fields without hydrating related nodes.

- Configured views gain two presentations beside the table. `variants.kanban` renders a board whose columns come from a single-valued enum, boolean, or relation field (`columnField`), including empty schema values, and moving a card stages an edit for review. `variants.card` renders dense cards grouped like the table. Both share one card spec (`eyebrow`, `title`, `preview`, `fields`), and every view still offers a table. Enum values accept `@view(tone: ...)` for status marks, grouping by a list field now places a note under each value it holds, group counts are totals rather than page-local, and generated default views show identifier, title, status, and summary instead of repeating the path. The Agentic Engineering starter now ships Efforts, Specs, User Stories, and Feature Areas views; Action Items gains a board; Complex Domain gains Domain Types cards and a Requirements board. New view issue codes: `invalid_card_variant`, `invalid_kanban_variant`, `missing_kanban_column_field`. Upgrading: older binaries cannot load an ontology that uses `@view(tone: ...)`, so everyone sharing a repository should move to this release before accepting the updated starter ontology.

- Remove embedded credentials and replace credential-dependent tests with synthetic fixtures. Source builds use externally supplied provider keys.
- Align the frontend agent permission-mode type with the three supported harness modes; reconcile code-mode concurrency, starter installation guidance and delivered effort status records identified by the repository audit.
- Load development and release credentials from a shared 1Password Environment. Source builds contain no credentials; encrypted Voyage and TypeSafe bundles are generated only during explicitly opted-in releases. OpenAI and Cerebras are no longer bundled. Environment credentials are no longer offered for plaintext persistence, and semantic compression requires explicit enablement and is no longer offered during setup.

- Add bulk ignore checks and concurrent Jev batches with partial results, coordinated throttling, and resumable repository audit scripts. Audits screen contract/status evidence, support reproducible pilot samples, and publish only source-verified improvements with stale-review invalidation. Code mode now overlaps compatible calls and supports explicit execution deadlines up to 24 hours.

- Expose Jev through code-mode `rzm.evaluate` and `rzm agent evaluate`, with typed questions and results, shared credential resolution, and usage metadata.

- Add a standalone Go client for TypeSafe (Jev), with typed questions and answers, bounded retries, and TypeSafe credentials in the encrypted Atomic team key bundle.

- Watcher indexing yields to interactive index requests by canceling its current batch, retaining unfinished reconciliation work, and releasing the index lock after active writes finish.

- Compressed prompt output closes its vault wrapper for successful compression and empty results.
- Progress output reports how much input was consumed when a destination accepts only part of a write, allowing callers to retry without losing output.

- Context commands skip semantic startup when compression is disabled or unavailable, and prompt fallback correctly reports that no compressor is available.
- Inline property edits preserve neighboring fields, undeclared properties, and surrounding prose during batch previews, lineage tracking, and duplicate cleanup.
- Runtime startup continues initializing later capabilities after an early search failure and releases cancellation subscriptions when construction fails.
- Batched code-chunk writes now read current anchor metadata, fixing a database error that prevented those batches from being saved.

- Each vault now has one runtime process: `rzm serve`/`rzm start` wins `.rhizome/runtime.lock`, and `rzm index`, `rzm agent start`, MCP, and code mode start a headless one on demand (`rzm serve --headless`) that keeps the index fresh and exits after an hour idle. `rzm index` runs inside that runtime with streamed progress and joins an index already running instead of contending for the index lock; `--in-process`, `RZM_RUNTIME_AUTOSTART=0`, or `runtime.autostart: false` keep the old in-process path. Code-mode scripts execute catalog operations in the runtime. New `rzm stop [--all]`, `rzm index --status` shows the runtime and lock holder, a second `rzm serve` exits with code 3, and `rzm serve` or `rzm start` takes over from a background runtime (`rzm start` against an already attached runtime opens its URL). Follower mode, the cache-hints log, and the `leaderFollower` config block are removed. Upgrading: stop any `rzm serve` from an earlier release first; the new runtime refuses to compete with it, and an old `index.lock` is reported as foreign once.

- Two Rhizome processes opening a fresh or upgraded vault database at the same time, such as a command and the runtime it starts, no longer fail with `database is locked` or leave a half-migrated schema: creation and migration are serialized by `.rhizome/db.sqlite.init.lock`.

- A runtime on a vault with embeddings turned off now reports ready instead of staying in the warming state, and `rzm stop` no longer reports a stopped runtime as stuck when its parent process has not reaped it yet.

- `rzm new-worktree` copies the source index as one consistent SQLite snapshot instead of copying the database and its WAL and SHM files, so the source may keep indexing while the copy runs.

- Markdown editing keeps modified blocks in place, continues list markers with Enter, removes them with Backspace, and labels Markdown note modes Structure and Markdown.
- Structural relation targets that point inside a note, such as action items assigned to a person, now show the item's own text in the note rail, sit with related notes instead of linked code, and open the note at that item.
- The HTML note viewer runs edge to edge in the read pane with no framing box, and expanding HTML metadata now pushes the document down and scrolls the pane instead of squeezing the viewer.
- Agentic Engineering supports Markdown efforts and linked HTML effort workspaces with reusable templates, shared lifecycle queries, compatible identifier allocation, and release evidence from committed work logs and governing specs admitted by each snapshot’s note discovery rules.
- HTML note metadata starts collapsed, leaving more room for the document while keeping errors and pending edits visible. Exact HTML metadata links and provider-authored section backlinks participate in typed queries.
- Agentic Engineering plans group work into verifiable delivery batches with explicit review boundaries and bounded experiments, while preserving user and harness delegation defaults.

- Code-mode schema discovery accepts JavaScript method names and can return only input or output schemas with `--schema input|output`, keeping shared call and outcome guidance.

- Search preserves exact note titles and structural targets through ranking, deduplicates repeated evidence facts, qualifies weak answers, and uses generation-bound fixed-window continuation without duplicate or skipped sources. `rzm search --json` exposes the canonical application contract, and `--continuation` advances its opaque cursor.

- Low-confidence search answers no longer name a must-read source that only graph popularity or a stray body term supports; the page says no source is about the query and lists those sources as supporting. A prose request under a code-navigation intent no longer locks onto a code member that merely shares one of its words: an exactly named note resolves instead, and speculative matches stay informational without blocking retrieval. An optional Voyage cross-encoder reranker (`RHIZOME_RERANK_PROVIDER=voyage`) blends into the top of the ranking, keeps exact matches pinned, and degrades to the base order when unavailable.

- Semantic code chunks now embed the owning module's summary and the rationale comments inside each symbol, so paraphrased questions reach code whose explanation lives in comments. Specs, efforts, and analysis notes are classified separately from reference docs. Multi-facet answers let a documentation or test facet lead with a source in that role. `rzm`'s embedding providers honor `RHIZOME_EMBEDDING_CACHE`, and `tools/searchquality -embedding-cache` pins live-provider evaluations to reusable vectors.

- Search confidence is honest on real repositories: high confidence now requires an exact title, path, or symbol match or corroboration from two retrieval lanes, one shared word no longer counts as strong support, and no-answer questions come back low instead of high. An exact-title hit leads the must-read list; `go_to_def` on a bare name resolves to the declaration instead of same-named struct fields and demotes other same-named anchors; `rzm search` and MCP infer the same default mode from one heuristic.

- MCP `semantic_query`, HTTP `/api/v1/search`, the agent CLI, and code mode now run through the same application search path as `rzm search`: one ranking, one assessment, one continuation cursor. Continuation requests resend the same queries, seeds, mode, and controls with the token; the `offset` argument and `scorePercentiles` field are removed, and `requireExactSymbol` filters inside the engine. The GraphQL `search` root runs the unified engine for notes and no longer takes a `mode` argument; callers may widen the candidate window, so `first` up to 200 and the Explorer's 200-result search are honored. The web search workspace shows ranked counts, target resolution, confidence, and coverage gaps. Multi-facet answers now count a facet as covered when its own top-ranked useful source is selected, so paraphrased facets can be satisfied. Indexing merges validation diagnostics that share one stable issue key instead of failing the run. An exact symbol hit in a small file is no longer replaced by its module summary.

- HTML files can be admitted as first-class notes with static retrieval, ontology roots, normal Notes tabs, isolated interactive viewing, parent-approved generated downloads, and staged root-metadata editing.
- Add full-width retained project-search tabs with ranked note/code evidence, type and folder filters, continuation, source navigation, and shareable state. Notes keep one local list filter, and their Info, Outline, and Graph tabs no longer shift or repeat the note identity.

- Note workspaces open faster after server startup because code-reference discovery skips ignored build caches and dependency directories before scanning them.

- Existing-note GraphQL navigation remains available during unrelated metadata reconciliation, and warm servers can open note reads before background ontology and graph indexing completes.

- Code-mode discovery explains operation semantics and shared call options. Installed examples preserve truncation metadata and successful partial results, and teach targeted rereads after projected evidence.

- Session startup includes the compact code-operation catalog and describe/execute examples. Rhizome guidance explains when code mode helps and how to fetch selected contracts together before writing a script.

- Add command-specific agent discovery, compact ontology authoring contracts, selected executable query fragments, and opt-in compact search sources. Search exclusions now apply throughout the answer packet and persist across continuation pages; code-mode search accepts per-query modes.

- Warm indexing accepts intentionally empty ontology catalogs while still repairing missing rows. Materialization version 6 records expected catalog sizes and identity digests and restores blank-note fallback roots during incremental indexing.

- Tag extraction skips regex scans on prose lines without a hashtag marker, reducing repeated ontology matching work.

- Indexing honors Voyage retry delays with bounded recovery, reuses compatible intent exemplars when dimensions are detected automatically, and publishes intent vectors through the shared writer. Timings expose ontology embedding activity, provider retries, and local ownership/finalization phases.

- Agent guidance now routes substantive work through bounded Rhizome context, distinguishes governing constraints from supporting evidence, and composes that base contract with starter-specific workflows.
- The Agentic Engineering and Complex Domain starters provide clearer execution, resume, reconciliation, provenance, and traceability flows, with bounded query recipes and additive composition hooks.
- Add `rzm agent code` for selective discovery and typed client generation across the full agent task surface, with one persistent stdio process per client, explicit write authority, bounded calls and cleanup.

- Opening and closing note tabs preserves Home’s configured view, including across reload. Tabs closed before vault status loads stay closed when stored tabs are restored.

- Graphs support pinch zoom and drag panning while leaving ordinary scrolling to the page; zoom and fit controls remain available.

- All workspaces share the compact navigation header. The ontology atlas adds searchable type focus, readable overview labels, tighter placement, and correctly aligned connection routes.

- Opening a different note retains existing file tabs; revisiting a file reuses its tab, including navigation from the Home graph.
- Document links use file paths; previously emitted root fingerprints resolve when their document identity still matches. Failed section links retain a breadcrumb back to the file.

- Notes tabs retain deep-link targets, provide a cached file-root breadcrumb, keep the selected tab visible beside editing controls, and offer retry after failed note reads.
- GraphQL preserves narrative whitespace so consecutive edits retain anchored story identities and indentation.
- Staged, removed, and discarded edits refresh mounted content. Queued narrative typing preserves newer drafts and replays against the preceding edit.
- Home graphs distinguish loading and retryable failure from empty results; collection rows honor Cmd/Ctrl opening beside the current note.

- Embedded-node clicks preserve content-derived identity across offset changes; sidebar navigation, pane history, and edit-session replay follow the selected item. Ambiguous or changed identities fail explicitly, while identical current items remain listable.
- Unlinked criteria remain valid without block IDs. Citation guidance now requires applying and resolving an on-demand block target before persisting its link; planning alone does not make a durable citation.
- Streaming index drains report caller cancellation after flushing accepted writes. Concurrent cache-refresh verification waits for settled cache state without consuming another caller's completion event.
- Ontology materialization version 5 rebuilds derived node identities once on first use. Authored block IDs remain authoritative, and list-item block IDs no longer become section identifiers.
- Note panes render note-backed embedded nodes (user stories, acceptance criteria, list items) instead of "Choose a note", and percent-encoded fragment deep links resolve; the app now writes fragments percent-encoded.
- Node events carry `canonicalRef` when an embedded item is renumbered by an edit above it, so open panes follow the node instead of failing on the stale ref.
- The live watcher no longer hands code files to the ontology-node embedding sync; an embedding-provider failure degrades the epoch and retries the affected notes later instead of forcing a full re-reconcile every few seconds.
- Note discovery prunes ignored directories (`node_modules`, build output) before reading them, and the vault cache keeps serving the last index while a stale-triggered recrawl runs.
- The web graph cache lets each request cancel independently, keeps building for healthy followers when the first requester disconnects, and drains its background work on shutdown.
- Touch-only re-indexes (unchanged content, new mtimes) update freshness evidence only and keep the ontology and graph generations; the derived Intel index upgrades to schema 64 (column-scoped `notes` revision trigger, materialized assessment flags) and ontology materialization version 4, which rebuilds derived ontology state once on first use. Older binaries require a pre-upgrade index backup or an explicit rebuild.
- Ontology summary, atlas and type pages, embedded-node validation, and no-op metadata checks do markedly less work per request or run; browser query invalidation is scoped by vault event kind.
- Neighborhood reads surface note-type read errors instead of silently dropping inbound typed edges.

- Graph-score refreshes batch inserts within the existing atomic transaction and prepare validated arguments before acquiring the writer lock.

- Broad GraphQL roots share a successful metadata inventory within each execution, avoiding repeated vault scans while preserving retry, cancellation and freshness between requests.

- Unchanged indexing preserves identical configuration files, avoiding unnecessary watcher reconciliation and browser invalidation. Genuine configuration and workflow changes still persist.

- `rzm index` now determines work from configuration and freshness. The `--code` and `--semantic` flags and mode-specific rebuilds are removed; use plain `index` or explicit full `index --rebuild`. Disabled code and embedding settings stay disabled.
- Code indexing now rolls back freshness and derived artifacts together when a batch fails or is canceled, allowing the next run to retry incomplete files. Explicit indexing checks file contents even when timestamps are preserved.
- Targeted note reads, catalog projection, typed field sorting, and graph assembly avoid repeated work across unrelated notes and edges. Incremental alias indexing and ownership reconciliation use bounded batch operations.
- Opening linked notes reuses the runtime’s cached reader and avoids parsing unrelated note bodies. Graphs retain their renderers across click-handler changes.
- Web note panes keep stable subscriptions and cancel superseded reads, including rapid navigation and same-file embedded-node transitions.
- Web graph cache checks use a transactional revision instead of scanning graph inputs, and reliably detect same-count edge rewrites. The derived Intel index upgrades to schema 62; older binaries require a pre-upgrade index backup or an explicit rebuild.
- SQLite write retries are shared across stores, honor cancellation during backoff, and return permanent failures promptly.
- Rename the canonical `spec-driven` starter to `agentic-engineering`. Rerunning init migrates starter state, removes only catalog-proven unchanged legacy process docs, retains modified or unproven docs with notices and a tracked `.rhizome/migrations/agentic-engineering/README.md`, and retires superseded managed skills after the replacement router is accepted.
- Managed agent-doc refreshes keep the core Rhizome block in place when template blocks are preserved instead of moving it below them.
- The `agentic-engineering` starter now takes the phase as a router argument (`agentic-engineering plan`); the `specify`, `effort-new`, `plan`, `implement`, and `effort-finish` skills are retired and cleaned up on rerun. Team policy under `docs/engineering/` is factored by concern (testing, quality gates, documentation, review and approval, architecture, release) and outranks skill defaults. Skill overlays may target a skill-relative Markdown file.

## [v0.50.5] - 2026-08-11

- Make `rzm index --rebuild` clobber and recreate the unified SQLite index, including stale WAL/SHM sidecars.

## [v0.50.4] - 2026-08-11

- Indexing now always discovers exact `CONTEXT.md` files without weakening repository containment or ignore rules.
- Semantic-query now uses indexed execution paths that reduce latency while preserving output parity and reporting actionable readiness problems.
- One-shot agent and CLI operations now avoid result-neutral runtime startup, watchers, leader work, and unnecessary stores.
- Effort identifiers now support a filename-derived DATETIME strategy with deterministic allocation and collision reconciliation.
- The release branch now uses migrated DATETIME effort identifiers across its authoritative notes and structured references.

## [v0.50.3] - 2026-07-31

- Improved file-context performance and consistency while preserving indexed retrieval parity and making freshness and truncation behavior explicit.

## [v0.50.2] - 2026-07-23

- Release publishing is now atomic, retry-safe, topology-validated, and supports both main-line releases and release-branch hotfixes.
- Pinned command delegation now uses one shared resolver and reports multi-target results accurately.
- Targeted agent startup now uses a fast minimal path and read-only indexed enrichment for richer context with lower latency.

## [v0.50.1] - 2026-07-18

- Repository configuration now preserves settings it does not yet understand, improving compatibility across versions.
- Validation now provides deterministic identifier repair and link rewriting, while warning when delegated repositories use unsupported configuration.
- Code intelligence now resolves modern TypeScript and JavaScript modules and relationships more accurately across NodeNext, monorepo, ESM, CommonJS, and barrel patterns.

## [v0.50.0] - 2026-07-17

- Added projection-backed validation, deterministic identifier reconciliation, transactional repair operations, CI suites, and executable remediation guidance.
- Consolidated public API and agent surfaces around typed registries and canonical versioned contracts while retiring legacy routes, filenames, compatibility paths, and configuration keys.
- Added first-launch index readiness signaling and retries, plus runtime-aware SQLite locking and safeguards for shared filesystems and WAL recovery.
- Replaced generated answer cards with richer source-owned semantic chunks for ontology and code retrieval.
- Hardened web data lifecycles, restored interface-aware note search and graph document discovery, and restored public workspace projection parity.
- Fixed embedded-node GraphQL resolution for Markdown and code-path collisions while retaining code reference fallbacks.
- Added evidence-grounded release orchestration with reviewable artifacts, curated changelog generation, validation, resumable publishing, and safe partial retries.

## [v0.49.0] - 2026-06-19

- PHP support added for indexing and code intelligence, with better coverage on legacy PHP codebases.
- Sparse-graph fallback keeps hotspot and doc coverage reports useful even when call graphs are incomplete.
- Validation now surfaces next actions and clearer issue states.
- Init/onboarding improved with smoother bootstrap, self-hosted dev binary delegation, and starter management workflows.
- Search and query outputs now provide better ranking, rationale evidence, and tighter validation.
- Spec-driven workflow guidance and agent docs were simplified and expanded for easier use.
- Removed the `RHIZOME_SQLITE_TXLOCK` tuning knob; Rhizome now selects transaction locking internally per operation.

## [v0.48.0] - 2026-05-21

- Better link hygiene checks, with validation catching and fixing more broken or non-Obsidian-friendly links.
- Execution notes now use UTC timestamps and event kinds for clearer effort history.
- Identifier-first Obsidian wikilinks and alias mirroring are now the preferred note-linking path.
- Live serve/index behavior is more reliable with shared SQLite handles and improved freshness events.
- Notes and type navigation now share a single scrollable rail for easier browsing.
- macOS installer PATH registration now fails fast when `/etc/paths.d` cannot be written.

## [v0.47.0] - 2026-05-14

- Add the `complex-domain` starter for source-backed requirements, feature areas, domain models, workflows, and traceability views.
- Add recipes and views for requirement coverage, source review, feature-area backlogs, and domain traceability.
- Add domain-focused skills for requirements ingest, curation, modeling, workflow mapping, spec drafting, traceability review, and domain backport.
- Support starter composition through skill-template overlays, including `complex-domain` extending `spec-driven` skills at render time.
- Add `skill-overlays` validation and `--skill-overlay-manifest` debug output for init runs.
- Switch effort closure to a `Closure Checklist` and remove the old `blocked` / `audit-status` / `backport-status` / `compound-status` workflow fields.

## [v0.46.0] - 2026-05-11

- Added a focused-note **Info** sidebar with related notes, code links, issues, nearby nodes, and local graph
- Added **List/Info** sidebar tabs and automatic Info focus when a pane is selected
- Kept focused note state in the URL `note=` query and clear it when the last pane closes
- Rendered wikilinks and note links now use real web URLs while preserving click-to-open behavior
- Improved stacked-pane focus/scroll behavior for more reliable navigation
- Improved note and section resolution, including structural refs and `name` frontmatter title fallback
- Moved the local graph out of the inline note pane and into the sidebar Info view

## [v0.45.1] - 2026-05-10

- More reliable cache readiness checks during concurrent refreshes.
- Retry certain unique-constraint index failures with a fresh rebuild.
- Release builds now include the frontend assets automatically.
- Improved startup robustness when a crawl is already in progress.
- Fewer false failures from partially stale index state during rebuilds.

## [v0.45.0] - 2026-05-10

- Better configured-table editing with improved overlays, row actions, and inline field editing
- More reliable edit sessions across the web UI and API, including cleaner dirty-state handling and replay behavior
- Fewer ontology/live-sync surprises: indexing, visibility, and locking issues are hardened
- Faster and more accurate configured views from ontology pushdown and full read-model replacement
- Action-items and other views now fail more gracefully, with richer error reporting instead of crashes
- Smoother setup and runtime commands for install, init, serve, and index
- Stricter view/config validation surfaces invalid recipes earlier

## [v0.44.0] - 2026-05-07

- Unified the configured type home and table view experience
- Added direct cell editing for enum, boolean, and relation fields
- Added GraphQL-backed configured views with ordered grouped tables
- Improved ontology table loading with indexed field-value read support
- Added core identity and action item starter templates
- Added item-backed embedded action items and unified type-home entry points

## [v0.43.0] - 2026-05-04

- New public API for GraphQL and REST access to ontology data
- Added an in-app GraphQL explorer for public queries
- Aligned CLI and web GraphQL behavior for parity
- Stricter validation now catches invalid lifecycle states and broken links earlier
- Improved ontology/schema validation for schema-first remediation
- Hardened query recipes for skill metadata and saved query use
- Breaking/behavior change: previously tolerated invalid notes, links, or lifecycle states may now fail validation

## [v0.42.0] - 2026-05-02

- Support nested spec and effort directories without false typed-note classification.
- Keep freeform sibling notes untyped unless they provide the required identifier.
- Add first-class `Plan` notes and updated spec-driven authoring guidance.
- Add `note-by-path` for loading a note and its immediate neighborhood.
- Add batch `next-id` allocation for creating multiple same-type notes at once.
- Add `companion-docs` validation to catch broken companion-doc paths earlier.
- Update docs for durable wikilinks, multi-template installs, and the new id workflow.

## [v0.41.0] - 2026-05-02

- Add safe heading rename commands (`note rename-heading`, `agent note-rename-heading`) that update links and guard fragile external references.
- Switch embedded nodes to identifier-backed block IDs and add validation for orphaned block IDs to reduce broken references.
- Extend `validate` with new checks and auto-fix guidance for fragile external links and block ID issues.
- Promote query recipes to first-class, humanized artifacts with improved CLI (`query-recipe`, `query-recipe render`) and refreshed examples.
- Enhance `init` interactive flow with a settings menu, including toggling the default workflow template.
- Update agent skills, ontology docs, and starters to remove context packs, clarify quality gates, and improve spec-driven workflows.

## Unreleased

- Improve spec-driven story authoring: section-backed embedded nodes now parse metadata bullets, and starter specs use descriptive `USn - Outcome` headings with list-style `#criterion` acceptance criteria.
- Repo launchers now auto-repair missing or stale pinned platform binaries before running normal commands.
- Add `rhizome.devBinaryDir` for self-hosting development repos that must delegate to `bin/<goos>/rzm` without a release pin.
- Promote query-recipe out of `ontology`: `rzm query-recipe {list,show,validate,run}` and `rzm agent query-recipe {list,validate,run}` are the new entry points; the old `rzm ontology query-recipe` and `rzm agent ontology-query-recipe` paths are removed.
- Rename MCP tool `ontology_query_recipe` to `query_recipe`.
- Bump recipe `apiVersion` from `rhizome.ontology-query-recipe.v1` to `rhizome.query-recipe.v1`. Recipes carrying the old envelope now fail validation as `unsupported_api_version`.
- Default `rzm query-recipe list` to a human-friendly table and `rzm query-recipe validate` to grouped per-recipe issue rendering. Add `--json` to either for the legacy JSON shape.
- Add `rzm query-recipe show <id>` printing problem, inputs, GraphQL preview, output contract, adaptation guidance, and a synthesized example invocation.
- Add `rzm query-recipe run --summary` to emit a stderr summary footer alongside the JSON envelope.
- Fix the bundled `story-acceptance-pack` recipe so it selects `UserStory.efforts` (plural) to match the live schema.

## [v0.40.0] - 2026-04-30

- Add integrated agent chat surface with richer retrieval tools and provider/model filtering.
- Introduce saved ontology query recipes (YAML + GraphQL variables) and `query_recipe` CLI support.
- Align spec-driven and new project-kb starters around AO-KB query recipes and phase-oriented skills.
- Document rationale and subsystem flows for search, ontology, indexing, and agent behavior.
- Upgrade ontology read/query runtime for more robust graph reads, answer cards, and runtime queries.
- Tighten validation and lifecycle rules (including frozen-scope drift detection) to surface vault/ontology issues earlier.
- Refine LLM provider handling and model metadata; model selection behavior may differ when defaults are used.

## [v0.39.0] - 2026-04-28

- Introduced NodeRead-based ontology graph reads and embedded node IDs, improving graph navigation, local graphs, and type detail UX.
- Enhanced search and answer engine with ontology-aware answer packets, a stronger card index, and refined semantic ranking.
- Improved unified indexing for ontology, notes, and code, fixing ontology-aware note chunking/indexing and reducing regressions.
- Added manifest-backed install and `rzm update` flow for safer, version-pinned installation and upgrades.
- Expanded skills and guidance with new refactor-planning and development-loop skills plus richer spec-driven/ontology docs.
- Improved `rzm init` with better credential rejection/skip handling and new environment configuration helpers.
- Breaking: retired reorganize commands in favor of refactor-planning; ontology schema and search behavior are more ontology-driven and may alter existing queries and navigation.

## [v0.38.0] - 2026-04-20

- Revamped ontology UI (type detail, node views, styling) for clearer graph exploration and navigation.
- Introduced ontology type-instance handling to better represent and inspect concrete entities.
- Improved search locality and repair logic to return more relevant ontology results and recover from partial data.
- Refined Notes and Explorer Workspace components and added tests to increase stability of the web experience.
- Standardized userstory endpoint on canonical browser identity for more predictable cross-session behavior.
- Renamed installer entrypoint to `install-rzm.sh`; update scripts or docs that referenced the previous name.

## [v0.37.0] - 2026-04-18

- Add Ontology Atlas workspace for interactive exploration of note types and their relationships
- Improve ontology graph layout and edge routing for clearer, more readable diagrams
- Introduce ontology note and type detail panes for in-place inspection of schema and related notes
- Enhance Notes workspace with improved type-aware navigation and hierarchical note-type rail
- Unify graph styling and type accent colors across ontology and notes views
- Increase test coverage for ontology and notes UI components to improve stability and confidence

## Unreleased

- Register the macOS user install path in `/etc/paths.d/rhizome` so desktop apps can discover `rzm`.
- Add manifest-backed `rzm update` with repo version pinning via `.rhizome/config.yml`.
- Replace the installer zip flow with a hosted `install-rzm.sh` bootstrap that supports user installs and repo launchers at caller-chosen paths.
- Delegate normal global `rzm` commands to the repo-pinned platform binary when `rhizome.version` is configured.
- Prefer `make build` output at `bin/<goos>/rzm` when running inside the Rhizome source checkout.
- Refresh managed repo launchers during `update --pinned` and guard against overwriting unmanaged launcher files.
- Publish latest and versioned S3 artifacts with SHA256 checksums while keeping legacy secret-suffixed archive aliases.
- Add Ontology Atlas page (`/ontology`) to the web app: hub view with stats, schema ER diagram (Mermaid), and per-type detail pages showing matchers, fields, typed relations, implemented interfaces, companion docs, and example notes.
- New `GET /api/ontology/atlas` endpoint and path-style `GET /api/ontology/types/{name}` (replaces the older `/api/ontology/type?name=` query form). Extended `TypeDoc` JSON with `locator`, `propertyCase`, `semantics`, `implements`, and `companionDocs`.

## [v0.36.1] - 2026-04-17

- Add macOS installer zip (`install-rzm.command` + `rzm` launcher) for simplified one-time installation.
- Publish the macOS installer zip as an extra file in GitHub Releases.
- Document a stable macOS installer S3 URL in the README alongside existing platform binaries.
- Update `make cut-release` flow to publish GitHub artifacts and stable S3 downloads, including the installer, in one step.

## [v0.36.0] - 2026-04-17

- Introduced ontology-driven Notes workspace with dedicated home views (all, modified, issues, by type).
- Enhanced ontology editing UI with inline body/narrative editing, property panels, and improved identity display.
- Expanded validation system with cached results, richer checks, and staged autofix workflows.
- Added UI issue widgets for broken links, duplicate preferred identifiers, and generic validation issues.
- Documented and scaffolded ontology-driven transcript ingestion, including starter templates.
- Improved semantic index migration and embed owner handling for more robust indexing and search.
- Updated OpenAPI schema and generated web client to cover new notes, ontology, and validation endpoints.

## [v0.35.0] - 2026-04-15

- Expand `rzm agent surface`:
  - Return a curated, ordered command list with `source` and `category` metadata.
  - Advertise discovery tools (`semantic-query`, `files`, `file-context`, `report`, ontology commands) and note-safe operations like `note-move`.
  - Document a canonical agent retrieval loop and session usage in the surface notes.
- Harden indexing:
  - Add automatic detection of corrupt or schema-incompatible SQLite indexes and trigger a full rebuild when needed.
  - Factor index rebuild into a reusable helper and improve error messages instead of failing with opaque SQL errors.
- Improve note search and `list` performance:
  - Index normalized note search terms (path, title, content segments) in a new `note_search_terms` table.
  - Use the metadata store to pre-filter candidates for `find:` and boolean expressions, falling back to fuzzy matching only when necessary.
- Make `rzm serve` more robust:
  - Defer runtime shutdown until after the HTTP server stops and background goroutines complete.
  - Attach the watcher hub to the web runtime so browser surfaces can observe live note changes.
- Evolve the ontology browser workspace:
  - Introduce a canonical node workspace graph (heterogeneous nodes, typed edges, derived views) as the primary browser contract.
  - Add SSE-first node event plumbing (`/api/ontology/events`) and node-scoped workspace APIs (`/api/ontology/node-workspace`, `/api/ontology/nodes/resolve`) behind the UI.
  - Update the web app to consume node-centric workspaces, unify structural/section handling, and avoid duplicate section rendering.
- Clarify inline property and schema authoring:
  - Update authoring guides to prefer one `key:: value` per line for inline properties and avoid list bullets for metadata.
  - Align embedded/section schema guidance and examples with the new inline property stance.
- Add `foundation-review` agent skill:
  - Ship a new skill for reviewing foundational architecture phases, wired into `.agents`, `.claude`, and starter templates.
  - Document it in the agent workflow as the canonical “Review” phase between implement and audit.

## [v0.34.0] - 2026-04-13

- Add ontology workspace to the web app with URL-based ontology note navigation and improved graph views.
- Default typed notes to a structural view for clearer, spec-driven editing.
- Introduce validation CLI with deterministic fixes and fix-plan reporting, plus a rationale CLI with confidence-aware graph analysis.
- Consolidate Rhizome docs into a spec-driven knowledge base, including ontology section types, starter patterns, and requirements-only specs.
- Implement ontology edit session read/write APIs and web UI support for interactive ontology editing.
- Enhance Obsidian alias handling and coderef parsing (including HTML/CSS comment families) for more reliable anchors and references.
- Optimize indexing and semantic sync (call-edge streaming, writeback batching, web graph caching) for better performance on large repositories.

## [v0.33.1] - 2026-04-07

- Streamlined `serve` stderr output in non-debug mode, reducing background indexer and watcher noise.
- Added compact, color-aware C/M/D indicators for watcher file activity on long-running sessions.
- Improved progress bar log filtering so important lines are kept even when prefixed with timestamps.
- Fixed background indexing cleanup to restore logging correctly and emit a clear “Watching file system…” ready message.
- Refined semantic indexing progress reporting, including better unchanged/skip messages and a final “Index complete” for notes-only runs.
- Updated internal gitignore to exclude SQLite database artifacts from Rhizome’s internal repository.

## [v0.33.0] - 2026-04-07

- Added ontology support and documentation for GraphQL interfaces and section contracts, including section-local neighbor traversal.
- Expanded ontology reference docs with guidance on property naming, interfaces, sections, query usage, and revision workflows.
- Updated bundled example ontologies (project KB, codebase docs) to showcase typed sections and interface-backed contracts.
- Introduced `rhizome-skill-creator` skill plus reference notes for building Rhizome-aware Agent Skills from live repo surfaces.
- Renamed skills to clarify scope: `rhizome-docs` → `rhizome-code-docs`, `rhizome-markdown-authoring` → `rhizome-note-authoring`, and updated all callers.
- Corrected ontology compiler check so `@section` fields must target `Section` or a concrete type that implements `Section`.
- Removed deprecated `.mcp.json` configuration and adjusted `.rhizome/.gitignore` to reflect current Rhizome database handling.

## [v0.32.0] - 2026-04-06

- Strengthen ontology validation: `rzm ontology validate` now enforces schema/typed-note contracts, flags unresolved internal note links with line/link details, and prints per-type note inventories.
- Add ontology sections: introduce `@section(level:, heading:, required:)`, `SectionLevel`, and the `Section` interface for heading-derived body structure with subtree-scoped `@neighbors`.
- Add ontology interfaces: support GraphQL `interface` / `implements` for shared contracts, including interface-aware validation, reference docs, authoring guides, and query fragments.
- Enhance ontology tooling surfaces: reference/authoring docs and schema views now show type roles (note/section/interface), implemented interfaces, section bindings, and neighbor scopes.
- Extend ontology query engine: allow querying sections (built-in `Section` fields, nested section types, subtree neighbors) and using interfaces in roots, fragments, and ambient relations.
- Simplify agent integration: consolidate MCP tooling behind `agentapi`, removing legacy MCP server/adapters while keeping startup responsive via the existing async runtime.

## [v0.31.0] - 2026-04-06

- Introduced a full ontology subsystem for notes and skills (GraphQL SDL schemas, CLI commands, and guides) to support ontology-driven authoring and querying.
- Switched from MCP to an agent CLI + serve runtime as the default integration surface, with `.agents` skills and refreshed Rhizome skill templates.
- Enhanced `rzm init` to manage `.rhizome` (including gitignore), add a Rhizome skill-creator template, and streamline onboarding docs and workflows.
- Changed default note property naming to kebab-case and added `rzm list` / `rzm properties` tooling for inspecting and working with note metadata. **Breaking:** update any configs or queries that relied on old property names.
- Re-architected the indexing and embeddings pipeline (single-writer, batched writes, sqlite-vec) for more reliable runs, fewer lock issues, and clearer unified index progress.
- Improved semantic and ontology-aware search with locality-aware retrieval, better seed-local behavior, and correct enforcement of traversal `maxDepth`.
- Added `rzm serve` and related agent/ontology/query commands, plus web runtime updates, to provide more stable, ontology-backed agent sessions.

## [v0.30.0] - 2026-02-09

- Speed up indexing with parallel note/code ingest, content-hash + mtime guards, and adaptive batched writers; unchanged files are skipped more reliably.
- Move unified search and code-intel/indexing orchestration into shared app packages, simplifying `rzm code search` and `rzm index` behavior and making future tuning safer.
- Tighten SQLite usage for unified indexes: per-open tx-lock options, 1 GiB mmap default, and explicit WAL checkpoints after batch index / graph maintenance.
- Make MCP watching more resilient: better handling of Windows directory deletes, suppression of noisy cache paths, and coalesced stale/resync events to avoid hot loops.
- Extend MCP + agent integration: Cursor skills are now generated by `rzm init`, MCP tool list is complete, and skill docs include Cursor tool-name conventions.
- Clarify docs around unified code index design, concurrency, and rebuild behavior, including where to look for reverse-index and checkpoint logic.
- Behavior change: tx-lock is no longer controlled by mutating `RHIZOME_SQLITE_TXLOCK` at runtime; callers should rely on DSN/open options instead.

## [v0.29.0] - 2026-02-04

- Optimize call-edge indexing with a reverse index and def-delta based rebuilds, so only changed and impacted files have call edges recomputed (with deterministic logging and safe fallbacks).
- Extend `semantic_query` with explicit `mode` support (including per-query modes), richer intent catalog (`search`, `docs_for_code`, `related_to_seed`, `overview`, `code_for_docs`, `find_usages`, `go_to_def`, `explain_symbol`, `tests_for_code`, etc.), and response metadata (`modeApplied`, `modeDetected`, `modeScore`, `warnings`).
- Add dedicated retrievers for definitions, call edges, and tests-for-code, improving IDE-style intents (go-to-definition, find usages/callers/callees, tests for a file) while avoiding vector noise.
- Harden code indexing: introduce tree-sitter parse timeouts and limits, preserve previous intel on parse failures/timeouts, and improve Python/TS/C# call/type-ref extraction and logging.
- Switch ignore handling to go-git’s gitignore implementation with root + nested `.gitignore` support and clearer directory negation semantics, aligning indexing/search with Git behavior.
- Enhance compression and embeddings: add token-aware chunked compression with parallelism and Cerebras-tuned defaults; integrate Voyage AI as an embeddings provider with token-aware batching and team API key support.
- Improve robustness of env and MCP tooling by tolerating invalid `.env` keys and malformed YAML frontmatter, and by enforcing `mode` (not `intent`) for MCP `semantic_query` calls.

## [v0.28.0] - 2026-01-16

- Add intent-driven LLM compression for tools and contextpacks, with new principle-based prompts and a compression cache (now the default; can be tuned/disabled in config).
- Introduce Contextpack Hub and expanded documentation for defining, sharing, and reusing contextpacks.
- Harden vault watcher behavior with retries and fsnotify-based fallbacks to reduce missed updates and improve reliability across platforms.
- Improve `init` command idempotency, diff UX, and add `--reject-all` to quickly decline all proposed template/config updates.
- Enhance code anchors with suffix matching and a `code anchors validate` command for safer, more flexible anchor references.
- Migrate MCP server to the new async LiveRuntime for more robust long-running sessions and capability-based tools.
- Add Cerebras LLM provider and refine embeddings configuration (preserve explicit overrides; remove redundant fields).

## [v0.27.11] - 2026-01-14

- Improve SQLite integrity checks with retries and a longer timeout to better handle slow or locked databases.
- Avoid retrying when actual corruption is detected, keeping corruption reporting accurate.
- Extend integrity-check timeout to 15s to better support slow filesystems (e.g., remote dev environments).
- Add detailed diagnostic logging to the filesystem watch hub for incoming events and watcher lifecycle.
- Log when internal, ignored, or out-of-vault paths are dropped to make watch behavior easier to understand.
- Add logging for recursive directory walking and watch additions to assist in debugging missing file events.

## [v0.27.10] - 2026-01-14

- Filter out internal `.rhizome/` events at the watcher backend level to avoid `db.sqlite-wal` flooding the event buffer
- Improve watcher stability and responsiveness on large or busy vaults by reducing noise from internal database writes
- Prevent unnecessary rescans and downstream reactions to changes in `.rhizome/` internals (no breaking changes)

## [v0.27.9] - 2026-01-14

- Avoid unnecessary `RebuildAllCallEdges` on boot when call edges are already present, improving startup performance
- Add `HasAnyCallEdges` check to the SQLite store to detect existing call edges efficiently
- Stop attaching file-system watches to the `.rhizome/` directory to prevent event buffer flooding from database files
- Continue tracking `.rhizome/ignore` changes while filtering other `.rhizome/` files from watcher events
- Improve overall watcher robustness and reduce risk of buffer overflows in large or busy vaults

## [v0.27.8] - 2026-01-14

- Reduce verbosity of filesystem watcher (fsnotify/FSEvents) logging
- Continue logging user-relevant file events for visibility into changes
- Simplify internal watcher logging while preserving error diagnostics
- Keep watcher behavior and defaults unchanged (no breaking changes)

## [v0.27.7] - 2026-01-14

- Improve watchhub diagnostic logging for filesystem watcher behavior
- Log backend creation and FS notification enablement to aid troubleshooting
- Add detailed logs for root additions, recursive walks, and already-walked paths
- Log handling of internal ignore roots and final watcher setup state (including watch counts)

## [v0.27.6] - 2026-01-14

- Fix fsnotify watcher not receiving events in leader/follower mode when the backend is enabled after startup  
- Ensure watch roots are only marked as walked when a backend is present, allowing proper re-walk on later fsnotify enablement  
- Improve reliability of file/secret change detection in clustered and delayed-initialization setups

## [v0.27.5] - 2026-01-14

- Toned down repeated debug logging during graph score lock contention for clearer diagnostics
- Improved lock contention message to explicitly note when another process is indexing
- Added a summary log of total watched paths after watcher setup to assist troubleshooting
- Preserved existing graph scoring and watcher behavior; no breaking changes or default flips

## [v0.27.4] - 2026-01-14
- Treat MCP server `context.Canceled` as a normal shutdown instead of a fatal error
- Add debug logs for successful and failed code root registration in MCP/codeanchor
- Log when the code watcher is ready, including counts of note and code roots
- Improve embedding watcher logs with duration and error counts for note embedding runs
- Increase debug output to make MCP and embedding behavior easier to diagnose without altering user-facing defaults

## [v0.27.3] - 2026-01-14

- Improve MCP startup time by opening the session store asynchronously so the initial handshake is not blocked
- Make MCP connections more responsive for clients while background session initialization completes
- Ensure session cleanup is started only after the session store is successfully opened
- Update MCP documentation to explicitly require async handling for session store opening alongside other slow operations

## [v0.27.2] - 2026-01-14

- Fix MCP handshake timeouts by ensuring initialization is async and non-blocking
- Run directory watch root enumeration in a background goroutine to avoid blocking MCP startup
- Move session cleanup to an asynchronous task so clients connect reliably even on large stores
- Document MCP startup responsiveness requirements in AGENTS and indexing pipeline reference

## [v0.27.1] - 2026-01-14

- Fix edge re-indexing so existing code edges are correctly refreshed, improving code graph accuracy.
- Treat unchanged-but-touched files as “touched” in the index and refresh their mtimes, reducing stale code intel.
- Clarify incremental indexing stats by accurately counting `Indexed` vs `Unchanged` files.
- Add store support for bulk mtime refresh of code paths to make incremental indexing more reliable.
- Correct `init` config saving so updated embedding provider settings are written to the proper project config path.
- Refine Rhizome docs/onboarding guidance for better agent use of CONTEXT and note tools (docs-only).

## [v0.27.0] - 2026-01-14

- Optimize incremental indexing: rebuild call edges and wikilink graph only for changed files/notes to speed up `rzm index` on small edits.
- Harden SQLite usage: distinguish real corruption from transient busy/locked errors and gate destructive recovery on index-lock ownership.
- Improve CLI/MCP coordination: background semantic embedding and graph scoring defer when CLI requests priority via the shared index lock.
- Require explicit embeddings provider configuration (breaking): semantic search and embeddings now fail fast unless a provider is set; `rzm init` upgrades old configs by prompting for a provider or disabling embeddings.
- Preserve embeddings `provider` in config to avoid silent default changes between releases.
- Track anchor changes more precisely so scope recomputation and graph scoring only run when anchors or notes actually change.

## [v0.26.0] - 2026-01-13

- Change default embeddings provider to Ollama, add provider-choice prompts in `rzm init`, and wire Ollama endpoints into `.rhizome/config.yml`.
- Optimize MCP context tools: reuse `sessionId`, prewarm graph analysis on server boot, and refine `vault_context`/`file_context` guidance for once-per-session usage.
- Extend `file_context` with `submoduleDepth` to include submodule `CONTEXT.md` docs, and tighten vault_context output to focus on high-signal module docs.
- Add interactive, diff-based template updates to `rzm init` with per-hunk approval, persistent rejection tracking, and new `--skip-rejected` / `--clear-rejections` flags.
- Harden SQLite index lifecycle: safe WAL sidecar cleanup, embeddings-domain repair, and more conservative purge behavior to avoid corruption under concurrent MCP/index use.
- Enable FTS5 in CI/tests and builds, add a vendor patch for go-sqlite3 FTS5, and switch to native per-OS builds instead of cross-compiling.
- Reduce default context packing budget from 150k to 50k chars and update Rhizome docs/skills (specify/plan/implement, hub-notes, docs) to favor token-dense, two-tier note models.

## [v0.25.1] - 2026-01-10

- Enabled SQLite FTS5 in all release binaries for improved full-text search
- Updated `go install` instructions to include `-tags fts5` for consistent behavior with releases
- Clarified that local builds without `-tags fts5` will not include the enhanced search capabilities

## [v0.25.0] - 2026-01-10

- Optimize unified indexing to rebuild call edges only for files that actually changed, speeding up incremental runs.
- Auto-detect scope config changes (vault includes/excludes, code roots) and invalidate mtime caches so new in-scope files are picked up without `--rebuild`.
- Switch all SQLite usage to `mattn/go-sqlite3` with FTS5 enabled by default for intel and embeddings stores.
- Introduce explicit write-access mode for anchor services, making read-only commands (e.g., `rzm code explain`) avoid index writes.
- Add scope-config hash metadata and mtime-cache invalidation APIs to the intel store for more predictable reindex behavior.
- Breaking: remove Windows arm64 release target and associated documentation; Windows support is now amd64-only.

## [v0.24.0] - 2026-01-10

- Add `rzm changelog` command backed by an embedded `CHANGELOG.md` for easy release history inspection
- Recompute code anchor scopes when notes defining `code-anchors` are ingested, even if no code files changed
- Improve codeanchor watcher with periodic deletion reconciliation on Linux/Windows to clean up stale indexed paths
- Tighten RHIZOME.md and MCP usage templates with explicit `vault_context` session rules and code-anchor authoring guidance
- Refine ignore system documentation, splitting behavior vs. implementation notes to make configuration and debugging clearer
- Enhance release script to manage version files, embedded changelog, tags, and cleanup automatically during `cut_release` runs

## [v0.23.0] - 2026-01-10

- Switched macOS vault watching to an FSEvents backend to avoid file descriptor exhaustion on large trees
- Added shared write mutexes and more aggressive WAL checkpointing across MCP stores to improve SQLite index stability
- Fixed semantic chunk hash computation and unified hashing between note and code indexers for correct embedding cache reuse
- Clarified and implemented indexing pipeline concurrency + batching requirements (per-path transactions, parallel-parse/serial-write, provider batching)
- Updated `rzm init` to remove stale rhizome.* prompts/skills, standardize on `rhizome-` names, and keep agent artifacts in sync
- Expanded RHIZOME.md and related docs with a unified documentation philosophy (agents as primary audience, tests as executable docs)

## [v0.22.5] - 2026-01-10

- No user-facing changes; behavior matches v0.22.4.
- Internal maintenance only; no new features or bug fixes.
- No breaking changes or default flips in this release.
- No configuration changes or migrations required.

## [v0.22.4] - 2026-01-10
- Avoid duplicate `RebuildAllCallEdges` execution to reduce redundant analysis work
- Improve performance and stability for workflows that trigger repeated call-graph rebuilds
- Ensure `RecomputeAnchorScopes` respects completed call-edge rebuilds via internal state tracking

## [v0.22.3] - 2026-01-10

- Parallelized `RebuildAllCallEdges` to speed up code-intel indexing on larger projects.
- Added periodic progress logging during call-edge rebuilds for better visibility into long runs.
- Improved robustness of call-edge rebuilding by continuing past individual file errors.
- Updated `cut_release.sh` to present explicit major/minor/patch version choices with a suggested option.
- Changed release script fallback to default to a minor version bump when LLM suggestions are unavailable and surfaced LLM reasoning text.

## [0.22.2] - 2026-01-10

- Speed up `RebuildAllCallEdges` by batching call/import/type-ref edge writes to the intel store
- Add `UpsertIntelCallEdgesForPathsBatch` to SQLite store for transactional, batched edge upserts
- Preserve compatibility for non-batch intel stores by falling back to per-path edge updates

## [v0.22.1] - 2026-01-10

- Fix duplicate `doc_links` constraint violations in SQLite-backed stores by using `INSERT OR REPLACE`
- Improve reliability of `ReplaceDocLinksForPath` and batch variants when re-indexing documentation links
- Ensure doc link updates consistently overwrite existing records instead of failing on duplicates

## [v0.22.0] - 2026-01-10

- Speed up note and code indexing by increasing batch sizes to 500 items per write lane.
- Batch note upserts into single transactions to reduce database round-trips during indexing.
- Batch doc link writes for both notes and code, improving performance on large projects.
- Clarify that multi-process leader/follower watcher mode is enabled by default, with optional config/CLI overrides.
- Improve robustness and throughput for large vaults by reducing per-file transaction overhead.

## [v0.21.0] - 2026-01-10

- Speed up code indexing by batching file summary updates into a single transaction
- Improve consistency of symbol, inheritance, and annotation data when files are reindexed
- Reduce chances of partial index updates on failures through atomic batch operations
- Optimize SQLite index writes by reusing prepared statements for batched summaries
- Fix S3 release publishing script to correctly match goreleaser output directories across OS/arch targets

## [v0.20.0] - 2026-01-10

- Enable CGO in release builds and add cross-compilation toolchains so tree-sitter indexing works on macOS, Linux, and Windows amd64.
- Add index lock priority mechanism so CLI index commands can preempt background MCP indexing instead of waiting indefinitely.
- Update MCP background index, semantic embed scheduler, and graph score scheduler to use yielding heartbeats that respect CLI priority and re-queue remaining work.
- Refine graph edge weights and doc scoring so documentation links drive importance, with tuned weights for calls, type refs, imports, and tests.
- Remove redundant embedding cache writes during semantic sync to reduce indexing overhead on large repositories.
- Refresh banner copy and expand documentation on Rhizome’s purpose, release build requirements, and recommended documentation structure (CONTEXT.md, in-code docs).

## [v0.18.0] - 2026-01-09

- Add `rzm graph web` command to mirror web UI graph endpoints from the CLI, with JSON and timing output.
- Extend Python and TypeScript indexers to emit import and type-reference edges, enriching the code graph used by search and navigation.
- Introduce a centralized edge-kind registry to control weights and priorities for calls/imports/tests/type refs, fixing under-weighted import/test edges in graph-based ranking.
- Optimize graph construction (global, local, and module views) with edge aggregation, module collapsing, and cached ignore checks for better performance on large vaults.
- Normalize embedding storage so the cache is the primary source of truth, add pruning of stale cache entries, and route chunk/item queries through the cache.
- Improve embedding sync robustness with expanded OpenAI/network retry logic and progress that separates cache reuse from new embedding work.
- Ensure release tooling updates the in-repo version so `rzm --version` correctly reports v0.18.0.

## [0.17.0] - 2026-01-08
- Speed up unified indexing with better concurrency, planning, and mtime-based short-circuiting
- Add automatic recovery for corrupted SQLite index databases to reduce rebuilds and failures
- Improve Go and Python code indexing and fix missing web graph edges for test files
- Add markdown link rewriting on note rename/move and prefer markdown links for coderefs by default
- Switch search embeddings to oai provider and refresh skills/onboarding documentation
- Refine web UI graph styling, colors, and file tree for clearer navigation
- Expand and reorganize MCP tools for semantic queries, files/context, graph, and health operations

## [v0.16.0] - 2026-01-07

- Speed up incremental code and note indexing with mtime-based skips, batched SQLite writes, and conditional call-edge/scope recomputation.
- Improve unified index and semantic embedding concurrency with shared write locks, coordinated provider concurrency, and parallel planning/embedding.
- Make filesystem watching case-insensitive on Windows/macOS for watcher paths, watch roots, and pending events to reduce missed/duplicate updates.
- Harden MCP leader transitions by enabling FS notifications only after subscribers are registered, and add exponential backoff/retry for semantic embeds and graph score rebuilds.
- Add S3 release utilities: `make release-s3-check` for AWS preflight validation and `make release-s3-dry` for dry-run uploads; integrate preflight into `cut_release.sh`.
- Enhance SQLite-based intel and embedding stores with centralized write helpers, schema-rebuild safety, and periodic logging of write wait/hold times.

## [v0.15.0] - 2026-01-06

- Add bundled web UI (`rzm web`) with graph and search views for interactive vault exploration.
- Enhance semantic search: type filters, query normalization, better packing, and fixes for non-code vaults.
- Speed up and harden indexing with background indexing, unified WatchHub, index locks, and fewer unnecessary resyncs.
- Improve onboarding and init flows with updated MCP-aware templates, agent skills, and RHIZOME.md/CONTEXT.md docs.
- Add Ollama-first embeddings defaults, batching, and safer rebuild behavior; refine OpenAI retries and embedding reuse.
- Upgrade code intelligence with better language indexers, unified note/code embeddings, and tuned graph/community scoring.
- Centralize path handling and ignore rules for more reliable behavior across platforms, especially Windows.

## [v0.14.0] - 2025-12-17

- Add `--use-fts-body` option to unified search to render code snippets from the FTS code index (faster, includes doc comments; falls back to file reads when unavailable).
- Enhance `rzm code overview` with directory-level aggregation, graph-based ranking, CONTEXT.md summaries, submodule listings, better test detection, and a new `--depth` flag.
- Improve embeddings performance and resilience: raise OpenAI concurrency caps and introduce generation-based lazy pruning for note and code indexes to preserve useful items across branch switches.
- Index Go package-level doc comments into anchors and semantic chunks so package docs appear in search and context.
- Add `--no-config` flag to `rzm init` to update project templates without modifying existing config.
- Tidy indexing UX and config writes: drop verbose per-item embedding logs from `rzm index` and limit `SaveCodeConfig` to minimal local code settings.
- Add and wire new documentation hubs and reference notes for search, retrieval, relevance, graph algorithms, vault config/ignore, and knowledge handles.

## [v0.13.0] - 2025-12-17

- Generate a standalone `RHIZOME.md` during `rzm init` and switch AGENTS/CLAUDE/Cursor/Codex harnesses to reference it instead of embedding Rhizome guidance.
- Restructure `.rhizome/config.yml` to use top-level `noteEmbeddings`, `codeEmbeddings`, `graph`, and `agents` keys; `rzm init` migrates existing `agent:` configs in-place.
- Change default code query embeddings to `text-embedding-3-small`, inherit provider/endpoint from note embeddings, and auto-reset code indexes when metadata (model/provider/dimensions) changes.
- Move logs to `.rhizome/logs.d/` with a `.rhizome/logs` symlink to today’s file, and always print logging setup warnings even without `--debug`.
- Improve `rzm init` robustness: handle `.claude` as a file, tolerate problematic symlinked directories, add `--agentsmd` to disable `AGENTS.md` updates, and treat some path issues as non-fatal warnings.
- Drop legacy `appliesToAnchors` frontmatter and CLI output; code anchors now rely solely on `code-anchors` definitions.

## [v0.12.0] - 2025-12-17

- Unify note + code embeddings and intel graph into a single index, improving semantic linking between notes and code.
- Introduce unified semantic + code search and code-intel graph (anchors/edges), with new CLI commands for code search, overview, analytics, and relatedness (including C# and multi-language support).
- Optimize search and indexing performance via SQL-filtered vector search, batch indexing, adaptive retriever concurrency, and an embedding reuse cache; preserve embeddings across schema rebuilds.
- Make MCP startup asynchronous with readiness-gated tools, fixing init blocking and improving reliability of semantic tools.
- Expand and refine AGENTS/init pipeline: agent-agnostic harness templates, richer code-intel guidance, and persisted agent mode overrides.
- Add lexical prefiltering for notes and adaptive retriever timing to improve result relevance and reduce unnecessary work.
- Breaking: migrate to a unified SQLite schema and reorganized packages (`app`, `vault`, `anchors`, `search`); code-intel and embedding data will be migrated and some CLI code commands have changed.

## [v0.11.0] - 2025-12-12

- BREAKING: `graph file-context`, `graph vault-context`, and MCP `file_context`/`vault_context` now return budgeted, LLM-optimized text output by default (instead of JSON).
- Add code anchor indexing for Go and TypeScript/JavaScript (Tree-sitter), including improved matching (glob/dir anchors, TS import path-tail fallback) and updated CLI/MCP wiring.
- Add `index` command to manage semantic + code indexes together (`--status`, `--rebuild`, `--semantic`, `--code`).
- Improve `file_context` output: directory-aware context, explicit output budgeting (`budgetChars`), frontmatter stubs/blessed-frontmatter surfacing when content is omitted, and expanded linked notes for hubs/MOCs.
- Add optional inclusion of context docs in graph analysis (with cache key support and tests).
- Refine `vault_context` defaults/payload shape (trim components/topAuthority payload, hide components by default).
- Streamline `init` (AGENTS.md Rhizome section generator + templates) and add CLI banner/logo assets.
- Add comprehensive docs covering graph analysis, list/prompt DSL, coderefs, code anchors, and embeddings; add a polyglot integration fixture + integration tests.

## [v0.10.0] - 2025-12-09

- Add `graph file-context` CLI command and MCP `file_context` tool (alias: `note_context`) to return graph + community context for notes and documentation context for code files (linked notes, ancestor `CONTEXT.md`, inferred communities).
- Add optional code reference scanning (configured via `.rhizome.yaml`) to index `[[wikilinks]]` and `@NotePath` mentions in source code; expose `codeRefs` in `files`/`file_context` responses and apply a soft authority boost to referenced notes.
- Support collection-style vaults with `vault add` options (`--root`, `--includes`, `--excludes`, `--links`) and enhanced `vault list`; semantic indexing and MCP caching now respect glob-based includes/excludes.
- Add `graph broken-links`, `graph dead-ends`, and `graph stale` commands to surface missing wikilinks, inbound-only notes, and notes stale by modification time.
- Add `vault_health` MCP tool to return a JSON health report (brokenLinks, staleNotes, deadEnds, suggestedMerges) with configurable thresholds and filters.
- Update `rename` and `move` commands to optionally rewrite code references when code ref scanning is enabled, reporting code ref update counts.
- Wire CLI version to `pkg/version.Version` and set it via GoReleaser ldflags for accurate release reporting.

## [v0.9.0] - 2025-12-08
- Improve `semantic search` to interleave chunk matches across notes and rank notes by average chunk relevance; `--limit` now controls total chunks (default 25).
- Add `semantic find-connections` (alias: `similar`) to discover notes connected to a given note via stored chunk embeddings, with chunk-level context.
- Change MCP `semantic_query` to return per-note groups with aggregate scores and chunk arrays, matching the CLI’s chunk-based search model.
- Add MCP `find_connections` tool to expose embedding-based note connections (with query/match metadata and reasons) to agents.
- Simplify `graph note-context` output by replacing the `related` notes list with a single embedding-based `similarity` score to top community notes.
- BREAKING: Remove `semantic search --chunks` flag and change `--limit` semantics; scripts depending on the old note-level search output may need updates.

## [v0.8.0] - 2025-12-07

- Add opt-in semantic search CLI (`rhizome semantic index/search/similar/status/enable/disable/rebuild`) using OpenAI or local Ollama embeddings
- Wire semantic index into MCP: `note_context` now includes `related` semantic notes; new `semantic_query` tool returns chunk-level matches with text
- Simplify MCP surface: remove `community_detail`, drop daily-note tools, and streamline `vault_context` to a compact summary plus optional `note_context` payloads
- Enhance graph commands: `graph note-context` accepts file arguments instead of `--files` and can include semantically similar notes; `graph vault-context` defaults to concise output with `--all` for full detail
- Improve semantic index UX: progress bar during indexing, `semantic status` for provider/index metadata, deterministic test provider, and automatic gitignore entries for the SQLite index
- Strengthen cache and watcher reliability, including `.obsidianignore`-aware resyncs, better dirty-path tracking, and reduced SQLite lock contention during background updates

## [v0.7.0] - 2025-12-07
- Added `graph vault-context` and `graph note-context` commands to emit JSON vault and per-note graph context (communities, hubs/authorities, neighbors, backlinks, recency).
- Replaced PageRank with HITS hub/authority scores across graph stats, communities, and MCP responses, with CLI output updated to show both hub and authority metrics.
- Turned on multi-hop recency cascading by default for graph analysis (CLI and MCP); use `--recency-cascade=false` or `recencyCascade:false` to opt out. **Default behavior change.**
- Enriched MCP graph tools (`community_list`, `community_detail`) and added `note_context` / `vault_context` tools with authority distributions, recency summaries, bridge strength, weak components, and key-note/MOC detection.
- Parallelized graph build and recency computation, reused cached note metadata (including derived content times), and fixed a Windows watcher deadlock for more reliable, faster runs on large vaults.
- Increased default graph listing limits (e.g., `graph --limit` now 100) and added optional timing output for graph commands and MCP to inspect analysis cost.

## [0.6.2] - 2025-12-06
- Refresh analysis cache providers before using cached backlinks/graph data for more accurate results.
- Prevent the files MCP tool from mutating base `SuppressedTags` when per-call overrides are supplied.
- Reload `.obsidianignore` patterns on crawl/resync so ignore changes apply without restarting.
- Clarify daily note MCP tool description (no longer claims to create missing notes).
- Update `move_notes` MCP tool comment to reflect default backlink rewriting behavior.
- Simplify release script to pass the generated release notes file directly to GoReleaser.

## [0.6.1] - 2025-12-06

- Fix Windows-specific issues (path separators, JSON escaping, permission/error handling) for more reliable behavior on Windows
- Harden file-watcher and cache behavior, including race-condition fixes and real fsnotify-based integration tests
- Improve analysis cache correctness by avoiding caching results when the vault version changes during computation
- Add GitHub Actions CI workflow for linting, unit/integration tests across Linux/macOS/Windows, and multi-OS builds
- Enhance developer tooling with new Makefile targets (`lint`, `integration`, `test_all`) and a more capable release helper script
- Rewrite README into a clearer project landing page with badges, feature overview, command reference, and MCP usage docs

## [v0.6.0] - 2025-12-06

- Add `file` command group so move/rename operations work for both notes and attachments, updating backlinks/embeds by default.
- Improve backlink/graph performance and reliability via analysis memoization and a hardened vault cache with watcher fallback.
- Extend `properties` CLI with `set`, `delete`, and `rename` operations for bulk frontmatter editing (YAML-aware, with dry-run and worker controls).
- Replace MCP tag/property tools with unified `mutate_tags` and `mutate_properties` operations, supporting scoped inputs and dry-run summaries.
- Enhance `prompt` command to optionally emit absolute paths in `<file path="...">` blocks for better downstream tooling.
- Breaking: flip default for `note move --update-backlinks` to `true`; pass `--update-backlinks=false` to skip backlink/embedding rewrites.
