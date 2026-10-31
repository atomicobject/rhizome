//! The Tasks page's two native actions: open an item's worktree in a new
//! window, and start an item in Terminal. The page names only a path or an
//! item number. Both are checked against the window's own repository, and
//! the command Terminal runs is built here.
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
        Ask::OpenTaskWorktree { path } => {
            let path = task_worktree(&main, &path)?;
            git_worktree_of(&path, &common).await?;
            terminal::open_in_new_window(app, &path).await;
            Ok(())
        }
        Ask::StartTask { item } => {
            let rzm = executable(app, repository, &main).await?;
            let script = start_script(&main, &rzm, &item)?;
            let output = Command::new("osascript")
                .arg("-e")
                .arg(script)
                .output()
                .await
                .map_err(|e| format!("Cannot open Terminal: {e}"))?;
            if !output.status.success() {
                return Err(format!(
                    "Cannot open Terminal: {}",
                    String::from_utf8_lossy(&output.stderr).trim()
                ));
            }
            Ok(())
        }
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
    let output = Command::new("git")
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
    let chosen = main.to_str().and_then(|main| {
        library
            .repository(repository)
            .ok()
            .and_then(|r| r.executables.get(main).cloned())
    });
    match chosen.or(library.global_executable) {
        Some(path) => Ok(PathBuf::from(path)),
        None => bridge::sidecar(),
    }
}

/// The AppleScript that opens Terminal in `main` running `rzm tasks start
/// <item>`. The item must be a number; the paths are quoted for the shell,
/// then the command is quoted as an AppleScript string.
pub fn start_script(main: &Path, rzm: &Path, item: &str) -> Result<String, String> {
    if item.is_empty() || item.len() > 9 || !item.bytes().all(|b| b.is_ascii_digit()) {
        return Err(format!("{item:?} is not a task item number."));
    }
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

/// An absolute path the Terminal command may name: no control characters,
/// which Terminal would type as keystrokes.
fn command_path(path: &Path) -> Result<&str, String> {
    path.to_str()
        .filter(|p| path.is_absolute() && !p.chars().any(char::is_control))
        .ok_or_else(|| format!("Cannot start a task from {}.", path.display()))
}

fn shell_quote(text: &str) -> String {
    format!("'{}'", text.replace('\'', r"'\''"))
}

fn applescript_string(text: &str) -> String {
    format!("\"{}\"", text.replace('\\', r"\\").replace('"', "\\\""))
}

#[cfg(all(test, unix))]
mod tests {
    use super::*;
    use std::{fs, os::unix::fs::symlink};

    /// A main checkout with a task worktree folder `a`, and a folder outside.
    fn checkout() -> (tempfile::TempDir, PathBuf, PathBuf) {
        let dir = tempfile::tempdir().unwrap();
        let main = dir.path().join("main checkout");
        let worktrees = main.join(".rhizome/task-state/worktrees");
        fs::create_dir_all(worktrees.join("a/sub")).unwrap();
        let outside = dir.path().join("outside");
        fs::create_dir_all(&outside).unwrap();
        (dir, main, worktrees)
    }

    #[test]
    fn only_a_folder_directly_inside_the_task_worktrees_opens() {
        let (dir, main, worktrees) = checkout();
        let inside = worktrees.join("a");
        assert_eq!(
            task_worktree(&main, inside.to_str().unwrap()).unwrap(),
            fs::canonicalize(&inside).unwrap()
        );
        let outside = dir.path().join("outside");
        for path in [
            outside.clone(),
            worktrees.clone(),
            worktrees.join("a/sub"),
            worktrees.join("missing"),
            worktrees.join("../../../../outside"),
            worktrees.join("a/../../worktrees/../../task-state"),
            main.clone(),
        ] {
            assert!(
                task_worktree(&main, path.to_str().unwrap()).is_err(),
                "{path:?}"
            );
        }
        assert!(task_worktree(&main, ".rhizome/task-state/worktrees/a").is_err());
        assert!(
            task_worktree(&main, worktrees.join("a/../a").to_str().unwrap()).is_ok(),
            "a path that resolves inside is the folder itself"
        );
    }

    #[test]
    fn a_symlink_out_of_the_task_worktrees_is_refused() {
        let (dir, main, worktrees) = checkout();
        symlink(dir.path().join("outside"), worktrees.join("escape")).unwrap();
        assert!(task_worktree(&main, worktrees.join("escape").to_str().unwrap()).is_err());
        // A symlinked state folder still resolves; what matters is where it leads.
        let linked = dir.path().join("linked");
        symlink(&main, &linked).unwrap();
        assert!(task_worktree(&linked, worktrees.join("a").to_str().unwrap()).is_ok());
    }

    #[test]
    fn only_a_worktree_of_the_same_repository_passes_the_git_check() {
        tauri::async_runtime::block_on(same_repository_check());
    }

    async fn same_repository_check() {
        let dir = tempfile::tempdir().unwrap();
        let git = |cwd: &Path, args: &[&str]| {
            let status = std::process::Command::new("git")
                .current_dir(cwd)
                .args(args)
                .status()
                .unwrap();
            assert!(status.success(), "git {args:?}");
        };
        let main = dir.path().join("repo");
        fs::create_dir_all(&main).unwrap();
        git(&main, &["init", "-q"]);
        git(
            &main,
            &[
                "-c",
                "user.name=t",
                "-c",
                "user.email=t@example.com",
                "commit",
                "-q",
                "--allow-empty",
                "-m",
                "init",
            ],
        );
        let worktree = main.join(".rhizome/task-state/worktrees/w");
        git(
            &main,
            &["worktree", "add", "-q", worktree.to_str().unwrap()],
        );
        let other = dir.path().join("other");
        fs::create_dir_all(&other).unwrap();
        git(&other, &["init", "-q"]);
        let stray = main.join(".rhizome/task-state/worktrees/plain");
        fs::create_dir_all(&stray).unwrap();

        let (common, found) = main_checkout(&worktree).await.unwrap();
        assert_eq!(found, fs::canonicalize(&main).unwrap());
        let worktree = task_worktree(&found, worktree.to_str().unwrap()).unwrap();
        assert!(git_worktree_of(&worktree, &common).await.is_ok());
        let (other_common, _) = main_checkout(&other).await.unwrap();
        assert!(git_worktree_of(&worktree, &other_common).await.is_err());
        // A plain folder inside the main checkout is that checkout's subfolder,
        // not a worktree root.
        let stray = task_worktree(&found, stray.to_str().unwrap()).unwrap();
        assert!(git_worktree_of(&stray, &common).await.is_err());
    }

    #[test]
    fn the_start_command_quotes_paths_for_the_shell_and_applescript() {
        let script = start_script(
            Path::new(r#"/Users/a b/it's "here"\x"#),
            Path::new("/Applications/Rhizome Dev.app/Contents/MacOS/rzm"),
            "12",
        )
        .unwrap();
        assert_eq!(
            script,
            "tell application \"Terminal\"\n\tdo script \"cd '/Users/a b/it'\\\\''s \\\"here\\\"\\\\x' && '/Applications/Rhizome Dev.app/Contents/MacOS/rzm' tasks start 12\"\n\tactivate\nend tell"
        );
        let main = Path::new("/work/repo");
        let rzm = Path::new("/bin/rzm");
        for item in ["", "1;rm -rf ~", "1 2", "-1", "1\n", "1234567890", "１"] {
            assert!(start_script(main, rzm, item).is_err(), "{item:?}");
        }
        assert!(start_script(Path::new("/work/re\npo"), rzm, "1").is_err());
        assert!(start_script(main, Path::new("rzm"), "1").is_err());
    }
}
