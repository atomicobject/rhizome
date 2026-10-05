use crate::{
    bridge::{self, Failure, RepositoryRef, Request},
    commands::{self, discovery, Desktop},
    pane::{Panes, Supervise},
    pipeline,
    state::{Discovery, Library},
};
use serde::Deserialize;
use serde_json::{json, Value};
use std::{
    collections::{BTreeMap, BTreeSet},
    future::Future,
    sync::{
        atomic::{AtomicBool, Ordering},
        Mutex,
    },
    time::{Duration, Instant, SystemTime},
};
use tauri::{AppHandle, Manager};
use tokio::sync::Notify;

const POLL: Duration = Duration::from_secs(3);
const DISCOVERY: Duration = Duration::from_secs(30);
const TIMEOUT: Duration = Duration::from_secs(20);

#[derive(Clone, Copy, Debug, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
pub enum State {
    Running,
    Starting,
    Stopped,
}

/// A worktree's runtime as the bridge verified it. `url` is present only for
/// a running runtime whose identity and loopback origin were checked.
#[derive(Clone, Debug, Deserialize, PartialEq)]
#[serde(rename_all = "camelCase")]
pub struct Runtime {
    pub state: State,
    #[serde(default)]
    pub mode: Option<String>,
    #[serde(default)]
    pub pid: Option<i64>,
    #[serde(default)]
    pub url: Option<String>,
}

#[derive(Default, Deserialize)]
struct Status {
    #[serde(default)]
    repositories: Vec<Found>,
    runtimes: BTreeMap<String, Runtime>,
}

#[derive(Deserialize)]
struct Found {
    repository: Option<Value>,
    error: Option<Failure>,
}

#[derive(Default)]
struct Snapshot {
    /// Library repository id to `{"info": discovery}` or `{"error": failure}`.
    repositories: BTreeMap<String, Value>,
    runtimes: BTreeMap<String, Runtime>,
    discovered: Option<Instant>,
    published: Option<Value>,
}

/// A development build's size and modification time; `None` while missing.
type Stamp = Option<(u64, SystemTime)>;

fn stamp(path: &str) -> Stamp {
    let metadata = std::fs::metadata(path).ok()?;
    Some((metadata.len(), metadata.modified().ok()?))
}

#[derive(Default)]
struct Build {
    stamp: Stamp,
    changed: bool,
}

/// Records each development build's stamp and returns the builds that changed
/// and then held still for a poll, so a build still being written waits. A
/// build seen for the first time is the one already running.
fn settled(builds: &mut BTreeMap<String, Build>, current: BTreeMap<String, Stamp>) -> Vec<String> {
    builds.retain(|path, _| current.contains_key(path));
    let mut ready = vec![];
    for (path, stamp) in current {
        let build = builds.entry(path.clone()).or_insert(Build {
            stamp,
            changed: false,
        });
        if build.stamp != stamp {
            build.stamp = stamp;
            build.changed = true;
        } else if build.changed && stamp.is_some() {
            build.changed = false;
            ready.push(path);
        }
    }
    ready
}

/// The app-wide view of repositories and runtimes. One loop polls it for every
/// window, so status polls never overlap and one bridge process serves a tick.
pub struct Presence {
    wake: Notify,
    discover: AtomicBool,
    snapshot: Mutex<Snapshot>,
    /// Development builds that discovered worktrees run, by executable path.
    builds: Mutex<BTreeMap<String, Build>>,
    /// Worktrees whose build changed while their runtime was starting, by
    /// path to repository id, replaced once they run.
    waiting: Mutex<BTreeMap<String, String>>,
}

impl Default for Presence {
    fn default() -> Self {
        Self {
            wake: Notify::new(),
            discover: AtomicBool::new(true),
            snapshot: Mutex::default(),
            builds: Mutex::default(),
            waiting: Mutex::default(),
        }
    }
}

impl Presence {
    /// Polls status soon. Requests during a poll coalesce into one more poll.
    pub fn refresh(&self) {
        self.wake.notify_one();
    }

    /// Rediscovers worktrees on the next poll, and polls soon.
    pub fn rediscover(&self) {
        self.discover.store(true, Ordering::SeqCst);
        self.refresh();
    }

    /// The last message sent to shells, for a shell that attaches later.
    pub fn published(&self) -> Option<Value> {
        self.snapshot.lock().unwrap().published.clone()
    }

    pub fn runtimes(&self) -> BTreeMap<String, Runtime> {
        self.snapshot.lock().unwrap().runtimes.clone()
    }

    async fn run<F: Future<Output = ()>>(&self, interval: Duration, mut poll: impl FnMut() -> F) {
        loop {
            poll().await;
            let _ = tokio::time::timeout(interval, self.wake.notified()).await;
        }
    }

    /// Running headless worktrees whose development build changed and
    /// settled since the last poll, as repository id and worktree path.
    fn rebuilt(&self) -> Vec<(String, String)> {
        let (uses, runtimes) = {
            let snapshot = self.snapshot.lock().unwrap();
            let mut uses = vec![];
            for (id, found) in &snapshot.repositories {
                for worktree in found["info"]["worktrees"].as_array().into_iter().flatten() {
                    if let (Some(path), Some(executable)) =
                        (worktree["path"].as_str(), worktree["executable"].as_str())
                    {
                        uses.push((executable.to_string(), id.clone(), path.to_string()));
                    }
                }
            }
            (uses, snapshot.runtimes.clone())
        };
        let current = uses.iter().map(|(e, _, _)| (e.clone(), stamp(e))).collect();
        let ready = settled(&mut self.builds.lock().unwrap(), current);
        let mut waiting = self.waiting.lock().unwrap();
        for (executable, id, path) in uses {
            if ready.contains(&executable) {
                waiting.insert(path, id);
            }
        }
        // A runtime still starting may have loaded the old build; it waits
        // until it runs. One that stopped will start on the new build.
        let mut rebuilt = vec![];
        waiting.retain(
            |path, id| match runtimes.get(path).map(|r| (r.state, r.mode.as_deref())) {
                Some((State::Starting, _)) => true,
                Some((State::Running, Some("headless"))) => {
                    rebuilt.push((id.clone(), path.clone()));
                    false
                }
                _ => false,
            },
        );
        rebuilt
    }

    fn discovery_due(&self) -> bool {
        let stale = self
            .snapshot
            .lock()
            .unwrap()
            .discovered
            .map_or(true, |at| at.elapsed() >= DISCOVERY);
        self.discover.swap(false, Ordering::SeqCst) || stale
    }

    /// Worktrees of saved repositories from the last discovery.
    fn worktrees(&self, library: &Library) -> BTreeSet<String> {
        let snapshot = self.snapshot.lock().unwrap();
        library
            .repositories
            .iter()
            .filter_map(|r| snapshot.repositories.get(&r.id))
            .filter_map(|found| found["info"]["worktrees"].as_array())
            .flatten()
            .filter_map(|w| w["path"].as_str().map(String::from))
            .collect()
    }

    fn record(&self, library: &Library, discovered: bool, status: Status) {
        let mut snapshot = self.snapshot.lock().unwrap();
        if discovered {
            snapshot.repositories = library
                .repositories
                .iter()
                .zip(status.repositories)
                .map(|(repository, found)| {
                    let entry = match (found.repository, found.error) {
                        (Some(info), _) => match discovery(&info) {
                            Ok(_) => json!({ "info": info }),
                            Err(failure) => json!({ "error": failure }),
                        },
                        (None, error) => json!({ "error": error.unwrap_or_else(|| {
                            Failure::from("Rhizome returned no repository.".to_string())
                        }) }),
                    };
                    (repository.id.clone(), entry)
                })
                .collect();
            snapshot.discovered = Some(Instant::now());
        }
        snapshot.runtimes = status.runtimes;
    }

    /// Returns the shell message when it differs from the last one sent. A
    /// worktree some window is opening shows as starting until it runs.
    fn changed(&self, opening: &BTreeSet<String>) -> Option<Value> {
        let mut snapshot = self.snapshot.lock().unwrap();
        let mut runtimes = serde_json::Map::new();
        for (path, runtime) in &snapshot.runtimes {
            let state = match runtime.state {
                State::Running => "running",
                State::Starting => "starting",
                State::Stopped if opening.contains(path) => "starting",
                State::Stopped => "stopped",
            };
            runtimes.insert(
                path.clone(),
                json!({ "state": state, "mode": runtime.mode }),
            );
        }
        for path in opening {
            runtimes
                .entry(path.clone())
                .or_insert_with(|| json!({ "state": "starting", "mode": null }));
        }
        let message = json!({
            "type": "presence",
            "repositories": snapshot.repositories,
            "runtimes": runtimes,
        });
        if snapshot.published.as_ref() == Some(&message) {
            return None;
        }
        snapshot.published = Some(message.clone());
        Some(message)
    }
}

/// Starts the app-wide presence loop: every few seconds while a window is
/// visible, and soon after a focus, library change, or completed open.
pub fn start(app: AppHandle) {
    tauri::async_runtime::spawn(async move {
        let presence = app.state::<Presence>();
        presence.run(POLL, || poll(&app)).await;
    });
}

/// Sends changed presence to every shell.
pub fn publish(app: &AppHandle) {
    let panes = app.state::<Panes>();
    if let Some(message) = app.state::<Presence>().changed(&panes.opening()) {
        panes.broadcast(&message);
    }
}

async fn poll(app: &AppHandle) {
    let visible = app
        .windows()
        .values()
        .any(|w| w.is_visible().unwrap_or(false) && !w.is_minimized().unwrap_or(false));
    if !visible {
        return;
    }
    let presence = app.state::<Presence>();
    let desktop = app.state::<Desktop>();
    let Ok(library) = desktop.library().await.map(|(_, library)| library) else {
        return;
    };
    let discover = presence.discovery_due();
    let repositories: Vec<RepositoryRef> = if discover {
        library
            .repositories
            .iter()
            .map(|r| RepositoryRef {
                folder: r.root.clone(),
                primary: r.primary.clone(),
            })
            .collect()
    } else {
        vec![]
    };
    let mut folders = app.state::<Panes>().displayed();
    if !discover {
        folders.extend(presence.worktrees(&library));
    }
    let folders: Vec<String> = folders.into_iter().collect();
    let status = if repositories.is_empty() && folders.is_empty() {
        Ok(Status::default())
    } else {
        bridge::call(
            &desktop.directory,
            Request {
                operation: "status",
                folders: Some(&folders),
                repositories: Some(&repositories),
                ..Request::default()
            },
            TIMEOUT,
        )
        .await
        .and_then(|value| {
            serde_json::from_value::<Status>(value)
                .map_err(|_| Failure::from("Rhizome returned an invalid status.".to_string()))
        })
    };
    let Ok(status) = status else {
        if discover {
            presence.discover.store(true, Ordering::SeqCst);
        }
        return;
    };
    // Entries saved under another identity, such as a version 1 folder that
    // was unavailable when the library migrated, merge once they resolve.
    let resolved: Vec<Discovery> = library
        .repositories
        .iter()
        .zip(&status.repositories)
        .filter_map(|(repository, found)| {
            let found = discovery(found.repository.as_ref()?).ok()?;
            (found.id != repository.id).then_some(found)
        })
        .collect();
    presence.record(&library, discover, status);
    publish(app);
    let runtimes = presence.runtimes();
    for (window, action) in app.state::<Panes>().supervise(&runtimes) {
        match action {
            Supervise::Relaunch(generation) => pipeline::relaunch(app, &window, generation),
            Supervise::Follow(generation, url, pid) => {
                pipeline::follow(app, &window, generation, url, pid)
            }
            Supervise::GaveUp(generation) => pipeline::abandon(app, &window, generation),
        }
    }
    for (id, worktree) in presence.rebuilt() {
        let app = app.clone();
        tauri::async_runtime::spawn(async move {
            let name = worktree.clone();
            if let Err(failure) = pipeline::restart(app.clone(), id, worktree, false).await {
                app.state::<Panes>().broadcast(&json!({
                    "type": "alert",
                    "code": failure.code,
                    "message": format!("Could not restart {name} on its new build: {}", failure.message),
                }));
            }
        });
    }
    if !resolved.is_empty() {
        commands::adopt(app, &resolved).await;
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::state::Repository;
    use std::sync::{
        atomic::{AtomicUsize, Ordering::SeqCst},
        Arc,
    };

    fn running(origin: &str) -> Runtime {
        Runtime {
            state: State::Running,
            mode: Some("headless".into()),
            pid: Some(4),
            url: Some(origin.into()),
        }
    }

    #[test]
    fn shells_hear_only_changes() {
        let presence = Presence::default();
        let none = BTreeSet::new();
        let library = Library {
            repositories: vec![Repository {
                id: "a".repeat(64),
                root: "/work/project/.git".into(),
                ..Repository::default()
            }],
            ..Library::default()
        };
        let status = |state| Status {
            repositories: vec![Found {
                repository: Some(json!({
                    "id": "a".repeat(64), "root": "/work/project/.git", "name": "project",
                    "primary": "/work/project",
                    "worktrees": [{"path": "/work/project"}, {"path": "/work/feature"}],
                })),
                error: None,
            }],
            runtimes: BTreeMap::from([
                ("/work/project".into(), state),
                ("/work/feature".into(), running("http://127.0.0.1:2/")),
            ]),
        };
        presence.record(&library, true, status(running("http://127.0.0.1:1/")));
        assert_eq!(
            presence.worktrees(&library),
            BTreeSet::from(["/work/feature".into(), "/work/project".into()])
        );
        let first = presence.changed(&none).unwrap();
        assert_eq!(first["runtimes"]["/work/project"]["state"], "running");
        assert!(first["repositories"]["a".repeat(64)]["info"].is_object());
        assert!(
            first.to_string().find("127.0.0.1").is_none(),
            "shells receive state, not addresses"
        );
        assert_eq!(presence.changed(&none), None);

        presence.record(&library, false, status(running("http://127.0.0.1:9/")));
        assert_eq!(
            presence.changed(&none),
            None,
            "a new address alone is not shown"
        );

        let stopped = Runtime {
            state: State::Stopped,
            mode: None,
            pid: None,
            url: None,
        };
        presence.record(&library, false, status(stopped));
        let opening = BTreeSet::from(["/work/project".to_string(), "/work/new".to_string()]);
        let message = presence.changed(&opening).unwrap();
        assert_eq!(message["runtimes"]["/work/project"]["state"], "starting");
        assert_eq!(message["runtimes"]["/work/new"]["state"], "starting");
        let message = presence.changed(&none).unwrap();
        assert_eq!(message["runtimes"]["/work/project"]["state"], "stopped");
        assert_eq!(presence.published(), Some(message));
    }

    #[test]
    fn a_changed_build_restarts_once_it_holds_still() {
        let mut builds = BTreeMap::new();
        let at = |n: u64| Some((n, SystemTime::UNIX_EPOCH + Duration::from_secs(n)));
        let poll = |builds: &mut BTreeMap<String, Build>, stamp: Stamp| {
            settled(builds, BTreeMap::from([("/rzm".to_string(), stamp)]))
        };
        assert!(
            poll(&mut builds, at(1)).is_empty(),
            "first sight is the running build"
        );
        assert!(poll(&mut builds, at(1)).is_empty());
        assert!(poll(&mut builds, at(2)).is_empty(), "still being written");
        assert!(poll(&mut builds, None).is_empty(), "replaced mid-build");
        assert!(poll(&mut builds, at(3)).is_empty());
        assert_eq!(poll(&mut builds, at(3)), vec!["/rzm".to_string()]);
        assert!(
            poll(&mut builds, at(3)).is_empty(),
            "restarts once per build"
        );
        assert!(settled(&mut builds, BTreeMap::new()).is_empty());
        assert!(builds.is_empty(), "forgets builds no worktree uses");
    }

    #[test]
    fn a_rebuilt_runtime_still_starting_is_restarted_once_it_runs() {
        let directory = tempfile::tempdir().unwrap();
        let executable = directory.path().join("rzm");
        std::fs::write(&executable, b"old").unwrap();
        let presence = Presence::default();
        let set = |state| {
            let mut snapshot = presence.snapshot.lock().unwrap();
            snapshot.repositories = BTreeMap::from([(
                "a".repeat(64),
                json!({"info": {"worktrees": [{"path": "/w", "executable": executable}]}}),
            )]);
            snapshot.runtimes = BTreeMap::from([(
                "/w".to_string(),
                Runtime {
                    state,
                    mode: Some("headless".into()),
                    pid: Some(1),
                    url: None,
                },
            )]);
        };
        set(State::Running);
        assert!(
            presence.rebuilt().is_empty(),
            "first sight is the running build"
        );
        std::fs::write(&executable, b"new build").unwrap();
        assert!(
            presence.rebuilt().is_empty(),
            "waits for the build to hold still"
        );
        set(State::Starting);
        assert!(
            presence.rebuilt().is_empty(),
            "a starting runtime is not replaced yet"
        );
        set(State::Running);
        assert_eq!(presence.rebuilt(), vec![("a".repeat(64), "/w".to_string())]);
        assert!(presence.rebuilt().is_empty());
    }

    #[test]
    fn a_shared_build_restarts_each_running_worktree_once() {
        let directory = tempfile::tempdir().unwrap();
        let executable = directory.path().join("rzm");
        std::fs::write(&executable, b"old").unwrap();
        let presence = Presence::default();
        let set = |second| {
            let runtime = |state| Runtime {
                state,
                mode: Some("headless".into()),
                pid: Some(1),
                url: None,
            };
            let mut snapshot = presence.snapshot.lock().unwrap();
            snapshot.repositories = BTreeMap::from([(
                "a".repeat(64),
                json!({"info": {"worktrees": [
                    {"path": "/a", "executable": executable},
                    {"path": "/b", "executable": executable},
                ]}}),
            )]);
            snapshot.runtimes = BTreeMap::from([
                ("/a".to_string(), runtime(State::Running)),
                ("/b".to_string(), runtime(second)),
            ]);
        };
        set(State::Running);
        presence.rebuilt();
        std::fs::write(&executable, b"new build").unwrap();
        presence.rebuilt();
        set(State::Starting);
        let id = "a".repeat(64);
        assert_eq!(presence.rebuilt(), vec![(id.clone(), "/a".to_string())]);
        assert!(
            presence.rebuilt().is_empty(),
            "the running one is not reopened"
        );
        set(State::Running);
        assert_eq!(presence.rebuilt(), vec![(id, "/b".to_string())]);
    }

    #[test]
    fn polls_never_overlap_and_requests_coalesce() {
        let presence = Arc::new(Presence::default());
        let (active, peak, polls) = (
            Arc::new(AtomicUsize::new(0)),
            Arc::new(AtomicUsize::new(0)),
            Arc::new(AtomicUsize::new(0)),
        );
        for _ in 0..5 {
            presence.refresh();
        }
        let (a, p, n) = (active.clone(), peak.clone(), polls.clone());
        let looped = presence.clone();
        tauri::async_runtime::block_on(async move {
            let poller = tokio::time::timeout(
                Duration::from_millis(300),
                looped.run(Duration::from_secs(3600), move || {
                    let (a, p, n) = (a.clone(), p.clone(), n.clone());
                    async move {
                        p.fetch_max(a.fetch_add(1, SeqCst) + 1, SeqCst);
                        n.fetch_add(1, SeqCst);
                        tokio::time::sleep(Duration::from_millis(20)).await;
                        a.fetch_sub(1, SeqCst);
                    }
                }),
            );
            let waker = presence.clone();
            let wakes = tauri::async_runtime::spawn(async move {
                tokio::time::sleep(Duration::from_millis(100)).await;
                for _ in 0..50 {
                    waker.refresh();
                    tokio::time::sleep(Duration::from_millis(2)).await;
                }
            });
            let _ = poller.await;
            let _ = wakes.await;
        });
        assert_eq!(peak.load(SeqCst), 1);
        let polls = polls.load(SeqCst);
        // Initial poll plus one coalesced follow-up before the wakes, then at
        // most one poll per 20 ms during ~100 ms of rapid wakes.
        assert!((4..=12).contains(&polls), "{polls}");
    }
}
