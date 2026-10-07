use crate::{
    bridge::{self, Failure, Request},
    menu::{self, MenuEntry},
    pane::Panes,
    pipeline,
    presence::Presence,
    security,
    setup::{ScopeEdits, Secret, SetupChoices},
    state::{valid_id, Discovery, Library},
    windows::{self, Sessions},
};
use serde::Deserialize;
use serde_json::{json, Value};
use std::path::Path;
use tauri::{ipc::Channel, AppHandle, LogicalPosition, LogicalSize, Manager, Rect, State, Webview};
use tauri_plugin_opener::OpenerExt;
mod state;
use state::finish_global_operation;
pub use state::Desktop;

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
    SetupReport {
        id: String,
        worktree: String,
        choices: Option<SetupChoices>,
    },
    Initialize {
        id: String,
        worktree: String,
        choices: SetupChoices,
        key: Option<Secret>,
    },
    Scope {
        id: String,
        worktree: String,
    },
    ScopeEdit {
        id: String,
        worktree: String,
        edits: ScopeEdits,
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
    Stop {
        worktree: String,
    },
    Reveal {
        worktree: String,
    },
    OpenInBrowser {
        id: String,
        worktree: String,
    },
    Reorder {
        ids: Vec<String>,
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
        Action::Trust { id, worktree } => {
            worktree_action(
                &state,
                &id,
                &worktree,
                Request {
                    operation: "trust",
                    ..Request::default()
                },
            )
            .await
        }
        Action::SetupReport {
            id,
            worktree,
            choices,
        } => {
            worktree_action(
                &state,
                &id,
                &worktree,
                Request {
                    operation: "setup-report",
                    setup: choices.as_ref(),
                    ..Request::default()
                },
            )
            .await
        }
        Action::Initialize {
            id,
            worktree,
            choices,
            key,
        } => {
            worktree_action(
                &state,
                &id,
                &worktree,
                Request {
                    operation: "initialize",
                    setup: Some(&choices),
                    key: key.as_ref(),
                    ..Request::default()
                },
            )
            .await
        }
        Action::Scope { id, worktree } => {
            worktree_action(
                &state,
                &id,
                &worktree,
                Request {
                    operation: "scope",
                    ..Request::default()
                },
            )
            .await
        }
        Action::ScopeEdit {
            id,
            worktree,
            edits,
        } => {
            worktree_action(
                &state,
                &id,
                &worktree,
                Request {
                    operation: "scope-edit",
                    edits: Some(&edits),
                    ..Request::default()
                },
            )
            .await
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
        Action::Stop { worktree } => {
            absolute_worktree(&worktree)?;
            pipeline::stop(app, worktree).await?;
            Ok(Value::Null)
        }
        Action::Reveal { worktree } => {
            absolute_worktree(&worktree)?;
            app.opener()
                .reveal_item_in_dir(&worktree)
                .map_err(|e| e.to_string())?;
            Ok(Value::Null)
        }
        Action::OpenInBrowser { id, worktree } => {
            absolute_worktree(&worktree)?;
            // The page this window shows, else the runtime's home page, started
            // first when it is not running.
            let url = match windows::worktree_page_url(&window, &worktree) {
                Some(url) => url,
                None => {
                    pipeline::open_runtime(&app, &id, &worktree, false, false)
                        .await?
                        .ok_or("Rhizome did not start for this worktree.".to_string())?
                        .url
                }
            };
            app.state::<Presence>().refresh();
            app.opener()
                .open_url(url.as_str(), None::<&str>)
                .map_err(|e| e.to_string())?;
            Ok(Value::Null)
        }
        Action::Reorder { ids } => {
            let (_lock, mut library) = state.library().await?;
            library.reorder(&ids);
            save(&app, &state, &library)
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
            menu::popup(&window, x, y, &items)?;
            Ok(Value::Null)
        }
    }
}

/// Trust, setup, and scope run the worktree's selected executable only for a current
/// worktree of a saved repository.
async fn worktree_action(
    state: &Desktop,
    id: &str,
    worktree: &str,
    request: Request<'_>,
) -> Result<Value, Failure> {
    let lock = state.worktree(worktree);
    let _serialized = lock.lock().await;
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
    bridge::call(
        &state.directory,
        Request {
            folder: Some(worktree),
            executable: repository.executables.get(worktree).map(String::as_str),
            global_executable: library.global_executable.as_deref(),
            ..request
        },
        bridge::TIMEOUT,
    )
    .await
}

#[cfg(test)]
mod tests;
