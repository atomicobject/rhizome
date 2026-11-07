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
fn a_symlinked_worktrees_folder_cannot_move_the_root() {
    let (dir, main, worktrees) = checkout();
    let elsewhere = dir.path().join("elsewhere");
    fs::create_dir_all(elsewhere.join("b")).unwrap();
    fs::remove_dir_all(&worktrees).unwrap();
    symlink(&elsewhere, &worktrees).unwrap();
    assert!(task_worktree(&main, worktrees.join("b").to_str().unwrap()).is_err());
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
