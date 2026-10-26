# pkg/app/tasks

Scheduled tasks ([[scheduled-tasks|SPEC-0127]]); user guide: `docs/reference/guides/scheduled-tasks-guide.md`.

- **Entry points:** `OpenRepo` (repository identity by Git common directory), `New`/`Service` with `RunDue`, `Run`, `RunDraft`, `PlanStart`/`MarkStarted`/`UndoStart`, `Status`, `Inbox`, and the item actions. `cmd/tasks.go` is the thin Cobra front; `pkg/app/cli/serve` starts `rzm tasks run-due` on an interval.
- **Definitions come from the primary ref only.** `LoadDefinitions` reads `.rhizome/tasks/*.yml` through `git ls-tree`/`git show` at one resolved commit; the working tree never matters. A file that fails to parse is a `Problem`, not an error. Parsing lives in `pkg/vault/taskdef`, which `rzm validate tasks` also uses on the working tree.
- **Two locks.** `run.lock` serializes every check and background run for the repository, held from resolving the ref through publishing results; passes that find it held skip. `state.lock` guards short read-modify-writes of `state.json`, so inbox commands never wait for a run.
- **Clock.** `person` tasks use the local history; `team` tasks also use the latest commit on the primary ref whose message has the line `Rhizome-Task: <id>` (`git log --grep`, whole line, so squash bodies count).
- **Processes.** Checks run as session leaders under a `pkg/proctree` tracker, which records descendants by PID and start time; when a check ends or times out, everything it started is killed, and `WaitDelay` keeps a descendant holding the output pipes from holding the run lock. Prompt runs go through `pkg/harness`, whose transport does the same on `Close`.
- **Background runs never push.** `work` makes `rzm/<task>/<date>` (suffixed when taken) and a worktree under the state folder, runs the prompt, then `commitWork` commits everything with the agent's message and the tag, adding an empty tagged commit when the agent committed everything itself. No changes: the worktree and branch are removed.
- **Retries rerun the check.** `Run` runs the task's check again first, so a failed check never reaches agent work and the prompt gets fresh context. When the check says nothing is needed, an item holding a failed attempt's worktree stays needs attention so the work can still be reviewed or discarded.
- **Failures.** A failed check reuses the task's earlier failure notice when it holds no worktree; failed prompt runs record the tokens they used.
- **Trust.** Nothing runs unless `Options.Trusted` accepts the main checkout; `cmd` passes `repositorytrust`.
