//! Opening a folder from the terminal. `rzm desktop` launches the app with
//! `--open <path>`: a running app receives the arguments through the
//! single-instance plugin, and a new app reads its own after restoring the
//! session. Only process arguments carry the request, so web content cannot.
use crate::{
    bridge::Failure,
    commands::{self, discovery, Desktop},
    pane::Panes,
    presence::Presence,
    state::Discovery,
    windows::{self, Sessions},
};
use serde_json::{json, Value};
use std::{
    path::{Path, PathBuf},
    sync::Mutex,
    time::Duration,
};
use tauri::{AppHandle, Manager, Window};

/// The folder named by the last `--open <path>`, if any. Other arguments are
/// ignored; a relative path is rejected.
pub fn requested(args: &[String]) -> Option<Result<PathBuf, String>> {
    let mut found = None;
    let mut args = args.iter();
    while let Some(arg) = args.next() {
        if arg == "--open" {
            found = Some(match args.next() {
                Some(path) if Path::new(path).is_absolute() => Ok(PathBuf::from(path)),
                Some(path) => Err(format!("--open needs an absolute path, not {path:?}.")),
                None => Err("--open needs a path.".into()),
            });
        }
    }
    found
}

/// Starts the open `args` request. Returns false when they request none, or
/// an invalid one, which is logged.
pub fn handle(app: &AppHandle, args: &[String]) -> bool {
    match requested(args) {
        Some(Ok(path)) => {
            let app = app.clone();
            tauri::async_runtime::spawn(async move { open(&app, &path).await });
            true
        }
        Some(Err(problem)) => {
            eprintln!("Rhizome Desktop ignored a terminal open: {problem}");
            false
        }
        None => false,
    }
}

/// Window labels in the order they last gained focus, most recent last.
#[derive(Default)]
pub struct Focus(Mutex<Vec<String>>);

impl Focus {
    pub fn focused(&self, window: &str) {
        let mut order = self.0.lock().unwrap();
        order.retain(|w| w != window);
        order.push(window.into());
    }

    pub fn closed(&self, window: &str) {
        self.0.lock().unwrap().retain(|w| w != window);
    }

    /// The most recently focused of the `open` windows, else the first one
    /// opened, as after a launch before any window gained focus.
    pub fn target(&self, open: &[String]) -> Option<String> {
        let order = self.0.lock().unwrap();
        order
            .iter()
            .rev()
            .find(|w| open.contains(w))
            .or_else(|| open.iter().min_by_key(|w| windows::number(w)))
            .cloned()
    }
}

/// The candidate and worktree a terminal path opens. A configured worktree
/// containing the path wins, the deepest first; then a configured worktree
/// whose Git worktree root contains it, so a Git root or a folder beside a
/// subfolder vault opens that vault; then any worktree containing it, whose
/// open asks for setup or reports why it is unavailable.
pub fn choose(candidates: &[Discovery], path: &Path) -> Option<(usize, String)> {
    let worktrees = candidates
        .iter()
        .enumerate()
        .flat_map(|(i, found)| found.worktrees.iter().map(move |w| (i, found, w)));
    let (mut inside, mut beside, mut any) = (vec![], vec![], vec![]);
    for (i, found, w) in worktrees {
        let usable = w.configured && w.error.is_none();
        if path.starts_with(&w.path) {
            if usable {
                inside.push((i, Path::new(&w.path), &w.path));
            }
            any.push((i, Path::new(&w.path), &w.path));
        } else if let Some(root) = git_root(&w.path, &found.subpath) {
            if usable && path.starts_with(root) {
                beside.push((i, root, &w.path));
            }
        }
    }
    [inside, beside, any].into_iter().find_map(|matches| {
        // The deepest match; the earliest candidate among equals.
        let (i, _, worktree) = matches
            .into_iter()
            .rev()
            .max_by_key(|(_, at, _)| at.components().count())?;
        Some((i, worktree.clone()))
    })
}

/// The Git worktree root of a subfolder vault's worktree.
fn git_root<'a>(worktree: &'a str, subpath: &str) -> Option<&'a Path> {
    let worktree = Path::new(worktree);
    if subpath.is_empty() || !worktree.ends_with(subpath) {
        return None;
    }
    worktree
        .ancestors()
        .nth(Path::new(subpath).components().count())
}

/// Selects the worktree containing `path` in the most recently focused window,
/// adding its repository to the library when needed, and brings it forward.
async fn open(app: &AppHandle, path: &Path) {
    let Some(window) = target_window(app) else {
        eprintln!(
            "Rhizome Desktop could not open a window for {}",
            path.display()
        );
        return;
    };
    bring_forward(app, &window);
    let message = match resolve(app, path).await {
        Ok((library, repository, worktree)) => json!({
            "type": "select",
            "library": library,
            "repository": repository,
            "worktree": worktree,
        }),
        Err(failure) => {
            eprintln!(
                "Rhizome Desktop could not open {}: {}",
                path.display(),
                failure.message
            );
            json!({
                "type": "alert",
                "code": failure.code,
                "message": format!("Cannot open {}: {}", path.display(), failure.message),
            })
        }
    };
    app.state::<Panes>().deliver(window.label(), message);
}

fn target_window(app: &AppHandle) -> Option<Window> {
    let windows = app.windows();
    let open: Vec<String> = windows.keys().cloned().collect();
    if let Some(label) = app.state::<Focus>().target(&open) {
        return windows.get(&label).cloned();
    }
    // A new window keeps the last layout but not its selection, which the
    // terminal's choice replaces.
    let mut session = app.state::<Sessions>().last_closed();
    session.repository = None;
    session.worktree = None;
    windows::create(app, session).ok()
}

fn bring_forward(app: &AppHandle, window: &Window) {
    #[cfg(target_os = "macos")]
    let _ = app.show();
    let minimized = window.is_minimized().unwrap_or(false);
    let _ = window.show();
    let _ = window.unminimize();
    // Focusing also activates the app; it has no effect until a minimized
    // window finishes restoring.
    let _ = window.set_focus();
    if minimized {
        let window = window.clone();
        tauri::async_runtime::spawn(async move {
            tokio::time::sleep(Duration::from_millis(500)).await;
            let _ = window.set_focus();
        });
    }
    let _ = app;
}

/// Finds the repository and worktree for `path`, adding the repository to the
/// library when it is new. A path whose own discovery has no configured
/// worktree for it, such as the Git root of a subfolder vault, is also matched
/// against the library's repositories.
async fn resolve(app: &AppHandle, path: &Path) -> Result<(Value, String, String), Failure> {
    let path = std::fs::canonicalize(path)
        .ok()
        .filter(|p| p.is_dir())
        .ok_or_else(|| "That folder does not exist.".to_string())?;
    let folder = path
        .to_str()
        .ok_or_else(|| "The folder path is not valid UTF-8.".to_string())?;
    let desktop = app.state::<Desktop>();
    let own = discovery(&desktop.discover(folder, None).await?)?;
    let configured = choose(std::slice::from_ref(&own), &path).is_some_and(|(_, chosen)| {
        own.worktrees
            .iter()
            .any(|w| w.path == chosen && w.configured)
    });
    let known = own.id.clone();
    let mut candidates = vec![own];
    if !configured {
        let saved = desktop.library().await?.1;
        // ponytail: one discovery per saved repository, only on this rare path;
        // use the batched status operation if libraries grow large.
        for repository in saved.repositories.iter().filter(|r| r.id != known) {
            if let Ok(found) = desktop
                .discover(&repository.root, repository.primary.as_deref())
                .await
                .and_then(|value| discovery(&value))
            {
                candidates.push(found);
            }
        }
    }
    let (index, worktree) = choose(&candidates, &path)
        .ok_or_else(|| "No worktree of its repository contains it.".to_string())?;
    let found = &candidates[index];
    let (_lock, mut library) = desktop.library().await?;
    let snapshot = if library.add(found) {
        app.state::<Presence>().rediscover();
        commands::save(app, &desktop, &library)?
    } else {
        desktop.snapshot(&library)
    };
    Ok((snapshot, found.id.clone(), worktree))
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::state::DiscoveredWorktree;

    fn args(list: &[&str]) -> Vec<String> {
        list.iter().map(|s| s.to_string()).collect()
    }

    #[test]
    fn open_requests_come_from_forwarded_and_startup_arguments() {
        let forwarded = args(&[
            "/Applications/Rhizome.app/Contents/MacOS/rhizome-desktop",
            "--open",
            "/work/project-feature",
        ]);
        assert_eq!(
            requested(&forwarded),
            Some(Ok(PathBuf::from("/work/project-feature")))
        );
        let startup = args(&[
            "rhizome-desktop",
            "-psn_0_1",
            "--open",
            "/work/a",
            "--open",
            "/work/b",
        ]);
        assert_eq!(requested(&startup), Some(Ok(PathBuf::from("/work/b"))));
        assert_eq!(requested(&args(&["rhizome-desktop"])), None);
        assert_eq!(requested(&args(&["rhizome-desktop", "--other", "x"])), None);
    }

    #[test]
    fn relative_paths_are_rejected() {
        assert!(requested(&args(&["app", "--open", "project"]))
            .unwrap()
            .is_err());
        assert!(requested(&args(&["app", "--open", "./project"]))
            .unwrap()
            .is_err());
        assert!(requested(&args(&["app", "--open"])).unwrap().is_err());
    }

    #[test]
    fn the_most_recently_focused_open_window_is_the_target() {
        let focus = Focus::default();
        let open = args(&["window-1", "window-2", "window-3"]);
        assert_eq!(focus.target(&[]), None, "no window is open");
        assert_eq!(
            focus.target(&open).as_deref(),
            Some("window-1"),
            "focus unknown"
        );
        focus.focused("window-3");
        focus.focused("window-2");
        assert_eq!(focus.target(&open).as_deref(), Some("window-2"));
        focus.focused("window-3");
        assert_eq!(focus.target(&open).as_deref(), Some("window-3"));
        focus.closed("window-3");
        assert_eq!(
            focus.target(&args(&["window-1", "window-2"])).as_deref(),
            Some("window-2")
        );
        let restored = args(&["window-10", "window-9"]);
        assert_eq!(
            Focus::default().target(&restored).as_deref(),
            Some("window-9")
        );
    }

    fn found(id: char, subpath: &str, worktrees: &[(&str, bool)]) -> Discovery {
        Discovery {
            id: id.to_string().repeat(64),
            root: worktrees[0].0.into(),
            name: "project".into(),
            primary: worktrees[0].0.into(),
            subpath: subpath.into(),
            worktrees: worktrees
                .iter()
                .map(|(path, configured)| DiscoveredWorktree {
                    path: (*path).into(),
                    configured: *configured,
                    trust_required: false,
                    has_database: false,
                    error: None,
                })
                .collect(),
        }
    }

    fn chosen(candidates: &[Discovery], path: &str) -> Option<(usize, String)> {
        choose(candidates, Path::new(path))
    }

    #[test]
    fn the_worktree_containing_the_path_is_chosen() {
        let repo = found(
            'a',
            "",
            &[
                ("/work/project", true),
                ("/work/project/.worktrees/b", true),
            ],
        );
        let candidates = [repo];
        assert_eq!(
            chosen(&candidates, "/work/project"),
            Some((0, "/work/project".into()))
        );
        assert_eq!(
            chosen(&candidates, "/work/project/.worktrees/b/notes/deep"),
            Some((0, "/work/project/.worktrees/b".into())),
            "the deepest containing worktree wins"
        );
        assert_eq!(
            chosen(&candidates, "/work/project-other"),
            None,
            "a sibling with a shared prefix is not inside"
        );
    }

    #[test]
    fn a_git_root_opens_its_subfolder_vault() {
        // Discovery from the Git root sees an unconfigured whole-repository
        // worktree; the library holds the docs vault of the same repository.
        let whole = found(
            'w',
            "",
            &[("/work/project", false), ("/work/feature", false)],
        );
        let docs = found(
            'd',
            "docs",
            &[("/work/project/docs", true), ("/work/feature/docs", true)],
        );
        let candidates = [whole, docs];
        assert_eq!(
            chosen(&candidates, "/work/feature"),
            Some((1, "/work/feature/docs".into()))
        );
        assert_eq!(
            chosen(&candidates, "/work/feature/src"),
            Some((1, "/work/feature/docs".into())),
            "a folder outside the vault opens the vault of its Git worktree"
        );
        assert_eq!(
            chosen(&candidates, "/work/feature/docs/notes"),
            Some((1, "/work/feature/docs".into()))
        );
        assert_eq!(
            chosen(&candidates[..1], "/work/feature"),
            Some((0, "/work/feature".into())),
            "without a known vault the folder itself opens and asks for setup"
        );
        assert_eq!(chosen(&candidates, "/elsewhere"), None);
    }
}
