//! Quitting on macOS (SPEC-0104 US8). Quit Rhizome (⌘Q) stashes the window
//! session, closes the windows, and keeps the menu bar host. Quit Rhizome
//! Completely asks whether to wait for or stop a running task, drains every
//! held runtime, stops the runtimes the app holds, and exits through the
//! normal terminate path. "Keep running in the menu bar" off makes ⌘Q quit
//! completely.
use crate::{commands::Desktop, host, login, state::Preferences, windows};
use serde_json::{json, Value};
use std::sync::atomic::{AtomicBool, Ordering};
use tauri::{
    menu::{MenuItem, Submenu},
    AppHandle, Manager, Wry,
};
use tauri_plugin_dialog::{DialogExt, MessageDialogButtons, MessageDialogResult};

static WAITING: AtomicBool = AtomicBool::new(false);

/// Quit Completely is waiting for a run to finish.
pub fn waiting() -> bool {
    WAITING.load(Ordering::SeqCst)
}

fn preferences(app: &AppHandle) -> Preferences {
    Preferences::load(&app.state::<Desktop>().directory)
}

/// Changes a preference under the preferences lock and returns the result.
fn change(app: &AppHandle, f: impl FnOnce(&mut Preferences)) -> Result<Preferences, String> {
    let desktop = app.state::<Desktop>();
    let _saving = desktop.preferences.lock().unwrap();
    let mut saved = Preferences::load(&desktop.directory);
    f(&mut saved);
    saved.save(&desktop.directory)?;
    Ok(saved)
}

/// The last window closed: Rhizome stays in the menu bar, without a Dock icon.
pub fn to_menu_bar(app: &AppHandle) {
    let handle = app.clone();
    let _ = app.run_on_main_thread(move || {
        if handle.windows().is_empty() {
            let _ = handle.set_activation_policy(tauri::ActivationPolicy::Accessory);
        }
    });
}

/// ⌘Q. The first time, a notice explains that Rhizome keeps running.
pub fn quit(app: &AppHandle) {
    if !preferences(app).keep_in_menu_bar.0 {
        return quit_completely(app);
    }
    let first = !preferences(app).menu_bar_notice_shown
        && change(app, |saved| saved.menu_bar_notice_shown = true).is_ok();
    if !first {
        return windows::stash(app);
    }
    let handle = app.clone();
    let mut dialog = app.dialog().message(
            "Rhizome keeps running in the menu bar so scheduled tasks run on time, and your windows come back when you open it again. To stop it, choose Quit Rhizome Completely from the Rhizome menu or the menu bar icon.",
        )
    .title("Rhizome is still running")
    .buttons(MessageDialogButtons::OkCustom("OK".into()));
    // A sheet on the window ⌘Q came from, so Return dismisses it.
    if let Some(window) = windows::focused_window(app) {
        dialog = dialog.parent(&window);
    }
    dialog.show(move |_| windows::stash(&handle));
}

#[derive(Debug, PartialEq)]
enum Choice {
    Wait,
    Stop,
    Cancel,
}

/// Which quit question to ask.
#[derive(Clone, Copy, Debug, PartialEq)]
enum Question {
    /// Before draining: wait, stop, or keep Rhizome running.
    Quit,
    /// The drain found work the status had not shown; quitting has begun,
    /// so the only choice is wait or stop.
    Started,
    /// An earlier Quit Completely is waiting: stop now, or keep waiting.
    Waiting,
}

const WAIT: &str = "Wait";
const STOP: &str = "Stop Task";
const KEEP_WAITING: &str = "Keep Waiting";

/// Quit Rhizome Completely. The streamed status decides whether to ask
/// before draining; each runtime's drain answer then decides what to stop
/// or wait for, so a run that started since the last status, or one whose
/// stream dropped, is never left behind.
pub fn quit_completely(app: &AppHandle) {
    let app = app.clone();
    tauri::async_runtime::spawn(async move {
        let asked = match waiting() {
            true => Question::Waiting,
            false => Question::Quit,
        };
        let running = app.state::<host::Host>().running();
        let mut choice = None;
        if !running.is_empty() {
            match ask(&app, &running, asked).await {
                Choice::Cancel => return,
                answer => choice = Some(answer),
            }
        }
        let first = !WAITING.swap(true, Ordering::SeqCst);
        crate::tray::refresh(&app);
        // No runtime joins the host while quitting, so the drain sees them all.
        host::freeze(&app).await;
        let busy = host::drain_all(&app).await;
        let started = host::Busy::describe(&busy);
        let stop = match choice {
            Some(answer) => answer == Choice::Stop,
            None if started.is_empty() => false,
            None => {
                let question = if first {
                    Question::Started
                } else {
                    Question::Waiting
                };
                match ask(&app, &started, question).await {
                    Choice::Cancel => return,
                    answer => answer == Choice::Stop,
                }
            }
        };
        if stop {
            host::stop_runs(&busy).await;
        }
        // A later Quit Completely only stops; the first one finishes.
        if !first {
            return;
        }
        if !stop {
            host::wait_idle(&busy).await;
        }
        host::release_all(&app).await;
        app.exit(0);
    });
}

/// What the dialog says about the runs going on.
fn describe(running: &[(String, String)]) -> String {
    match running {
        [(repository, task)] => format!("{task} is running in {repository}."),
        runs => format!(
            "{} tasks are running: {}.",
            runs.len(),
            runs.iter()
                .map(|(repository, task)| format!("{task} in {repository}"))
                .collect::<Vec<_>>()
                .join(", ")
        ),
    }
}

async fn ask(app: &AppHandle, running: &[(String, String)], question: Question) -> Choice {
    let (sent, chosen) = tokio::sync::oneshot::channel();
    let wait_or_stop = format!(
        "{} Wait for it to finish before quitting, or stop it now? A stopped run is recorded as stopped.",
        describe(running)
    );
    let (message, buttons) = match question {
        Question::Quit => (
            wait_or_stop,
            MessageDialogButtons::YesNoCancelCustom(WAIT.into(), STOP.into(), "Cancel".into()),
        ),
        Question::Started => (
            wait_or_stop,
            MessageDialogButtons::OkCancelCustom(WAIT.into(), STOP.into()),
        ),
        Question::Waiting => (
            format!(
                "{} Rhizome quits when it finishes. Stop it and quit now?",
                describe(running)
            ),
            MessageDialogButtons::OkCancelCustom(STOP.into(), KEEP_WAITING.into()),
        ),
    };
    let mut dialog = app
        .dialog()
        .message(message)
        .title("Quit Rhizome Completely")
        .buttons(buttons);
    if let Some(window) = windows::focused_window(app) {
        dialog = dialog.parent(&window);
    }
    dialog.show_with_result(move |result| {
        let _ = sent.send(result);
    });
    choice(chosen.await.unwrap_or_default(), question)
}

fn choice(result: MessageDialogResult, question: Question) -> Choice {
    match (result, question) {
        (MessageDialogResult::Custom(label), _) if label == WAIT => Choice::Wait,
        (MessageDialogResult::Custom(label), _) if label == STOP => Choice::Stop,
        (MessageDialogResult::Yes, Question::Quit) => Choice::Wait,
        (MessageDialogResult::No, Question::Quit)
        | (MessageDialogResult::Ok, Question::Waiting) => Choice::Stop,
        // Quitting has begun, so dismissing the question waits.
        (_, Question::Started) => Choice::Wait,
        _ => Choice::Cancel,
    }
}

/// The app menu's Quit Rhizome Completely, shown only while ⌘Q keeps
/// Rhizome in the menu bar; otherwise ⌘Q itself quits completely.
struct QuitItems {
    menu: Submenu<Wry>,
    completely: MenuItem<Wry>,
}

pub fn manage_items(app: &AppHandle, menu: Submenu<Wry>, completely: MenuItem<Wry>) {
    app.manage(QuitItems { menu, completely });
    refresh_items(app);
}

fn refresh_items(app: &AppHandle) {
    let Some(items) = app.try_state::<QuitItems>() else {
        return;
    };
    let keep = preferences(app).keep_in_menu_bar.0;
    let shown = (items.menu.items().unwrap_or_default().iter())
        .any(|item| item.id() == items.completely.id());
    if keep && !shown {
        let _ = items.menu.append(&items.completely);
    } else if !keep && shown {
        let _ = items.menu.remove(&items.completely);
    }
}

/// Keeps or drops Quit Rhizome Completely in the app menu, and relabels the
/// menu bar's quit, after "Keep running in the menu bar" changes.
fn keep_changed(app: &AppHandle) {
    refresh_items(app);
    crate::tray::refresh(app);
}

/// What Settings shows under Menu bar.
pub fn settings(app: &AppHandle) -> Value {
    let saved = preferences(app);
    json!({
        "keepInMenuBar": saved.keep_in_menu_bar.0,
        "launchAtLogin": saved.launch_at_login,
    })
}

/// Saves a change from Settings. Launch at login registers or removes the
/// login item first, so a failure leaves the setting as it was.
pub fn set_settings(
    app: &AppHandle,
    keep_in_menu_bar: Option<bool>,
    launch_at_login: Option<bool>,
) -> Result<Value, String> {
    if let Some(enabled) = launch_at_login {
        login::set(&app.config().identifier, enabled)?;
    }
    let before = preferences(app).keep_in_menu_bar.0;
    let saved = change(app, |saved| {
        if let Some(keep) = keep_in_menu_bar {
            saved.keep_in_menu_bar.0 = keep;
        }
        if let Some(enabled) = launch_at_login {
            saved.launch_at_login = enabled;
        }
    })?;
    if saved.keep_in_menu_bar.0 != before {
        keep_changed(app);
    }
    Ok(settings(app))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_running_task_question_maps_each_answer() {
        let custom = |label: &str| MessageDialogResult::Custom(label.into());
        use Question::*;
        assert_eq!(choice(custom(WAIT), Quit), Choice::Wait);
        assert_eq!(choice(custom(STOP), Quit), Choice::Stop);
        assert_eq!(choice(custom("Cancel"), Quit), Choice::Cancel);
        assert_eq!(choice(MessageDialogResult::Cancel, Quit), Choice::Cancel);
        assert_eq!(choice(MessageDialogResult::Yes, Quit), Choice::Wait);
        assert_eq!(choice(MessageDialogResult::No, Quit), Choice::Stop);
        // A run the drain found after quitting began: wait or stop, never cancel.
        assert_eq!(choice(custom(STOP), Started), Choice::Stop);
        assert_eq!(choice(custom(WAIT), Started), Choice::Wait);
        assert_eq!(choice(MessageDialogResult::Cancel, Started), Choice::Wait);
        // While already waiting, the question is only whether to stop.
        assert_eq!(choice(custom(STOP), Waiting), Choice::Stop);
        assert_eq!(choice(MessageDialogResult::Ok, Waiting), Choice::Stop);
        assert_eq!(choice(custom(KEEP_WAITING), Waiting), Choice::Cancel);
    }

    #[test]
    fn the_question_names_each_running_task() {
        assert_eq!(
            describe(&[("notes".into(), "Daily digest".into())]),
            "Daily digest is running in notes."
        );
        assert_eq!(
            describe(&[
                ("notes".into(), "Daily digest".into()),
                ("site".into(), "Sync".into())
            ]),
            "2 tasks are running: Daily digest in notes, Sync in site."
        );
    }
}
