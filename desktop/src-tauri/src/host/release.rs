//! Letting go of held runtimes: Quit Rhizome Completely drains, stops or
//! waits for the run, then stops the runtimes; the native terminate path does
//! the same within a short budget without waiting on anything.
use super::{
    api::{label, Running},
    Api, Host,
};
use crate::{
    bridge::{self, Request},
    commands::Desktop,
};
use std::{sync::atomic::Ordering, time::Duration};
use tauri::{AppHandle, Manager};

const REQUEST: Duration = Duration::from_secs(10);
/// How often Quit Completely asks whether the run it waits for has ended.
const POLL: Duration = Duration::from_secs(1);
/// The exit path drains and stops runs within this, all hosts together.
const EXIT_BUDGET: Duration = Duration::from_secs(3);

/// A held runtime whose drain reported work, or did not answer.
pub struct Busy {
    pub api: Api,
    pub repository: String,
    /// The run the drain reported; `None` when the drain failed.
    pub running: Option<Running>,
}

impl Busy {
    /// Repository and task name, for the quit question.
    pub fn describe(busy: &[Busy]) -> Vec<(String, String)> {
        busy.iter()
            .filter_map(|b| {
                let running = b.running.as_ref()?;
                Some((
                    b.repository.clone(),
                    label(&running.name, &running.task).to_string(),
                ))
            })
            .collect()
    }
}

/// Drains every held runtime, so no new run starts, and returns those whose
/// drain answer reports a run or a started pass, or that did not answer.
pub async fn drain_all(app: &AppHandle) -> Vec<Busy> {
    drain(app.state::<Host>().named_apis()).await
}

async fn drain(apis: Vec<(String, Api)>) -> Vec<Busy> {
    let mut busy = vec![];
    for (repository, api) in apis {
        let running = match api.drain(REQUEST).await {
            Ok(None) => continue,
            Ok(Some(running)) => Some(running),
            Err(_) => None,
        };
        busy.push(Busy {
            api,
            repository,
            running,
        });
    }
    busy
}

/// Stops each busy runtime's run, recorded as stopped, and returns once
/// every stop has answered or timed out.
pub async fn stop_runs(busy: &[Busy]) {
    let stops = busy.iter().map(|b| {
        let api = b.api.clone();
        tauri::async_runtime::spawn(async move { api.stop_run(REQUEST).await })
    });
    for stop in stops.collect::<Vec<_>>() {
        let _ = stop.await;
    }
}

/// Waits until no busy runtime's drain reports work. A runtime that stops
/// answering has nothing left to wait for.
pub async fn wait_idle(busy: &[Busy]) {
    for b in busy.iter().filter(|b| b.running.is_some()) {
        while let Ok(Some(_)) = b.api.drain(REQUEST).await {
            tokio::time::sleep(POLL).await;
        }
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

#[cfg(test)]
#[path = "release_tests.rs"]
mod tests;
