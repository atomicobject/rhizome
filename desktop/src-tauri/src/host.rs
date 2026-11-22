//! The scheduled-task host (SPEC-0104 US7). A loop independent of windows
//! keeps a runtime up for every repository with "Run tasks on this machine"
//! on, and holds its `GET /api/tasks/host` stream: the runtime dispatches
//! scheduled tasks only while a host holds it, and the stream counts as
//! activity, so the runtime does not idle-exit. The menu bar menu and task
//! notifications read the streamed status.
// The menu bar host runs only on macOS; other platforms compile it for tests.
#![cfg_attr(not(target_os = "macos"), allow(dead_code))]
use crate::{bridge, commands::Desktop, pipeline, state::Library};
use serde::Deserialize;
use std::{
    collections::BTreeMap,
    path::{Path, PathBuf},
    sync::{
        atomic::{AtomicBool, Ordering},
        Mutex,
    },
    time::{Duration, Instant},
};
use tauri::{async_runtime::JoinHandle, AppHandle, Manager};
use tokio::sync::Notify;

#[path = "host/api.rs"]
pub mod api;
#[path = "host/notices.rs"]
mod notices;
#[path = "host/release.rs"]
mod release;

pub use api::{Api, HostStatus};
pub use release::{drain_all, release_all, stop_runs, terminate, wait_idle, Busy};

/// How often the loop lists hosted repositories when nothing wakes it.
const SYNC: Duration = Duration::from_secs(30);
const LIST_TIMEOUT: Duration = Duration::from_secs(30);

/// A repository `rzm tasks hosts --json` lists.
#[derive(Clone, Debug, Deserialize, PartialEq)]
#[serde(rename_all = "camelCase")]
pub struct Listed {
    pub common_dir: String,
    pub checkout: String,
    #[serde(default)]
    pub run_here: bool,
}

/// A repository this app hosts tasks for.
#[derive(Default)]
struct Held {
    checkout: String,
    name: String,
    /// Library repositories with this common directory, for the Dock badge.
    ids: Vec<String>,
    api: Option<Api>,
    headless: bool,
    status: Option<HostStatus>,
    problem: Option<String>,
    failures: u32,
    retry_at: Option<Instant>,
    stream: Option<JoinHandle<()>>,
    generation: u64,
}

impl Held {
    fn drop_stream(&mut self) {
        if let Some(stream) = self.stream.take() {
            stream.abort();
        }
    }
}

/// What the menu bar shows for one hosted repository.
#[derive(Clone, Debug, Default, PartialEq)]
pub struct View {
    pub key: String,
    pub name: String,
    pub status: Option<HostStatus>,
    pub problem: Option<String>,
}

/// A held runtime the app is letting go of.
pub struct Release {
    pub api: Option<Api>,
    pub checkout: String,
    pub headless: bool,
    pub running: bool,
}

#[derive(Default)]
pub struct Host {
    hosts: Mutex<BTreeMap<String, Held>>,
    /// Listing hosted repositories failed, for the menu bar.
    listing: Mutex<Option<String>>,
    wake: Notify,
    /// Quit Completely or exit began: ensure no runtime, hold no stream.
    closing: AtomicBool,
    /// Held while the loop lists and opens runtimes, so letting go waits for
    /// an open in flight rather than leaving the runtime it starts behind.
    syncing: tokio::sync::Mutex<()>,
    notices: Mutex<Option<notices::Notified>>,
}

impl Host {
    /// Lists and reconnects soon.
    pub fn refresh(&self) {
        self.wake.notify_one();
    }

    pub fn closing(&self) -> bool {
        self.closing.load(Ordering::SeqCst)
    }

    pub fn views(&self) -> Vec<View> {
        let hosts = self.hosts.lock().unwrap();
        hosts
            .iter()
            .map(|(key, held)| View {
                key: key.clone(),
                name: held.name.clone(),
                status: held.status.clone(),
                problem: held.problem.clone(),
            })
            .collect()
    }

    pub fn listing_problem(&self) -> Option<String> {
        self.listing.lock().unwrap().clone()
    }

    pub fn api(&self, key: &str) -> Option<Api> {
        self.hosts.lock().unwrap().get(key)?.api.clone()
    }

    pub fn apis(&self) -> Vec<Api> {
        let hosts = self.hosts.lock().unwrap();
        hosts.values().filter_map(|held| held.api.clone()).collect()
    }

    /// Each held runtime with its repository's name.
    pub fn named_apis(&self) -> Vec<(String, Api)> {
        let hosts = self.hosts.lock().unwrap();
        (hosts.values())
            .filter_map(|held| Some((held.name.clone(), held.api.clone()?)))
            .collect()
    }

    /// Repository and task names of the runs going on now.
    pub fn running(&self) -> Vec<(String, String)> {
        let hosts = self.hosts.lock().unwrap();
        hosts
            .values()
            .filter_map(|held| {
                let running = held.status.as_ref()?.active()?;
                Some((
                    held.name.clone(),
                    api::label(&running.name, &running.task).to_string(),
                ))
            })
            .collect()
    }

    /// The inbox count per library repository id, for hosted repositories
    /// whose runtime has reported.
    pub fn attention(&self) -> BTreeMap<String, u32> {
        let hosts = self.hosts.lock().unwrap();
        let mut counts = BTreeMap::new();
        for held in hosts.values() {
            if let Some(status) = &held.status {
                for id in &held.ids {
                    counts.insert(id.clone(), status.attention);
                }
            }
        }
        counts
    }

    /// Keeps the listed repositories with "run here" on, dropping the rest,
    /// and returns those that need a runtime and stream now.
    fn reconcile(&self, listed: &[Listed], library: &Library) -> Vec<(String, String)> {
        let mut hosts = self.hosts.lock().unwrap();
        let wanted: BTreeMap<&str, &Listed> = listed
            .iter()
            .filter(|l| l.run_here)
            .map(|l| (l.common_dir.as_str(), l))
            .collect();
        hosts.retain(|key, held| {
            let keep = wanted
                .get(key.as_str())
                .is_some_and(|l| l.checkout == held.checkout);
            if !keep {
                held.drop_stream();
            }
            keep
        });
        let now = Instant::now();
        let mut due = vec![];
        for (key, listed) in wanted {
            let held = hosts.entry(key.to_string()).or_insert_with(|| {
                let repositories = library.repositories.iter().filter(|r| r.root == key);
                Held {
                    checkout: listed.checkout.clone(),
                    ids: repositories.clone().map(|r| r.id.clone()).collect(),
                    name: repositories
                        .map(|r| r.name.clone())
                        .next()
                        .unwrap_or_else(|| {
                            let checkout = Path::new(&listed.checkout);
                            checkout
                                .file_name()
                                .map_or(listed.checkout.clone(), |name| {
                                    name.to_string_lossy().into_owned()
                                })
                        }),
                    ..Held::default()
                }
            });
            if held.stream.is_none() && held.retry_at.map_or(true, |at| at <= now) {
                due.push((key.to_string(), held.checkout.clone()));
            }
        }
        due
    }

    /// Records a newer status from the stream `generation` and returns the
    /// notices to show; `None` when the stream is stale or "run here" went
    /// off, which drops the host until it is listed again.
    fn record(&self, key: &str, generation: u64, status: HostStatus) -> Option<Vec<api::Notice>> {
        let mut hosts = self.hosts.lock().unwrap();
        let held = hosts.get_mut(key).filter(|h| h.generation == generation)?;
        if !status.run_here {
            held.drop_stream();
            hosts.remove(key);
            return None;
        }
        held.failures = 0;
        held.problem = None;
        let notices = status.notices.clone();
        held.status = Some(status);
        drop(hosts);
        Some(notices)
    }

    /// A stream ended or a runtime could not start: retry with backoff.
    fn failed(&self, key: &str, generation: Option<u64>, problem: String) -> Duration {
        let mut hosts = self.hosts.lock().unwrap();
        let Some(held) = hosts
            .get_mut(key)
            .filter(|h| generation.map_or(true, |g| g == h.generation))
        else {
            return SYNC;
        };
        // The runtime may still run a task, so quitting can still drain it.
        held.stream = None;
        held.status = None;
        held.problem = Some(problem);
        held.failures += 1;
        let delay = backoff(held.failures);
        held.retry_at = Some(Instant::now() + delay);
        delay
    }

    /// Stops holding every runtime: no stream, no new runtime. Returns what
    /// each held, for the caller to drain and stop.
    pub fn release(&self) -> Vec<Release> {
        self.closing.store(true, Ordering::SeqCst);
        let mut hosts = self.hosts.lock().unwrap();
        let released = hosts
            .values_mut()
            .map(|held| {
                held.drop_stream();
                Release {
                    api: held.api.take(),
                    checkout: held.checkout.clone(),
                    headless: held.headless,
                    running: held.status.as_ref().is_some_and(|s| s.active().is_some()),
                }
            })
            .collect();
        hosts.clear();
        released
    }
}

/// 2, 4, 8, … seconds, at most a minute.
pub fn backoff(failures: u32) -> Duration {
    Duration::from_secs(2u64.saturating_pow(failures.clamp(1, 6)).min(60))
}

/// Starts the host loop: at launch, every 30 seconds, and when woken, it
/// lists hosted repositories, ensures their runtimes, and holds their streams.
pub fn start(app: AppHandle) {
    tauri::async_runtime::spawn(async move {
        let host = app.state::<Host>();
        while !host.closing() {
            sync(&app).await;
            let _ = tokio::time::timeout(SYNC, host.wake.notified()).await;
        }
    });
}

async fn sync(app: &AppHandle) {
    let host = app.state::<Host>();
    let _syncing = host.syncing.lock().await;
    if host.closing() {
        return;
    }
    let directory = app.state::<Desktop>().directory.clone();
    let Ok(library) = app.state::<Desktop>().library().await.map(|(_, l)| l) else {
        return;
    };
    let listed = match list(&directory, &library).await {
        Ok(listed) => listed,
        // Keep holding what is held; a later listing decides.
        Err(problem) => {
            *host.listing.lock().unwrap() = Some(problem);
            crate::tray::refresh(app);
            return;
        }
    };
    *host.listing.lock().unwrap() = None;
    for (key, checkout) in host.reconcile(&listed, &library) {
        if host.closing() {
            return;
        }
        match pipeline::hold(app, &checkout).await {
            Ok(opened) => attach(app, &key, opened),
            Err(failure) => {
                host.failed(&key, None, failure.message);
            }
        }
    }
    crate::tray::refresh(app);
    crate::inbox::refresh(app);
}

/// The repositories with task hosting settings, from the app's `rzm`. It runs
/// in the app's data directory, so no repository pin redirects it.
async fn list(directory: &Path, library: &Library) -> Result<Vec<Listed>, String> {
    let rzm = match &library.global_executable {
        Some(path) => PathBuf::from(path),
        None => bridge::sidecar()?,
    };
    let output = crate::process::command(&rzm)
        .args(["tasks", "hosts", "--json"])
        .current_dir(directory)
        .stdin(std::process::Stdio::null())
        .kill_on_drop(true)
        .output();
    let output = tokio::time::timeout(LIST_TIMEOUT, output)
        .await
        .map_err(|_| "Listing task hosts took too long.".to_string())?
        .map_err(|e| format!("Cannot run {}: {e}", rzm.display()))?;
    if !output.status.success() {
        return Err(format!(
            "{} cannot list task hosts. Update Rhizome.",
            rzm.display()
        ));
    }
    serde_json::from_slice(&output.stdout).map_err(|_| "Rhizome listed task hosts badly.".into())
}

/// Holds `key`'s stream on the runtime the host loop just opened.
fn attach(app: &AppHandle, key: &str, opened: pipeline::Opened) {
    let api = match Api::new(opened.url) {
        Ok(api) => api,
        Err(problem) => {
            app.state::<Host>().failed(key, None, problem);
            return;
        }
    };
    let host = app.state::<Host>();
    let mut hosts = host.hosts.lock().unwrap();
    let Some(held) = hosts.get_mut(key) else {
        return;
    };
    held.generation += 1;
    held.api = Some(api.clone());
    held.headless = opened.mode == "headless";
    held.retry_at = None;
    // Quitting lets this runtime go once the open finishes; hold nothing.
    if host.closing() {
        return;
    }
    let (app, key, generation) = (app.clone(), key.to_string(), held.generation);
    held.stream = Some(tauri::async_runtime::spawn(async move {
        let problem = api
            .stream(|status| {
                let host = app.state::<Host>();
                match host.record(&key, generation, status) {
                    Some(notices) => notify(&app, &key, &notices),
                    None => host.refresh(),
                }
                crate::tray::refresh(&app);
                crate::inbox::refresh(&app);
            })
            .await;
        let host = app.state::<Host>();
        let delay = host.failed(&key, Some(generation), problem);
        crate::tray::refresh(&app);
        tokio::time::sleep(delay).await;
        host.refresh();
    }));
}

/// Shows each notice not shown before, here or in an earlier run.
fn notify(app: &AppHandle, key: &str, list: &[api::Notice]) {
    use tauri_plugin_notification::NotificationExt;
    if list.is_empty() {
        return;
    }
    let directory = app.state::<Desktop>().directory.clone();
    let host = app.state::<Host>();
    let fresh = {
        let mut notified = host.notices.lock().unwrap();
        let notified = notified.get_or_insert_with(|| notices::Notified::load(&directory));
        let fresh = notified.fresh(key, list);
        if !fresh.is_empty() {
            notified.save(&directory);
        }
        fresh
    };
    for notice in fresh {
        let _ = app
            .notification()
            .builder()
            .title(&notice.title)
            .body(&notice.body)
            .show();
    }
}

#[cfg(test)]
#[path = "host/tests.rs"]
mod tests;
