use crate::{
    bridge::{self, Failure, Request},
    pane::Panes,
    pipeline,
    presence::Presence,
    security,
    state::{migrate, valid_id, Discovery, Library, Stored},
    windows::{self, MenuEntry, Sessions},
};
use serde::Deserialize;
use serde_json::{json, Value};
use std::{
    collections::HashMap,
    path::{Path, PathBuf},
    sync::{
        atomic::{AtomicU64, Ordering},
        Arc,
    },
};
use tauri::{ipc::Channel, AppHandle, LogicalPosition, LogicalSize, Manager, Rect, State, Webview};
use tokio::sync::{Mutex, MutexGuard};

/// Serializes library file updates, global installation, and each worktree's
/// open, trust, and setup work. Different worktrees never wait on each other.
pub struct Desktop {
    pub directory: PathBuf,
    library: Mutex<()>,
    /// Counts library saves since launch. Shells apply a library snapshot only
    /// when its revision is newer than the one they hold, so a snapshot that
    /// arrives late cannot restore an entry a later save removed.
    revision: AtomicU64,
    global: Mutex<()>,
    // ponytail: one small lock per worktree ever touched, never pruned.
    worktrees: std::sync::Mutex<HashMap<String, Arc<Mutex<()>>>>,
}

impl Desktop {
    pub fn new(directory: PathBuf) -> Self {
        Self {
            directory,
            library: Mutex::default(),
            revision: AtomicU64::new(0),
            global: Mutex::default(),
            worktrees: std::sync::Mutex::default(),
        }
    }

    pub fn worktree(&self, path: &str) -> Arc<Mutex<()>> {
        self.worktrees
            .lock()
            .unwrap()
            .entry(path.into())
            .or_default()
            .clone()
    }

    /// Loads the library, migrating a version 1 folder library on first use.
    /// Hold the returned guard while changing and saving the library.
    pub async fn library(&self) -> Result<(MutexGuard<'_, ()>, Library), Failure> {
        let guard = self.library.lock().await;
        let library = match Library::load(&self.directory)? {
            Stored::Current(library) => library,
            Stored::Legacy(legacy) if legacy.folders.is_empty() => Library {
                global_executable: legacy.global_executable,
                ..Library::default()
            },
            Stored::Legacy(legacy) => {
                let mut found = vec![];
                for folder in &legacy.folders {
                    found.push(match self.discover(&folder.path, None).await {
                        Ok(value) => Some(discovery(&value)?),
                        Err(failure) if failure.code == "desktop_error" => return Err(failure),
                        Err(_) => None,
                    });
                }
                let library = migrate(legacy, &found);
                self.store(&library)?;
                library
            }
        };
        Ok((guard, library))
    }

    /// Writes the library under the next revision and returns the snapshot
    /// shells receive. Hold the library guard.
    fn store(&self, library: &Library) -> Result<Value, Failure> {
        library.save(&self.directory)?;
        self.revision.fetch_add(1, Ordering::SeqCst);
        Ok(self.snapshot(library))
    }

    /// The library as shells receive it, with the revision of the last save.
    /// Hold the library guard, so the revision is the one that wrote it.
    pub fn snapshot(&self, library: &Library) -> Value {
        let mut snapshot = json!(library);
        snapshot["revision"] = self.revision.load(Ordering::SeqCst).into();
        snapshot
    }

    pub async fn discover(&self, path: &str, primary: Option<&str>) -> Result<Value, Failure> {
        bridge::call(
            &self.directory,
            Request {
                operation: "repository",
                folder: Some(path),
                primary,
                ..Request::default()
            },
            bridge::TIMEOUT,
        )
        .await
    }
}

/// Saves the library and sends it to every window's shell, so a change made in
/// one window appears in all of them. Returns the snapshot the shells received.
pub fn save(app: &AppHandle, desktop: &Desktop, library: &Library) -> Result<Value, Failure> {
    let snapshot = desktop.store(library)?;
    announce(app, &snapshot);
    Ok(snapshot)
}

fn announce(app: &AppHandle, snapshot: &Value) {
    app.state::<Panes>()
        .broadcast(&json!({"type": "library", "library": snapshot}));
}

/// Merges library entries saved under another identity into the repositories
/// discovery resolved them to.
pub async fn adopt(app: &AppHandle, found: &[Discovery]) {
    let desktop = app.state::<Desktop>();
    let Ok((_lock, mut library)) = desktop.library().await else {
        return;
    };
    let mut changed = false;
    for found in found {
        changed |= library.adopt(found);
    }
    if changed && save(app, &desktop, &library).is_ok() {
        app.state::<Presence>().rediscover();
    }
}

pub fn discovery(value: &Value) -> Result<Discovery, Failure> {
    let found: Discovery = serde_json::from_value(value.clone())
        .map_err(|_| "Rhizome returned an invalid repository.".to_string())?;
    if !valid_id(&found.id) || !Path::new(&found.root).is_absolute() {
        return Err("Rhizome returned an invalid repository.".to_string().into());
    }
    Ok(found)
}

#[derive(Deserialize)]
#[serde(tag = "operation", rename_all = "kebab-case", deny_unknown_fields)]
pub enum Action {
    List,
    Discover {
        id: String,
    },
    Add {
        path: String,
    },
    Remove {
        id: String,
    },
    #[serde(rename_all = "camelCase")]
    SetPrimary {
        id: String,
        worktree: Option<String>,
    },
    SetExecutable {
        id: String,
        worktree: String,
        executable: Option<String>,
    },
    SetGlobalExecutable {
        executable: Option<String>,
    },
    GlobalStatus,
    GlobalInstall,
    GlobalUpdate,
    Trust {
        id: String,
        worktree: String,
    },
    Initialize {
        id: String,
        worktree: String,
    },
    /// `selection` numbers the user's choices in this window's shell.
    #[serde(rename_all = "camelCase")]
    Open {
        id: String,
        worktree: String,
        #[serde(default)]
        skip_seed: bool,
        selection: u64,
    },
    Deselect {
        selection: u64,
    },
    Restart {
        id: String,
        worktree: String,
    },
    Browse {
        to: windows::Browse,
    },
    #[serde(rename_all = "camelCase")]
    Session {
        repository: Option<String>,
        worktree: Option<String>,
        sidebar_collapsed: bool,
    },
    Layout {
        x: f64,
        y: f64,
        width: f64,
        height: f64,
        covered: bool,
    },
    Menu {
        x: f64,
        y: f64,
        items: Vec<MenuEntry>,
    },
}

fn absolute_executable(value: &Option<String>) -> Result<(), String> {
    if let Some(path) = value {
        if !Path::new(path).is_absolute() || !Path::new(path).is_file() {
            return Err("Choose an existing Rhizome executable using its absolute path.".into());
        }
    }
    Ok(())
}

fn absolute_worktree(path: &str) -> Result<(), String> {
    if !Path::new(path).is_absolute() {
        return Err("Choose a worktree using its absolute path.".into());
    }
    Ok(())
}

fn authorize(webview: &Webview) -> Result<(), Failure> {
    let location = webview.url().map_err(|e| e.to_string())?;
    if !security::shell_authorized(webview.label(), &location, cfg!(debug_assertions)) {
        return Err(Failure {
            code: "forbidden".into(),
            message: "Only the Rhizome desktop interface can manage Rhizome.".into(),
        });
    }
    Ok(())
}

/// Connects a window's shell to its progress, menu, and focus messages and
/// returns the layout and selection to restore.
#[tauri::command]
pub fn desktop_attach(
    app: AppHandle,
    webview: Webview,
    channel: Channel<Value>,
) -> Result<Value, Failure> {
    authorize(&webview)?;
    let window = webview.window();
    if let Some(presence) = app.state::<Presence>().published() {
        let _ = channel.send(presence);
    }
    app.state::<Panes>()
        .with(window.label(), |pane| pane.attach(channel));
    app.state::<Presence>().refresh();
    Ok(json!(app.state::<Sessions>().get(window.label())))
}

#[tauri::command]
pub async fn desktop_request(
    app: AppHandle,
    webview: Webview,
    state: State<'_, Desktop>,
    action: Action,
) -> Result<Value, Failure> {
    authorize(&webview)?;
    let window = webview.window();
    match action {
        Action::List => {
            let (_lock, library) = state.library().await?;
            Ok(state.snapshot(&library))
        }
        Action::Discover { id } => {
            let repository = state.library().await?.1.repository(&id)?.clone();
            state
                .discover(&repository.root, repository.primary.as_deref())
                .await
        }
        Action::Add { path } => {
            let found = discovery(&state.discover(&path, None).await?)?;
            let (_lock, mut library) = state.library().await?;
            library.add(&found);
            let snapshot = save(&app, &state, &library)?;
            app.state::<Presence>().rediscover();
            Ok(json!({"library": snapshot, "id": found.id}))
        }
        Action::Remove { id } => {
            let (_lock, mut library) = state.library().await?;
            library.repositories.retain(|r| r.id != id);
            let snapshot = save(&app, &state, &library)?;
            app.state::<Presence>().rediscover();
            Ok(snapshot)
        }
        Action::SetPrimary { id, worktree } => {
            if let Some(path) = &worktree {
                absolute_worktree(path)?;
            }
            let (_lock, mut library) = state.library().await?;
            library.repository_mut(&id)?.primary = worktree;
            let snapshot = save(&app, &state, &library)?;
            app.state::<Presence>().rediscover();
            Ok(snapshot)
        }
        Action::SetExecutable {
            id,
            worktree,
            executable,
        } => {
            absolute_worktree(&worktree)?;
            absolute_executable(&executable)?;
            let (_lock, mut library) = state.library().await?;
            let executables = &mut library.repository_mut(&id)?.executables;
            match executable {
                Some(executable) => executables.insert(worktree, executable),
                None => executables.remove(&worktree),
            };
            save(&app, &state, &library)
        }
        Action::SetGlobalExecutable { executable } => {
            absolute_executable(&executable)?;
            let (_lock, mut library) = state.library().await?;
            library.global_executable = executable;
            save(&app, &state, &library)
        }
        Action::GlobalStatus | Action::GlobalInstall | Action::GlobalUpdate => {
            let operation = match action {
                Action::GlobalStatus => "global-status",
                Action::GlobalInstall => "global-install",
                _ => "global-update",
            };
            let _installing = state.global.lock().await;
            let global = state.library().await?.1.global_executable;
            let result = bridge::call(
                &state.directory,
                Request {
                    operation,
                    global_executable: global.as_deref(),
                    ..Request::default()
                },
                bridge::TIMEOUT,
            )
            .await;
            let (_lock, mut library) = state.library().await?;
            let result = finish_global_operation(operation, result, &mut library, &state)?;
            if operation != "global-status" {
                announce(&app, &state.snapshot(&library));
            }
            Ok(result)
        }
        Action::Trust { id, worktree } => worktree_action(&state, "trust", &id, &worktree).await,
        Action::Initialize { id, worktree } => {
            worktree_action(&state, "initialize", &id, &worktree).await
        }
        Action::Open {
            id,
            worktree,
            skip_seed,
            selection,
        } => pipeline::open(app, window, id, worktree, skip_seed, selection).await,
        Action::Deselect { selection } => {
            pipeline::deselect(&app, &window, selection)?;
            Ok(Value::Null)
        }
        Action::Restart { id, worktree } => {
            absolute_worktree(&worktree)?;
            pipeline::restart(app, id, worktree, true).await?;
            Ok(Value::Null)
        }
        Action::Browse { to } => {
            windows::browse(&window, to)?;
            Ok(Value::Null)
        }
        Action::Session {
            repository,
            worktree,
            sidebar_collapsed,
        } => {
            app.state::<Sessions>()
                .update(&app, window.label(), |saved| {
                    saved.repository = repository;
                    saved.worktree = worktree;
                    saved.sidebar_collapsed = sidebar_collapsed;
                });
            Ok(Value::Null)
        }
        Action::Layout {
            x,
            y,
            width,
            height,
            covered,
        } => {
            let bounds = Rect {
                position: LogicalPosition::new(x, y).into(),
                size: LogicalSize::new(width.max(1.0), height.max(1.0)).into(),
            };
            windows::layout(&window, bounds, covered)?;
            Ok(Value::Null)
        }
        Action::Menu { x, y, items } => {
            windows::popup(&window, x, y, &items)?;
            Ok(Value::Null)
        }
    }
}

/// Trust and setup run the worktree's selected executable only for a current
/// worktree of a saved repository.
async fn worktree_action(
    state: &Desktop,
    operation: &str,
    id: &str,
    worktree: &str,
) -> Result<Value, Failure> {
    let library = state.library().await?.1;
    let repository = library.repository(id)?;
    let found = discovery(
        &state
            .discover(&repository.root, repository.primary.as_deref())
            .await?,
    )?;
    if !found.worktrees.iter().any(|w| w.path == worktree) {
        return Err("This worktree is no longer part of the repository."
            .to_string()
            .into());
    }
    let lock = state.worktree(worktree);
    let _serialized = lock.lock().await;
    bridge::call(
        &state.directory,
        Request {
            operation,
            folder: Some(worktree),
            executable: repository.executables.get(worktree).map(String::as_str),
            global_executable: library.global_executable.as_deref(),
            ..Request::default()
        },
        bridge::TIMEOUT,
    )
    .await
}

fn finish_global_operation(
    operation: &str,
    result: Result<Value, Failure>,
    library: &mut Library,
    desktop: &Desktop,
) -> Result<Value, Failure> {
    let result = result?;
    if operation != "global-status" {
        let executable = result["path"]
            .as_str()
            .ok_or("Rhizome returned no installed executable path.".to_string())?
            .to_string();
        absolute_executable(&Some(executable.clone()))?;
        library.global_executable = Some(executable);
        desktop.store(library)?;
    }
    Ok(result)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::state::Stored;

    fn saved_global(directory: &Path) -> Option<String> {
        match Library::load(directory).unwrap() {
            Stored::Current(library) => library.global_executable,
            Stored::Legacy(_) => None,
        }
    }

    #[test]
    fn completed_install_and_update_select_the_reported_executable() {
        let directory = tempfile::tempdir().unwrap();
        let executable = directory.path().join("rzm");
        std::fs::write(&executable, b"fixture").unwrap();
        let desktop = Desktop::new(directory.path().into());
        for operation in ["global-install", "global-update"] {
            let mut library = Library {
                global_executable: Some("/previous/rzm".into()),
                ..Library::default()
            };
            library.save(directory.path()).unwrap();
            let info = json!({"path": executable, "installed": true});
            let returned =
                finish_global_operation(operation, Ok(info.clone()), &mut library, &desktop)
                    .unwrap();
            assert_eq!(returned, info);
            assert_eq!(
                saved_global(directory.path()).as_deref(),
                executable.to_str()
            );
        }
    }

    #[test]
    fn failed_install_leaves_existing_selection_on_disk() {
        let directory = tempfile::tempdir().unwrap();
        let mut library = Library {
            global_executable: Some("/previous/rzm".into()),
            ..Library::default()
        };
        library.save(directory.path()).unwrap();
        let desktop = Desktop::new(directory.path().into());
        let failure = Failure {
            code: "install_error".into(),
            message: "Installation was interrupted.".into(),
        };
        assert!(
            finish_global_operation("global-install", Err(failure), &mut library, &desktop)
                .is_err()
        );
        assert_eq!(
            saved_global(directory.path()).as_deref(),
            Some("/previous/rzm")
        );
    }

    #[test]
    fn every_library_save_advances_the_revision_shells_receive() {
        let directory = tempfile::tempdir().unwrap();
        let desktop = Desktop::new(directory.path().into());
        let mut library = Library::default();
        assert_eq!(desktop.snapshot(&library)["revision"], 0);
        assert_eq!(desktop.store(&library).unwrap()["revision"], 1);
        library.global_executable = Some("/bin/rzm".into());
        let saved = desktop.store(&library).unwrap();
        assert_eq!(saved["revision"], 2);
        assert_eq!(saved["globalExecutable"], "/bin/rzm");
        assert_eq!(desktop.snapshot(&library), saved);
        let executable = directory.path().join("rzm");
        std::fs::write(&executable, b"fixture").unwrap();
        finish_global_operation(
            "global-install",
            Ok(json!({"path": executable})),
            &mut library,
            &desktop,
        )
        .unwrap();
        assert_eq!(desktop.snapshot(&library)["revision"], 3);
    }
}
