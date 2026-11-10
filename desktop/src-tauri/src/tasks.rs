//! Native page requests open task worktrees, start tasks in Terminal, or
//! open the current repository settings. Repository identity comes from the
//! window; task paths and item numbers are validated before execution.
use crate::{bridge, commands::Desktop, page::Ask, pane::Panes, terminal};
use serde_json::json;
use std::path::{Path, PathBuf};
use tauri::{AppHandle, Manager};
use tokio::process::Command;

/// Carries out a page's request for `window`, showing a failure in its shell.
pub fn handle(app: &AppHandle, window: &str, ask: Ask) {
    let Some((repository, worktree)) = app.state::<Panes>().with(window, |pane| pane.target())
    else {
        return;
    };
    if ask == Ask::OpenRepositorySettings {
        app.state::<Panes>().deliver(
            window,
            json!({"type": "repository-settings", "repository": repository, "worktree": worktree}),
        );
        return;
    }
    let (app, window) = (app.clone(), window.to_string());
    tauri::async_runtime::spawn(async move {
        if let Err(message) = act(&app, &repository, &worktree, ask).await {
            app.state::<Panes>().deliver(
                &window,
                json!({"type": "alert", "code": "task_action_failed", "message": message}),
            );
        }
    });
}

async fn act(app: &AppHandle, repository: &str, worktree: &str, ask: Ask) -> Result<(), String> {
    if worktree.is_empty() {
        return Err("This window has no repository open.".into());
    }
    let (common, main) = main_checkout(Path::new(worktree)).await?;
    match ask {
        Ask::OpenRepositorySettings => Ok(()),
        Ask::OpenTaskWorktree { path } => {
            let path = task_worktree(&main, &path)?;
            git_worktree_of(&path, &common).await?;
            terminal::open_in_new_window(app, &path).await;
            Ok(())
        }
        Ask::StartTask { item } => {
            let rzm = executable(app, repository, &main).await?;
            start_task(&main, &rzm, &item).await
        }
    }
}

async fn start_task(main: &Path, rzm: &Path, item: &str) -> Result<(), String> {
    let mut command = start_command(main, rzm, item)?;
    #[cfg(windows)]
    {
        // PowerShell owns the interactive console; waiting would hold the page action open.
        command
            .spawn()
            .map_err(|e| format!("Cannot open PowerShell: {e}"))?;
    }
    #[cfg(not(windows))]
    {
        let output = command
            .output()
            .await
            .map_err(|e| {
                if cfg!(target_os = "linux") {
                    format!("Cannot open GNOME Terminal. Install gnome-terminal or start the task from a terminal: {e}")
                } else {
                    format!("Cannot open Terminal: {e}")
                }
            })?;
        if !output.status.success() {
            return Err(format!(
                "Cannot open a task terminal: {}",
                String::from_utf8_lossy(&output.stderr).trim()
            ));
        }
    }
    Ok(())
}

fn start_command(main: &Path, rzm: &Path, item: &str) -> Result<Command, String> {
    item_number(item)?;
    #[cfg(not(target_os = "macos"))]
    let (main_text, rzm_text) = (command_path(main)?, command_path(rzm)?);
    #[cfg(target_os = "macos")]
    {
        let mut command = crate::process::command("osascript");
        command.args(["-e", &start_script(main, rzm, item)?]);
        Ok(command)
    }
    #[cfg(target_os = "linux")]
    {
        let mut command = crate::process::command("gnome-terminal");
        command.args([
            "--working-directory",
            main_text,
            "--",
            rzm_text,
            "tasks",
            "start",
            item,
        ]);
        Ok(command)
    }
    #[cfg(windows)]
    {
        let mut command = Command::new("powershell.exe");
        command
            .args([
                "-NoLogo",
                "-NoProfile",
                "-NoExit",
                "-Command",
                POWERSHELL_START,
            ])
            .env("RHIZOME_TASK_DIRECTORY", main_text)
            .env("RHIZOME_TASK_EXECUTABLE", rzm_text)
            .env("RHIZOME_TASK_ITEM", item)
            .creation_flags(0x00000010); // CREATE_NEW_CONSOLE
        Ok(command)
    }
}

/// The repository's canonical common Git directory and its main checkout, the
/// parent of a `.git` common directory, as `rzm tasks` finds them.
async fn main_checkout(worktree: &Path) -> Result<(PathBuf, PathBuf), String> {
    let (common, _) = rev_parse(worktree).await?;
    match (common.file_name(), common.parent()) {
        (Some(name), Some(main)) if name == ".git" => Ok((common.clone(), main.to_path_buf())),
        _ => Err(
            "This repository has no main checkout, so Rhizome Desktop cannot run its tasks.".into(),
        ),
    }
}

/// The canonical common directory and top level of the Git worktree at `dir`.
async fn rev_parse(dir: &Path) -> Result<(PathBuf, PathBuf), String> {
    let output = crate::process::command("git")
        .arg("-C")
        .arg(dir)
        .args([
            "rev-parse",
            "--path-format=absolute",
            "--git-common-dir",
            "--show-toplevel",
        ])
        .env_remove("GIT_DIR")
        .env_remove("GIT_WORK_TREE")
        .output()
        .await
        .map_err(|e| format!("Cannot run Git: {e}"))?;
    let text = String::from_utf8_lossy(&output.stdout);
    let mut lines = text.lines().map(std::fs::canonicalize);
    match (output.status.success(), lines.next(), lines.next()) {
        (true, Some(Ok(common)), Some(Ok(top))) => Ok((common, top)),
        _ => Err(format!("{} is not a Git worktree.", dir.display())),
    }
}

/// A path the page named as an item's worktree, canonical, when it is a
/// folder directly inside the main checkout's `.rhizome/task-state/worktrees`.
/// Canonicalizing first means `..` and symlinks cannot lead outside.
pub fn task_worktree(main: &Path, path: &str) -> Result<PathBuf, String> {
    let refused = || format!("{path} is not a task worktree of this repository.");
    if !Path::new(path).is_absolute() {
        return Err(refused());
    }
    let root =
        std::fs::canonicalize(main.join(".rhizome/task-state/worktrees")).map_err(|_| refused())?;
    // A symlinked worktrees folder must not move the allowed root elsewhere.
    let expected = std::fs::canonicalize(main)
        .map_err(|_| refused())?
        .join(".rhizome/task-state/worktrees");
    if root != expected {
        return Err(refused());
    }
    let found = std::fs::canonicalize(path).map_err(|_| refused())?;
    if found.parent() != Some(root.as_path()) || !found.is_dir() {
        return Err(refused());
    }
    Ok(found)
}

/// Requires `path` to be the root of a Git worktree sharing `common`.
async fn git_worktree_of(path: &Path, common: &Path) -> Result<(), String> {
    match rev_parse(path).await {
        Ok((theirs, top)) if theirs == common && top == path => Ok(()),
        _ => Err(format!(
            "{} is not a worktree of this repository.",
            path.display()
        )),
    }
}

/// The rzm the app would run for the main checkout: the user's choice for it,
/// else the global choice, else the bundled companion. Each defers to the
/// repository's pin through the CLI's own repository delegation.
async fn executable(app: &AppHandle, repository: &str, main: &Path) -> Result<PathBuf, String> {
    let library = app
        .state::<Desktop>()
        .library()
        .await
        .map_err(|failure| failure.message)?
        .1;
    let chosen = library
        .repository(repository)
        .ok()
        .and_then(|repository| selected_executable(repository, main))
        .map(str::to_owned);
    match chosen.or(library.global_executable) {
        Some(path) => Ok(PathBuf::from(path)),
        None => bridge::sidecar(),
    }
}

fn selected_executable<'a>(
    repository: &'a crate::state::Repository,
    main: &Path,
) -> Option<&'a str> {
    repository
        .executables
        .iter()
        .find_map(|(path, executable)| {
            std::fs::canonicalize(path)
                .ok()
                .filter(|path| path == main)
                .map(|_| executable.as_str())
        })
}

/// The AppleScript that opens Terminal in `main` running `rzm tasks start
/// <item>`. The item must be a number; the paths are quoted for the shell,
/// then the command is quoted as an AppleScript string.
#[cfg(any(target_os = "macos", all(test, unix)))]
pub fn start_script(main: &Path, rzm: &Path, item: &str) -> Result<String, String> {
    item_number(item)?;
    let command = format!(
        "cd {} && {} tasks start {item}",
        shell_quote(command_path(main)?),
        shell_quote(command_path(rzm)?)
    );
    Ok(format!(
        "tell application \"Terminal\"\n\tdo script {}\n\tactivate\nend tell",
        applescript_string(&command)
    ))
}

fn item_number(item: &str) -> Result<(), String> {
    if item.is_empty() || item.len() > 9 || !item.bytes().all(|b| b.is_ascii_digit()) {
        return Err(format!("{item:?} is not a task item number."));
    }
    Ok(())
}

/// An absolute path the Terminal command may name: no control characters,
/// which Terminal would type as keystrokes.
fn command_path(path: &Path) -> Result<&str, String> {
    path.to_str()
        .filter(|p| path.is_absolute() && !p.chars().any(char::is_control))
        .ok_or_else(|| format!("Cannot start a task from {}.", path.display()))
}

#[cfg(any(target_os = "macos", all(test, unix)))]
fn shell_quote(text: &str) -> String {
    format!("'{}'", text.replace('\'', r"'\''"))
}

#[cfg(any(target_os = "macos", all(test, unix)))]
fn applescript_string(text: &str) -> String {
    format!("\"{}\"", text.replace('\\', r"\\").replace('"', "\\\""))
}

#[cfg(windows)]
const POWERSHELL_START: &str = "Set-Location -LiteralPath $env:RHIZOME_TASK_DIRECTORY -ErrorAction Stop; & $env:RHIZOME_TASK_EXECUTABLE tasks start $env:RHIZOME_TASK_ITEM";

#[cfg(test)]
mod command_tests;
#[cfg(all(test, unix))]
mod tests;
