use crate::{
    bridge::{self, Failure, Request},
    commands::{self, discovery, Desktop},
    pane::{Load, Panes},
    presence::{self, Presence},
    security,
    state::Library,
    windows,
};
use serde_json::{json, Value};
use std::{
    path::Path,
    time::{Duration, SystemTime, UNIX_EPOCH},
};
use tauri::{AppHandle, Manager, Window};
use url::Url;

const LOAD_TIMEOUT: Duration = Duration::from_secs(60);

fn now() -> Duration {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
}

fn started() -> u64 {
    now().as_millis() as u64
}

struct Progress {
    app: AppHandle,
    window: String,
    generation: u64,
}

impl Progress {
    fn current(&self) -> bool {
        self.app
            .state::<Panes>()
            .with(&self.window, |pane| pane.current(self.generation))
            .unwrap_or(false)
    }

    fn step(&self, step: &str, detail: Value) {
        self.app.state::<Panes>().with(&self.window, |pane| {
            pane.report(self.generation, step, detail)
        });
    }

    fn fail(&self, failure: &Failure) {
        self.step(
            "error",
            json!({"code": failure.code, "message": failure.message}),
        );
    }

    /// Shows the generation's first step and hides the content until it loads.
    fn begin(&self) {
        if let Some(window) = self.app.get_window(&self.window) {
            let _ = windows::sync_visibility(&window);
        }
        presence::publish(&self.app);
        self.step("checking", json!({}));
    }

    /// Ends the open steps and refreshes presence right away. An open that
    /// displays nothing clears the content view.
    fn settle(&self, keep: bool) {
        let cleared = self
            .app
            .state::<Panes>()
            .with(&self.window, |pane| pane.settle(self.generation, keep))
            .unwrap_or(false);
        if cleared {
            if let Some(window) = self.app.get_window(&self.window) {
                windows::blank(&window);
                let _ = windows::sync_visibility(&window);
            }
        }
        presence::publish(&self.app);
        self.app.state::<Presence>().refresh();
    }
}

/// Starts opening a worktree in one window and returns its generation. The
/// shell numbers each selection when the user makes it, so a selection that
/// arrives after a newer one is discarded rather than replacing it.
pub async fn open(
    app: AppHandle,
    window: Window,
    id: String,
    worktree: String,
    skip_seed: bool,
    selection: u64,
) -> Result<Value, Failure> {
    if !Path::new(&worktree).is_absolute() {
        return Err("Choose a worktree using its absolute path."
            .to_string()
            .into());
    }
    let (generation, cleared) = app
        .state::<Panes>()
        .with(window.label(), |pane| {
            pane.select(selection, started(), &id, &worktree)
        })
        .ok_or("This window has closed.".to_string())?
        .ok_or_else(|| Failure {
            code: "superseded".into(),
            message: "A newer selection replaced this one.".into(),
        })?;
    if cleared {
        windows::blank(&window);
    }
    let progress = Progress {
        app: app.clone(),
        window: window.label().into(),
        generation,
    };
    progress.begin();
    let (library, snapshot) = match remember(&app, &id, &worktree).await {
        Ok(remembered) => remembered,
        Err(failure) => {
            progress.fail(&failure);
            progress.settle(false);
            return Err(failure);
        }
    };
    let result = json!({"generation": generation, "library": snapshot});
    tauri::async_runtime::spawn(drive(progress, library, id, worktree, skip_seed, false));
    Ok(result)
}

/// Marks a worktree opened: acknowledged, and its repository most recent.
/// Returns the saved library and the snapshot shells received.
async fn remember(app: &AppHandle, id: &str, worktree: &str) -> Result<(Library, Value), Failure> {
    let desktop = app.state::<Desktop>();
    let (_lock, mut library) = desktop.library().await?;
    let repository = library.repository_mut(id)?;
    repository.acknowledged.insert(worktree.into());
    repository.last_opened = Some(now().as_secs());
    let snapshot = commands::save(app, &desktop, &library)?;
    Ok((library, snapshot))
}

/// Reopens the worktree `generation` displayed after its runtime stopped,
/// unless a newer open or a deselection replaced it. Trust and settings are
/// unchanged, so this starts the same runtime the user last opened.
pub fn relaunch(app: &AppHandle, window: &str, generation: u64) {
    let Some(Some((generation, id, worktree))) = app.state::<Panes>().with(window, |pane| {
        let generation = pane.resume(generation, started(), "stopped")?;
        let (id, worktree) = pane.target();
        Some((generation, id, worktree))
    }) else {
        return;
    };
    let progress = Progress {
        app: app.clone(),
        window: window.into(),
        generation,
    };
    progress.begin();
    let app = app.clone();
    tauri::async_runtime::spawn(async move {
        let library = app
            .state::<Desktop>()
            .library()
            .await
            .map(|(_, library)| library);
        match library {
            Ok(library) => drive(progress, library, id, worktree, false, true).await,
            Err(failure) => {
                progress.fail(&failure);
                progress.settle(true);
            }
        }
    });
}

/// Moves the worktree `generation` displayed to the verified address of its
/// replacement runtime, unless a newer open or a deselection replaced it.
pub fn follow(app: &AppHandle, window: &str, generation: u64, url: Url, pid: i64) {
    let Some(Some(generation)) = app
        .state::<Panes>()
        .with(window, |pane| pane.resume(generation, started(), "moved"))
    else {
        return;
    };
    let progress = Progress {
        app: app.clone(),
        window: window.into(),
        generation,
    };
    if let Some(handle) = app.get_window(window) {
        let _ = windows::sync_visibility(&handle);
    }
    if let Err(failure) = show(&progress, url, pid, true) {
        progress.fail(&failure);
    }
    progress.settle(true);
}

/// Restarts a worktree's runtime and reloads every window showing it. Without
/// `force`, the runtime is replaced only when its build no longer matches the
/// worktree's selected executable, as after a development build changes.
pub async fn restart(
    app: AppHandle,
    id: String,
    worktree: String,
    force: bool,
) -> Result<(), Failure> {
    let desktop = app.state::<Desktop>();
    let library = desktop.library().await?.1;
    let repository = library.repository(&id)?;
    let opened = {
        let lock = desktop.worktree(&worktree);
        let _serialized = lock.lock().await;
        bridge::call(
            &desktop.directory,
            Request {
                operation: "open",
                folder: Some(&worktree),
                executable: repository.executables.get(&worktree).map(String::as_str),
                global_executable: library.global_executable.as_deref(),
                restart: force,
                ..Request::default()
            },
            bridge::TIMEOUT,
        )
        .await?
    };
    app.state::<Presence>().refresh();
    if !opened["spawned"].as_bool().unwrap_or(false) {
        return Ok(());
    }
    let url = security::runtime_url(
        opened["url"]
            .as_str()
            .ok_or("Rhizome returned no workspace address.".to_string())?,
    )?;
    let pid = opened["pid"]
        .as_i64()
        .ok_or("Rhizome returned no runtime process.".to_string())?;
    for (window, generation) in app.state::<Panes>().showing(&worktree) {
        follow(&app, &window, generation, url.clone(), pid);
    }
    Ok(())
}

/// Stops a worktree's runtime because the user asked. Every window showing it
/// sleeps first, leaving the page so its event stream ends, and none relaunches
/// it until the user opens the worktree again.
pub async fn stop(app: AppHandle, worktree: String) -> Result<(), Failure> {
    for window in app.state::<Panes>().sleep(&worktree) {
        if let Some(window) = app.get_window(&window) {
            windows::blank(&window);
            let _ = windows::sync_visibility(&window);
        }
    }
    presence::publish(&app);
    let desktop = app.state::<Desktop>();
    let stopped = {
        let lock = desktop.worktree(&worktree);
        let _serialized = lock.lock().await;
        bridge::call(
            &desktop.directory,
            Request {
                operation: "stop",
                folder: Some(&worktree),
                ..Request::default()
            },
            bridge::TIMEOUT,
        )
        .await
    };
    app.state::<Presence>().refresh();
    stopped.map(|_| ())
}

/// Clears the page of the worktree `generation` gave up on, unless a newer
/// open or a deselection replaced that generation and now owns the view.
pub fn abandon(app: &AppHandle, window: &str, generation: u64) {
    let cleared = app
        .state::<Panes>()
        .with(window, |pane| pane.abandon(generation))
        .unwrap_or(false);
    if let (true, Some(window)) = (cleared, app.get_window(window)) {
        windows::blank(&window);
        let _ = windows::sync_visibility(&window);
    }
}

/// Clears a window's worktree selection. The content view leaves the runtime's
/// page, so its event stream no longer keeps that runtime alive, and the app
/// stops supervising it.
pub fn deselect(app: &AppHandle, window: &Window, selection: u64) -> Result<(), Failure> {
    let cleared = app
        .state::<Panes>()
        .with(window.label(), |pane| pane.deselect(selection))
        .unwrap_or(false);
    if cleared {
        windows::blank(window);
        windows::sync_visibility(window)?;
        presence::publish(app);
    }
    Ok(())
}

/// Runs the open steps for a generation that has begun. A failed reconnect
/// keeps supervision so the supervisor retries it.
async fn drive(
    progress: Progress,
    library: Library,
    id: String,
    worktree: String,
    skip_seed: bool,
    reconnecting: bool,
) {
    let keep = match run(&progress, &library, &id, &worktree, skip_seed).await {
        Ok(shown) => shown,
        Err(failure) => {
            progress.fail(&failure);
            reconnecting
        }
    };
    progress.settle(keep);
}

async fn run(
    progress: &Progress,
    library: &Library,
    id: &str,
    worktree: &str,
    skip_seed: bool,
) -> Result<bool, Failure> {
    let desktop = progress.app.state::<Desktop>();
    let repository = library.repository(id)?;
    let request = Request {
        folder: Some(worktree),
        executable: repository.executables.get(worktree).map(String::as_str),
        global_executable: library.global_executable.as_deref(),
        ..Request::default()
    };
    let lock = desktop.worktree(worktree);
    let _serialized = lock.lock().await;
    if !progress.current() {
        return Ok(false);
    }
    let found = discovery(
        &desktop
            .discover(&repository.root, repository.primary.as_deref())
            .await?,
    )?;
    let entry = found.worktrees.iter().find(|w| w.path == worktree).ok_or(
        "This worktree is no longer part of the repository. Choose another worktree.".to_string(),
    )?;
    if let Some(error) = &entry.error {
        return Err(error.clone().into());
    }
    if !progress.current() {
        return Ok(false);
    }
    if !entry.configured {
        progress.step("setup", json!({}));
        return Ok(false);
    }
    if entry.trust_required {
        progress.step("trust", json!({}));
        return Ok(false);
    }
    let primary_indexed = found
        .worktrees
        .iter()
        .any(|w| w.path == found.primary && w.has_database);
    if !skip_seed && !entry.has_database && found.primary != worktree && primary_indexed {
        progress.step("seeding", json!({"primary": found.primary}));
        let seeded = bridge::call(
            &desktop.directory,
            Request {
                operation: "seed",
                primary: Some(&found.primary),
                ..request
            },
            bridge::SEED_TIMEOUT,
        )
        .await;
        match seeded {
            Err(failure) if failure.code != "seed_unsupported" => {
                progress.step(
                    "seed-error",
                    json!({"code": failure.code, "message": failure.message}),
                );
                return Ok(false);
            }
            _ if !progress.current() => return Ok(false),
            _ => {}
        }
    }
    if !progress.current() {
        return Ok(false);
    }
    progress.step("starting", json!({}));
    let opened = bridge::call(
        &desktop.directory,
        Request {
            operation: "open",
            ..request
        },
        bridge::TIMEOUT,
    )
    .await?;
    drop(_serialized);
    let url = security::runtime_url(
        opened["url"]
            .as_str()
            .ok_or("Rhizome returned no workspace address.".to_string())?,
    )?;
    let pid = opened["pid"]
        .as_i64()
        .ok_or("Rhizome returned no runtime process.".to_string())?;
    show(
        progress,
        url,
        pid,
        opened["spawned"].as_bool().unwrap_or(false),
    )
}

/// Shows a verified runtime. Returns whether this generation now displays it.
fn show(progress: &Progress, url: Url, pid: i64, restarted: bool) -> Result<bool, Failure> {
    let app = &progress.app;
    let Some(window) = app.get_window(&progress.window) else {
        return Ok(false);
    };
    let load = app
        .state::<Panes>()
        .with(&progress.window, |pane| {
            pane.expect(progress.generation, &url, pid, restarted)
        })
        .unwrap_or(Load::Stale);
    match load {
        Load::Stale => return Ok(false),
        Load::Ready => {
            windows::sync_visibility(&window)?;
            progress.step("ready", json!({}));
        }
        Load::Navigate => {
            progress.step("loading", json!({}));
            window
                .get_webview(&windows::content_label(&progress.window))
                .ok_or("This window has no content view.".to_string())?
                .navigate(url)
                .map_err(|e| e.to_string())?;
            let (app, window, generation) =
                (app.clone(), progress.window.clone(), progress.generation);
            tauri::async_runtime::spawn(async move {
                tokio::time::sleep(LOAD_TIMEOUT).await;
                app.state::<Panes>().with(&window, |pane| {
                    if pane.timed_out(generation) {
                        pane.report(generation, "error", json!({
                            "code": "load_timeout",
                            "message": "The workspace did not finish loading within a minute. Rhizome may have stopped; try again.",
                        }));
                    }
                });
            });
        }
    }
    Ok(true)
}

/// Notes that the content view began loading a page, so its finish counts.
pub fn navigation_started(app: &AppHandle, window: &str, url: &Url) {
    app.state::<Panes>()
        .with(window, |pane| pane.navigation_started(url));
}

/// Shows the content once the runtime's page finishes its first load.
pub fn loaded(app: &AppHandle, window: &str, url: &Url) {
    let panes = app.state::<Panes>();
    let Some(Some(generation)) = panes.with(window, |pane| pane.loaded(url)) else {
        return;
    };
    if let Some(handle) = app.get_window(window) {
        let _ = windows::sync_visibility(&handle);
    }
    panes.with(window, |pane| pane.report(generation, "ready", json!({})));
    presence::publish(app);
}
