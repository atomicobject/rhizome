//! Letting go of held runtimes: Quit Rhizome Completely drains, stops or
//! waits for the run, then stops the runtimes; the native terminate path does
//! the same within a short budget without waiting on anything.
use super::Host;
use crate::{
    bridge::{self, Request},
    commands::Desktop,
};
use std::{sync::atomic::Ordering, time::Duration};
use tauri::{AppHandle, Manager};

const REQUEST: Duration = Duration::from_secs(10);
/// The exit path drains and stops runs within this, all hosts together.
const EXIT_BUDGET: Duration = Duration::from_secs(3);

/// Drains every held runtime, so no new run starts.
pub async fn drain_all(app: &AppHandle) {
    for api in app.state::<Host>().apis() {
        let _ = api.drain(REQUEST).await;
    }
}

/// Stops each active run, recorded as stopped.
pub async fn stop_runs(app: &AppHandle) {
    for api in app.state::<Host>().apis() {
        let _ = api.stop_run(REQUEST).await;
    }
}

/// Quit Completely's last step: stops holding, then stops each headless
/// runtime the app held, as Stop Rhizome does. Attached runtimes belong to
/// their terminal.
pub async fn release_all(app: &AppHandle) {
    let directory = app.state::<Desktop>().directory.clone();
    let host = app.state::<Host>();
    host.closing.store(true, Ordering::SeqCst);
    let _syncing = host.syncing.lock().await;
    let stops = host
        .release()
        .into_iter()
        .filter(|held| held.headless)
        .map(|held| {
            let directory = directory.clone();
            tauri::async_runtime::spawn(async move {
                let request = Request {
                    operation: "stop",
                    folder: Some(&held.checkout),
                    ..Request::default()
                };
                let _ = bridge::call(&directory, request, Duration::from_secs(30)).await;
            })
        })
        .collect::<Vec<_>>();
    for stop in stops {
        let _ = stop.await;
    }
}

/// The native terminate path (`RunEvent::Exit`: logout, restart, shutdown,
/// Dock Quit, updater restart, or the end of Quit Completely). It is never
/// prevented and never asks. Within [`EXIT_BUDGET`] it drains each runtime
/// still held and stops its active run, so the run is recorded as
/// interrupted, then hands the runtime stops to companions that finish after
/// the app has gone. It does not wait for an open the loop has in flight; a
/// runtime that open starts idle-exits, since no stream holds it.
pub fn terminate(app: &AppHandle) {
    let Some(host) = app.try_state::<Host>() else {
        return;
    };
    let released = host.release();
    if released.is_empty() {
        return;
    }
    let directory = app.state::<Desktop>().directory.clone();
    let stopping: Vec<_> = released
        .iter()
        .filter_map(|held| held.api.clone().map(|api| (api, held.running)))
        .map(|(api, running)| {
            tauri::async_runtime::spawn(async move {
                let drained = api.drain(EXIT_BUDGET).await;
                if running || matches!(drained, Ok(Some(_))) {
                    let _ = api.stop_run(EXIT_BUDGET).await;
                }
            })
        })
        .collect();
    tauri::async_runtime::block_on(async {
        let _ = tokio::time::timeout(EXIT_BUDGET, async {
            for stop in stopping {
                let _ = stop.await;
            }
        })
        .await;
    });
    for held in released.iter().filter(|held| held.headless) {
        let request = Request {
            operation: "stop",
            folder: Some(&held.checkout),
            ..Request::default()
        };
        if let Err(error) = bridge::send(&directory, request) {
            eprintln!("Rhizome Desktop could not stop {}: {error}", held.checkout);
        }
    }
}
