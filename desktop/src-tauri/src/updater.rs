//! Self-update from the GitHub release feed. The app checks at launch and every
//! day, downloads in the background, and installs only when the user restarts
//! it, from the shell's control, the menu, or `rzm update`, which launches the
//! app with `--install-update`. Rhizome Dev never updates itself.
use crate::{pane::Panes, windows};
use serde::Serialize;
use serde_json::json;
use std::{sync::Mutex, time::Duration};
use tauri::{AppHandle, Manager};
use tauri_plugin_dialog::{DialogExt, MessageDialogButtons};
use tauri_plugin_updater::{Update, UpdaterExt};

const INTERVAL: Duration = Duration::from_secs(24 * 60 * 60);
const INSTALL_ARGUMENT: &str = "--install-update";
/// Set just before relaunching after an install. The relaunched process
/// inherits it along with the original arguments, so a carried-over
/// `--install-update` is not mistaken for a new request.
const RESTARTED: &str = "RHIZOME_UPDATE_RESTARTED";

#[derive(Clone, Debug, PartialEq, Serialize)]
#[serde(tag = "state", rename_all = "kebab-case")]
pub enum Status {
    Disabled,
    Idle,
    Checking,
    Current { version: String },
    Downloading { version: String },
    Ready { version: String },
    Failed { message: String },
}

pub struct Updates {
    status: Mutex<Status>,
    ready: Mutex<Option<(Update, Vec<u8>)>>,
    running: tokio::sync::Mutex<()>,
}

impl Default for Updates {
    fn default() -> Self {
        Self {
            status: Mutex::new(Status::Idle),
            ready: Mutex::new(None),
            running: tokio::sync::Mutex::new(()),
        }
    }
}

impl Updates {
    pub fn status(&self) -> Status {
        self.status.lock().unwrap().clone()
    }
}

/// Rhizome Dev (the `.dev` identifier) and local builds (version 0.0.0) never
/// update; only a release build replaces itself.
pub fn enabled(identifier: &str, version: &str) -> bool {
    !identifier.ends_with(".dev") && version != "0.0.0"
}

pub fn enabled_for(app: &AppHandle) -> bool {
    enabled(
        &app.config().identifier,
        &app.package_info().version.to_string(),
    )
}

fn set(app: &AppHandle, status: Status) {
    *app.state::<Updates>().status.lock().unwrap() = status.clone();
    app.state::<Panes>()
        .broadcast(&json!({"type": "update", "status": status}));
}

/// Starts the launch check and the daily schedule.
pub fn start(app: &AppHandle) {
    if !enabled_for(app) {
        set(app, Status::Disabled);
        return;
    }
    let app = app.clone();
    tauri::async_runtime::spawn(async move {
        loop {
            check(&app).await;
            tokio::time::sleep(INTERVAL).await;
        }
    });
}

/// Checks the feed and downloads a newer verified release, keeping it ready
/// for a restart. A failure leaves the app as it is.
pub async fn check(app: &AppHandle) -> Status {
    if !enabled_for(app) {
        return Status::Disabled;
    }
    let updates = app.state::<Updates>();
    let _running = updates.running.lock().await;
    set(app, Status::Checking);
    let status = match download(app).await {
        Ok(status) => status,
        // A verified download stays installable when refreshing the feed or
        // fetching a newer release fails.
        Err(error) => match updates.ready.lock().unwrap().as_ref() {
            Some((ready, _)) => Status::Ready {
                version: ready.version.clone(),
            },
            None => Status::Failed {
                message: error.to_string(),
            },
        },
    };
    set(app, status.clone());
    status
}

/// Reads the feed every time, so a postponed download is replaced when a newer
/// release appears and dropped when the feed no longer offers it.
async fn download(app: &AppHandle) -> tauri_plugin_updater::Result<Status> {
    let Some(update) = app.updater()?.check().await? else {
        app.state::<Updates>().ready.lock().unwrap().take();
        return Ok(Status::Current {
            version: app.package_info().version.to_string(),
        });
    };
    let version = update.version.clone();
    let cached = app
        .state::<Updates>()
        .ready
        .lock()
        .unwrap()
        .as_ref()
        .map(|(ready, _)| ready.version.clone());
    if cached.as_deref() == Some(version.as_str()) {
        return Ok(Status::Ready { version });
    }
    set(
        app,
        Status::Downloading {
            version: version.clone(),
        },
    );
    // Download verifies the signature against the embedded public key.
    let bytes = update.download(|_, _| {}, || {}).await?;
    *app.state::<Updates>().ready.lock().unwrap() = Some((update, bytes));
    Ok(Status::Ready { version })
}

/// Installs the downloaded release and relaunches, restoring the windows.
/// Running Rhizome runtimes are separate processes and keep running.
pub fn install(app: &AppHandle) -> Result<(), String> {
    let ready = app.state::<Updates>().ready.lock().unwrap().take();
    let Some((update, bytes)) = ready else {
        return Err("No downloaded update is ready to install.".into());
    };
    if let Err(error) = update.install(bytes) {
        let message = format!("Cannot install Rhizome {}: {error}", update.version);
        set(
            app,
            Status::Failed {
                message: message.clone(),
            },
        );
        return Err(message);
    }
    windows::quit(app);
    std::env::set_var(RESTARTED, "1");
    app.restart();
}

fn requested(args: &[String], restarted: bool) -> bool {
    !restarted && args.iter().any(|arg| arg == INSTALL_ARGUMENT)
}

/// Reads and clears the restart marker; call once at launch.
pub fn take_restart_marker() -> bool {
    let restarted = std::env::var_os(RESTARTED).is_some();
    std::env::remove_var(RESTARTED);
    restarted
}

/// Handles `--install-update` from `rzm update`: an explicit request to update
/// now. Without a newer verified release it does nothing.
pub fn handle(app: &AppHandle, args: &[String], restarted: bool) -> bool {
    if !requested(args, restarted) {
        return false;
    }
    let app = app.clone();
    tauri::async_runtime::spawn(async move {
        if let Status::Ready { .. } = check(&app).await {
            let _ = install(&app);
        }
    });
    true
}

/// "Check for Updates…": checks now and reports the outcome in a dialog.
pub fn check_from_menu(app: &AppHandle) {
    let app = app.clone();
    tauri::async_runtime::spawn(async move {
        let message = match check(&app).await {
            Status::Ready { version } => {
                let restart = app.clone();
                app.dialog()
                    .message(format!(
                        "Rhizome {version} is ready. Restart now to finish updating? Open repositories reopen after the restart."
                    ))
                    .title("Rhizome Updates")
                    .buttons(MessageDialogButtons::OkCancelCustom(
                        "Restart Now".into(),
                        "Later".into(),
                    ))
                    .show(move |confirmed| {
                        if confirmed {
                            let _ = install(&restart);
                        }
                    });
                return;
            }
            Status::Current { version } => format!("Rhizome {version} is up to date."),
            Status::Failed { message } => format!("Rhizome could not check for updates: {message}"),
            Status::Disabled => {
                "This development build does not update itself. Rhizome Dev is updated with make desktop-install-dev.".into()
            }
            Status::Idle | Status::Checking | Status::Downloading { .. } => return,
        };
        app.dialog()
            .message(message)
            .title("Rhizome Updates")
            .show(|_| {});
    });
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn only_release_builds_update() {
        assert!(enabled("com.atomicobject.rhizome", "0.51.0"));
        assert!(!enabled("com.atomicobject.rhizome.dev", "0.51.0"));
        assert!(!enabled("com.atomicobject.rhizome", "0.0.0"));
    }

    #[test]
    fn only_the_exact_argument_requests_an_install() {
        let args = |list: &[&str]| list.iter().map(|a| a.to_string()).collect::<Vec<_>>();
        assert!(requested(&args(&["Rhizome", "--install-update"]), false));
        assert!(!requested(&args(&["Rhizome", "--open", "/work"]), false));
        assert!(!requested(
            &args(&["Rhizome", "--install-update=now"]),
            false
        ));
        // The relaunch after an install keeps the argument but is not a request.
        assert!(!requested(&args(&["Rhizome", "--install-update"]), true));
    }

    #[test]
    fn status_serializes_for_the_shell() {
        assert_eq!(
            serde_json::to_value(Status::Ready {
                version: "0.51.0".into()
            })
            .unwrap(),
            json!({"state": "ready", "version": "0.51.0"})
        );
        assert_eq!(
            serde_json::to_value(Status::Disabled).unwrap(),
            json!({"state": "disabled"})
        );
    }
}
