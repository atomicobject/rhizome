use crate::{
    presence::{Runtime, State},
    security,
};
use serde_json::{json, Value};
use std::{
    collections::{BTreeMap, BTreeSet, HashMap},
    sync::{Arc, Mutex, RwLock},
};
use tauri::ipc::Channel;
use url::Url;

/// Consecutive relaunches without a completed page load before the pane gives
/// up. Counting attempts rather than a time window still stops a runtime whose
/// every attempt takes as long as the page-load timeout.
const RELAUNCHES: u8 = 3;
/// Consecutive stopped observations before relaunching, so a runtime being
/// replaced by `rzm start` is not raced by a new headless one.
const STOPPED_POLLS: u8 = 2;

/// The verified runtime a pane displays.
#[derive(Clone, Debug, PartialEq)]
pub struct Shown {
    pub pid: i64,
    pub origin: Url,
}

/// One window's content state. Every open starts a new generation; work from
/// an older generation can no longer report progress or navigate the content.
#[derive(Default)]
pub struct Pane {
    generation: u64,
    /// The newest user selection begun, numbered by the shell when the user
    /// made it, so a selection that reaches the pane late cannot win.
    selection: u64,
    started: u64,
    repository: String,
    worktree: String,
    pending: Option<u64>,
    /// The pending load's own navigation began, so a finished load is it and
    /// not an earlier page of the same origin completing late.
    navigated: bool,
    /// A load that outlived its timeout; its page may still finish.
    overdue: Option<u64>,
    ready: bool,
    covered: bool,
    opening: bool,
    /// Why the app, not the user, began the current generation.
    reconnecting: Option<&'static str>,
    /// Set while the pane displays a runtime the app keeps available.
    supervised: Option<Shown>,
    stopped: u8,
    /// Relaunches since the last completed page load.
    failures: u8,
    /// The runtime origin whose page last finished loading in the content view.
    page: Option<Url>,
    /// The content view's current top-level document, from its last page load.
    pub document: Option<Url>,
    /// The current document reported page state, so the toolbar shows its controls.
    pub reported: bool,
    pub expected: Arc<RwLock<Option<Url>>>,
    pub channel: Option<Channel<Value>>,
    /// A message for a shell that has not attached yet.
    held: Option<Value>,
}

pub enum Load {
    Stale,
    Ready,
    Navigate,
}

/// What the supervisor does for a displayed worktree after a status poll. Each
/// action names the generation it was computed for and is dropped when a
/// newer open or a deselection replaced that generation before it runs.
#[derive(Debug, PartialEq)]
pub enum Supervise {
    Relaunch(u64),
    Follow(u64, Url, i64),
    GaveUp(u64),
}

impl Pane {
    /// A shell attached, as after its webview loaded; it numbers selections
    /// from the start again.
    pub fn attach(&mut self, channel: Channel<Value>) {
        self.channel = Some(channel);
        self.selection = 0;
        if let Some(message) = self.held.take() {
            self.send(message);
        }
    }

    /// Sends a message now, or when the shell attaches, as after a launch.
    pub fn deliver(&mut self, message: Value) {
        match &self.channel {
            Some(_) => self.send(message),
            None => self.held = Some(message),
        }
    }

    /// Starts the generation for a user's selection. Returns `None` when a
    /// newer selection already began, else the generation and whether the
    /// content view holds another worktree's page and must be cleared. A
    /// user's open ends supervision and its relaunch history.
    pub fn select(
        &mut self,
        selection: u64,
        started: u64,
        repository: &str,
        worktree: &str,
    ) -> Option<(u64, bool)> {
        if selection < self.selection {
            return None;
        }
        self.selection = selection;
        let blank = self.worktree != worktree && self.unload();
        self.supervised = None;
        self.failures = 0;
        Some((self.restart(started, repository, worktree, None), blank))
    }

    /// Starts the app's own generation for the target `generation` displayed,
    /// keeping supervision. `reason` names why the shell sees a reconnect;
    /// `None` reopens quietly, as after a stop that failed. Returns `None`
    /// when that generation was replaced.
    pub fn resume(
        &mut self,
        generation: u64,
        started: u64,
        reason: Option<&'static str>,
    ) -> Option<u64> {
        if !self.current(generation) || self.worktree.is_empty() {
            return None;
        }
        let (repository, worktree) = self.target();
        Some(self.restart(started, &repository, &worktree, reason))
    }

    fn restart(
        &mut self,
        started: u64,
        repository: &str,
        worktree: &str,
        reconnecting: Option<&'static str>,
    ) -> u64 {
        self.generation += 1;
        self.started = started;
        self.repository = repository.into();
        self.worktree = worktree.into();
        self.pending = None;
        self.overdue = None;
        self.ready = false;
        self.opening = true;
        self.stopped = 0;
        self.reconnecting = reconnecting;
        self.generation
    }

    /// Clears the target for a selection without a worktree. Returns whether
    /// it applied and the caller must clear the content view.
    pub fn deselect(&mut self, selection: u64) -> bool {
        if selection < self.selection {
            return false;
        }
        self.selection = selection;
        self.repository.clear();
        self.worktree.clear();
        self.idle();
        self.unload();
        true
    }

    /// Starts a generation with nothing opening, loading, or supervised.
    fn idle(&mut self) {
        self.generation += 1;
        self.pending = None;
        self.overdue = None;
        self.ready = false;
        self.opening = false;
        self.reconnecting = None;
        self.supervised = None;
        self.stopped = 0;
        self.failures = 0;
    }

    /// Puts a pane targeting `worktree` to sleep after the user stopped its
    /// runtime: a new generation the supervisor leaves alone, so nothing
    /// relaunches the runtime until the user opens the worktree again. Returns
    /// `None` for a pane targeting another worktree, else the sleeping
    /// generation and whether the caller must clear the content view.
    pub fn sleep(&mut self, worktree: &str) -> Option<(u64, bool)> {
        if self.worktree != worktree {
            return None;
        }
        self.idle();
        let cleared = self.unload();
        self.report(self.generation, "sleeping", json!({}));
        Some((self.generation, cleared))
    }

    /// Whether the content view holds a loaded page of `worktree`'s runtime.
    pub fn shows(&self, worktree: &str) -> bool {
        self.worktree == worktree && self.ready
    }

    /// Forgets the content view's page and allowed origin. Returns whether the
    /// view may hold a runtime page, which the caller then navigates away from
    /// so its event streams stop keeping that runtime alive.
    fn unload(&mut self) -> bool {
        self.page = None;
        self.expected.write().unwrap().take().is_some()
    }

    /// Ends the open steps. `keep` retains supervision: the open displayed a
    /// runtime, or a reconnect attempt failed and will be retried. Returns
    /// whether the content view must be cleared because nothing is displayed.
    pub fn settle(&mut self, generation: u64, keep: bool) -> bool {
        if !self.current(generation) {
            return false;
        }
        self.opening = false;
        if keep {
            return false;
        }
        self.supervised = None;
        self.unload()
    }

    pub fn target(&self) -> (String, String) {
        (self.repository.clone(), self.worktree.clone())
    }

    /// The generation displaying or loading `worktree`'s runtime. An open still
    /// under way waits on the worktree's lock and reaches the new runtime itself.
    pub fn showing(&self, worktree: &str) -> Option<u64> {
        (self.worktree == worktree && self.supervised.is_some() && !self.opening)
            .then_some(self.generation)
    }

    /// Opening or loading; the supervisor leaves such a pane alone.
    pub fn busy(&self) -> bool {
        self.opening || self.pending.is_some()
    }

    /// Decides how to keep a displayed worktree available after a status poll.
    /// `elsewhere` means another window is opening the same worktree.
    pub fn observe(&mut self, status: Option<&Runtime>, elsewhere: bool) -> Option<Supervise> {
        if self.busy() || self.supervised.is_none() {
            return None;
        }
        let state = status.map(|s| s.state);
        if state != Some(State::Stopped) || elsewhere {
            self.stopped = 0;
        }
        match state {
            Some(State::Running) => {
                let runtime = status?;
                let origin = security::runtime_url(runtime.url.as_deref()?).ok()?;
                let pid = runtime.pid?;
                let shown = self.supervised.as_mut()?;
                if self.ready && security::same_origin(&shown.origin, &origin) {
                    // The web UI reconnects its own streams to a replacement.
                    shown.pid = pid;
                    return None;
                }
                if !self.ready && !self.allow_relaunch() {
                    return Some(self.give_up());
                }
                Some(Supervise::Follow(self.generation, origin, pid))
            }
            Some(State::Stopped) if !elsewhere => {
                self.stopped += 1;
                if self.stopped < STOPPED_POLLS {
                    return None;
                }
                if !self.allow_relaunch() {
                    return Some(self.give_up());
                }
                self.stopped = 0;
                Some(Supervise::Relaunch(self.generation))
            }
            _ => None,
        }
    }

    fn allow_relaunch(&mut self) -> bool {
        if self.failures >= RELAUNCHES {
            return false;
        }
        self.failures += 1;
        true
    }

    /// Stops supervising after repeated failures. The view keeps its page and
    /// allowed origin until the caller runs `abandon`, so a newer selection
    /// made in between still clears the page itself.
    fn give_up(&mut self) -> Supervise {
        self.supervised = None;
        self.reconnecting = None;
        self.ready = false;
        self.report(
            self.generation,
            "error",
            json!({
                "code": "runtime_stopped",
                "message": "Rhizome stopped and did not stay running after 3 restarts. Check the worktree’s Rhizome log, then try again.",
            }),
        );
        Supervise::GaveUp(self.generation)
    }

    /// Unloads the page `generation` gave up on. Returns whether the caller
    /// must clear the content view; a newer generation owns the view instead.
    pub fn abandon(&mut self, generation: u64) -> bool {
        self.current(generation) && self.unload()
    }

    pub fn current(&self, generation: u64) -> bool {
        self.generation == generation
    }

    /// Records the verified runtime origin. A page of that origin that finished
    /// loading, from a runtime that was not restarted, is ready without
    /// reloading; anything else, including an earlier load that failed or
    /// never finished, navigates again.
    pub fn expect(&mut self, generation: u64, url: &Url, pid: i64, restarted: bool) -> Load {
        if !self.current(generation) {
            return Load::Stale;
        }
        self.supervised = Some(Shown {
            pid,
            origin: url.clone(),
        });
        *self.expected.write().unwrap() = Some(url.clone());
        let reuse = !restarted
            && self
                .page
                .as_ref()
                .is_some_and(|page| security::same_origin(page, url));
        if reuse {
            self.ready = true;
            self.failures = 0;
            Load::Ready
        } else {
            self.page = None;
            self.pending = Some(generation);
            self.navigated = false;
            Load::Navigate
        }
    }

    /// Records that the content view began loading a page of the expected
    /// runtime, after the pending load's navigation was issued.
    pub fn navigation_started(&mut self, url: &Url) {
        let expected = self.expected.read().unwrap();
        let loading = self.pending.is_some() || self.overdue == Some(self.generation);
        if loading
            && expected
                .as_ref()
                .is_some_and(|o| security::same_origin(o, url))
        {
            self.navigated = true;
        }
    }

    /// Completes the pending load when the page that finished is the runtime's.
    /// A load that already timed out still completes its own generation.
    pub fn loaded(&mut self, url: &Url) -> Option<u64> {
        let generation = self
            .pending
            .or(self.overdue.filter(|g| *g == self.generation))?;
        let origin = self.expected.read().unwrap().clone()?;
        if !self.navigated || !security::same_origin(&origin, url) {
            return None;
        }
        self.pending = None;
        self.overdue = None;
        self.ready = true;
        self.failures = 0;
        self.page = Some(origin);
        Some(generation)
    }
    pub fn timed_out(&mut self, generation: u64) -> bool {
        let expired = self.pending == Some(generation);
        if expired {
            self.pending = None;
            self.overdue = Some(generation);
        }
        expired
    }

    pub fn cover(&mut self, covered: bool) {
        self.covered = covered;
    }

    pub fn visible(&self) -> bool {
        self.ready && !self.covered
    }

    pub fn send(&self, message: Value) {
        if let Some(channel) = &self.channel {
            let _ = channel.send(message);
        }
    }

    /// Reports an open step to the shell unless a newer open replaced it.
    pub fn report(&self, generation: u64, step: &str, detail: Value) {
        if !self.current(generation) {
            return;
        }
        let mut message = json!({
            "type": "open",
            "generation": generation,
            "started": self.started,
            "repository": self.repository,
            "worktree": self.worktree,
            "step": step,
        });
        if let Some(reason) = self.reconnecting {
            message["reconnecting"] = reason.into();
        }
        if let (Some(message), Value::Object(detail)) = (message.as_object_mut(), detail) {
            message.extend(detail);
        }
        self.send(message);
    }
}

#[derive(Default)]
pub struct Panes(Mutex<HashMap<String, Pane>>);

impl Panes {
    pub fn with<T>(&self, window: &str, f: impl FnOnce(&mut Pane) -> T) -> Option<T> {
        self.0.lock().unwrap().get_mut(window).map(f)
    }

    pub fn insert(&self, window: String, pane: Pane) {
        self.0.lock().unwrap().insert(window, pane);
    }

    pub fn deliver(&self, window: &str, message: Value) {
        self.with(window, |pane| pane.deliver(message));
    }

    pub fn remove(&self, window: &str) {
        self.0.lock().unwrap().remove(window);
    }

    pub fn broadcast(&self, message: &Value) {
        for pane in self.0.lock().unwrap().values() {
            pane.send(message.clone());
        }
    }

    /// Puts every pane targeting `worktree` to sleep. Returns each window with
    /// its sleeping generation and whether its content view must be cleared.
    pub fn sleep(&self, worktree: &str) -> Vec<(String, u64, bool)> {
        self.0
            .lock()
            .unwrap()
            .iter_mut()
            .filter_map(|(window, pane)| {
                let (generation, cleared) = pane.sleep(worktree)?;
                Some((window.clone(), generation, cleared))
            })
            .collect()
    }

    /// Windows displaying `worktree`'s runtime, with their generations.
    pub fn showing(&self, worktree: &str) -> Vec<(String, u64)> {
        self.0
            .lock()
            .unwrap()
            .iter()
            .filter_map(|(window, pane)| Some((window.clone(), pane.showing(worktree)?)))
            .collect()
    }

    /// Worktrees some window is opening or loading.
    pub fn opening(&self) -> BTreeSet<String> {
        busy(&self.0.lock().unwrap())
    }

    /// Worktrees some window shows or is opening; their status is always polled.
    pub fn displayed(&self) -> BTreeSet<String> {
        self.0
            .lock()
            .unwrap()
            .values()
            .filter(|p| !p.worktree.is_empty())
            .map(|p| p.worktree.clone())
            .collect()
    }

    pub fn supervise(&self, runtimes: &BTreeMap<String, Runtime>) -> Vec<(String, Supervise)> {
        let mut panes = self.0.lock().unwrap();
        let busy = busy(&panes);
        panes
            .iter_mut()
            .filter_map(|(window, pane)| {
                let elsewhere = busy.contains(&pane.worktree);
                pane.observe(runtimes.get(&pane.worktree), elsewhere)
                    .map(|action| (window.clone(), action))
            })
            .collect()
    }
}

fn busy(panes: &HashMap<String, Pane>) -> BTreeSet<String> {
    panes
        .values()
        .filter(|p| p.busy())
        .map(|p| p.worktree.clone())
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn url(raw: &str) -> Url {
        Url::parse(raw).unwrap()
    }

    /// The content view starts and finishes loading `page`.
    fn finish(pane: &mut Pane, page: &Url) -> Option<u64> {
        pane.navigation_started(page);
        pane.loaded(page)
    }

    /// Starts a user's open as the shell's `selection`th choice.
    fn open(pane: &mut Pane, selection: u64, worktree: &str) -> u64 {
        pane.select(selection, 1, "r", worktree).unwrap().0
    }

    #[test]
    fn a_restart_reloads_loading_panes_but_leaves_opens_to_finish() {
        let mut pane = Pane::default();
        let generation = open(&mut pane, 1, "/w");
        assert_eq!(
            pane.showing("/w"),
            None,
            "the open reaches the new runtime itself"
        );
        pane.expect(generation, &url("http://127.0.0.1:1111/"), 1, true);
        pane.settle(generation, true);
        assert_eq!(
            pane.showing("/w"),
            Some(generation),
            "still loading the old page"
        );
        assert_eq!(pane.showing("/other"), None);
    }

    #[test]
    fn a_late_finish_of_the_old_page_does_not_complete_the_new_load() {
        let mut pane = Pane::default();
        let origin = url("http://127.0.0.1:1111/");
        let first = open(&mut pane, 1, "/w");
        pane.expect(first, &origin, 1, true);
        assert_eq!(finish(&mut pane, &origin), Some(first));
        let generation = pane.resume(first, 2, Some("moved")).unwrap();
        pane.expect(generation, &origin, 2, true);
        assert_eq!(
            pane.loaded(&origin),
            None,
            "the old page finished before the reload began"
        );
        assert!(!pane.visible());
        assert_eq!(finish(&mut pane, &origin), Some(generation));
    }

    #[test]
    fn a_newer_open_discards_the_slower_earlier_one() {
        let mut pane = Pane::default();
        let first = open(&mut pane, 1, "/w");
        let second = open(&mut pane, 2, "/w");
        assert!(!pane.current(first));
        assert!(matches!(
            pane.expect(first, &url("http://127.0.0.1:1111/"), 1, true),
            Load::Stale
        ));
        assert_eq!(*pane.expected.read().unwrap(), None);
        assert!(matches!(
            pane.expect(second, &url("http://127.0.0.1:2222/"), 1, true),
            Load::Navigate
        ));
        assert_eq!(finish(&mut pane, &url("http://127.0.0.1:1111/")), None);
        assert!(!pane.visible());
        assert_eq!(
            finish(&mut pane, &url("http://127.0.0.1:2222/notes")),
            Some(second)
        );
        assert!(pane.visible());
    }

    #[test]
    fn a_selection_that_arrives_after_a_newer_one_is_discarded() {
        let mut pane = Pane::default();
        let newer = open(&mut pane, 7, "/b");
        assert_eq!(pane.select(6, 2, "r", "/a"), None);
        assert!(!pane.deselect(5));
        assert!(pane.current(newer));
        assert_eq!(pane.target().1, "/b");
        let (_, cleared) = pane.select(7, 3, "r", "/a").unwrap();
        assert!(!cleared, "nothing was loaded yet");
    }

    #[test]
    fn content_stays_hidden_until_loaded_and_while_covered() {
        let mut pane = Pane::default();
        let generation = open(&mut pane, 1, "/w");
        pane.expect(generation, &url("http://127.0.0.1:1111/"), 1, true);
        assert!(!pane.visible());
        assert_eq!(finish(&mut pane, &url("about:blank")), None);
        pane.cover(true);
        finish(&mut pane, &url("http://127.0.0.1:1111/"));
        assert!(!pane.visible());
        pane.cover(false);
        assert!(pane.visible());
        open(&mut pane, 2, "/w");
        assert!(!pane.visible());
    }

    #[test]
    fn same_runtime_is_reused_unless_it_restarted() {
        let mut pane = Pane::default();
        let runtime = url("http://127.0.0.1:1111/");
        let first = open(&mut pane, 1, "/w");
        pane.expect(first, &runtime, 1, false);
        finish(&mut pane, &runtime);
        let again = open(&mut pane, 2, "/w");
        assert!(matches!(
            pane.expect(again, &runtime, 1, false),
            Load::Ready
        ));
        assert!(pane.visible());
        let restarted = open(&mut pane, 3, "/w");
        assert!(matches!(
            pane.expect(restarted, &runtime, 1, true),
            Load::Navigate
        ));
        assert!(pane.timed_out(restarted));
        assert!(!pane.timed_out(restarted));
    }

    #[test]
    fn a_page_that_finishes_after_its_timeout_still_shows() {
        let mut pane = Pane::default();
        let runtime = url("http://127.0.0.1:1111/");
        let first = open(&mut pane, 1, "/w");
        pane.expect(first, &runtime, 1, true);
        assert!(pane.timed_out(first));
        assert_eq!(finish(&mut pane, &runtime), Some(first));
        assert!(pane.visible());
        let next = open(&mut pane, 2, "/w");
        pane.expect(next, &runtime, 1, true);
        assert!(pane.timed_out(next));
        open(&mut pane, 3, "/other");
        assert_eq!(
            finish(&mut pane, &runtime),
            None,
            "a newer open owns the view"
        );
    }

    #[test]
    fn a_retry_after_an_unfinished_load_navigates_again() {
        let mut pane = Pane::default();
        let runtime = url("http://127.0.0.1:1111/");
        let first = open(&mut pane, 1, "/w");
        assert!(matches!(
            pane.expect(first, &runtime, 1, false),
            Load::Navigate
        ));
        assert!(pane.timed_out(first));
        let retry = open(&mut pane, 2, "/w");
        assert!(matches!(
            pane.expect(retry, &runtime, 1, false),
            Load::Navigate
        ));
        assert!(!pane.visible());
    }

    fn runtime(state: State, origin: &str, pid: i64) -> Runtime {
        Runtime {
            state,
            mode: Some("headless".into()),
            pid: Some(pid),
            url: Some(origin.into()),
        }
    }

    /// A pane showing `origin` from runtime pid 1, as after a completed open.
    fn displayed(origin: &str) -> Pane {
        let mut pane = Pane::default();
        let generation = open(&mut pane, 1, "/w");
        pane.expect(generation, &url(origin), 1, true);
        pane.settle(generation, true);
        finish(&mut pane, &url(origin));
        pane
    }

    const A: &str = "http://127.0.0.1:1111/";
    const B: &str = "http://127.0.0.1:2222/";

    /// Relaunches after two stopped polls, then fails without loading a page.
    fn fail_relaunch(pane: &mut Pane, stopped: &Runtime) {
        assert_eq!(pane.observe(Some(stopped), false), None);
        let Some(Supervise::Relaunch(generation)) = pane.observe(Some(stopped), false) else {
            panic!("expected a relaunch");
        };
        let generation = pane.resume(generation, 2, Some("stopped")).unwrap();
        assert_eq!(pane.observe(Some(stopped), false), None, "busy");
        pane.settle(generation, true);
    }

    #[test]
    fn three_consecutive_failed_relaunches_give_up() {
        let mut pane = displayed(A);
        let stopped = runtime(State::Stopped, A, 0);
        for _ in 0..3 {
            fail_relaunch(&mut pane, &stopped);
        }
        pane.observe(Some(&stopped), false);
        let gave_up = Some(Supervise::GaveUp(pane.generation));
        assert_eq!(pane.observe(Some(&stopped), false), gave_up);
        assert!(pane.reconnecting.is_none());
        assert!(!pane.visible());
        assert!(pane.abandon(pane.generation));
        assert_eq!(*pane.expected.read().unwrap(), None, "the view is cleared");
        assert_eq!(pane.observe(Some(&stopped), false), None);
        assert_eq!(pane.observe(Some(&stopped), false), None);
    }

    #[test]
    fn a_completed_load_resets_the_relaunch_count() {
        let mut pane = displayed(A);
        let stopped = runtime(State::Stopped, A, 0);
        fail_relaunch(&mut pane, &stopped);
        fail_relaunch(&mut pane, &stopped);
        pane.observe(Some(&stopped), false);
        let Some(Supervise::Relaunch(generation)) = pane.observe(Some(&stopped), false) else {
            panic!("expected a relaunch");
        };
        let generation = pane.resume(generation, 3, Some("stopped")).unwrap();
        pane.expect(generation, &url(B), 2, true);
        pane.settle(generation, true);
        finish(&mut pane, &url(B));
        for _ in 0..3 {
            fail_relaunch(&mut pane, &stopped);
        }
    }

    #[test]
    fn a_replaced_runtime_is_followed_only_when_its_origin_changes() {
        let mut pane = displayed(A);
        assert_eq!(
            pane.observe(Some(&runtime(State::Running, A, 7)), false),
            None
        );
        assert_eq!(pane.supervised.as_ref().unwrap().pid, 7);
        assert!(pane.visible());
        assert_eq!(
            pane.observe(Some(&runtime(State::Running, B, 8)), false),
            Some(Supervise::Follow(pane.generation, url(B), 8))
        );
        assert_eq!(pane.failures, 0, "following a ready page is not a retry");
    }

    #[test]
    fn a_failed_reconnect_follows_a_runtime_started_elsewhere() {
        let mut pane = displayed(A);
        let generation = pane.resume(pane.generation, 2, Some("stopped")).unwrap();
        pane.settle(generation, true);
        assert_eq!(
            pane.observe(Some(&runtime(State::Running, A, 9)), false),
            Some(Supervise::Follow(generation, url(A), 9))
        );
    }

    #[test]
    fn supervisor_actions_are_dropped_after_a_newer_selection() {
        let mut pane = displayed(A);
        let Some(Supervise::Follow(follow, ..)) =
            pane.observe(Some(&runtime(State::Running, B, 8)), false)
        else {
            panic!("expected a follow");
        };
        let (_, cleared) = pane.select(2, 2, "other", "/other").unwrap();
        assert!(cleared, "the other worktree's page leaves the view");
        assert_eq!(pane.resume(follow, 3, Some("moved")), None);
        assert_eq!(pane.target(), ("other".into(), "/other".into()));

        let mut pane = displayed(A);
        let stopped = runtime(State::Stopped, A, 0);
        pane.observe(Some(&stopped), false);
        let Some(Supervise::Relaunch(relaunch)) = pane.observe(Some(&stopped), false) else {
            panic!("expected a relaunch");
        };
        assert!(pane.deselect(2));
        assert_eq!(pane.resume(relaunch, 3, Some("stopped")), None);
    }

    #[test]
    fn a_stale_give_up_leaves_a_newer_selection_displayed() {
        let mut pane = displayed(A);
        let stopped = runtime(State::Stopped, A, 0);
        for _ in 0..3 {
            fail_relaunch(&mut pane, &stopped);
        }
        pane.observe(Some(&stopped), false);
        let Some(Supervise::GaveUp(gave_up)) = pane.observe(Some(&stopped), false) else {
            panic!("expected to give up");
        };
        let (other, cleared) = pane.select(2, 2, "other", "/other").unwrap();
        assert!(cleared, "the abandoned page leaves the view");
        pane.expect(other, &url(B), 2, true);
        finish(&mut pane, &url(B));
        assert!(!pane.abandon(gave_up), "the newer selection owns the view");
        assert_eq!(*pane.expected.read().unwrap(), Some(url(B)));
        assert!(pane.visible());
    }

    #[test]
    fn a_sleeping_worktree_is_not_relaunched_until_opened_again() {
        let panes = Panes::default();
        panes.insert("window-1".into(), displayed(A));
        panes.insert("window-2".into(), displayed(A));
        let mut other = Pane::default();
        open(&mut other, 1, "/other");
        panes.insert("window-3".into(), other);
        let mut cleared: Vec<_> = panes
            .sleep("/w")
            .into_iter()
            .filter_map(|(window, _, cleared)| cleared.then_some(window))
            .collect();
        cleared.sort();
        assert_eq!(cleared, ["window-1", "window-2"]);
        let stopped = BTreeMap::from([("/w".to_string(), runtime(State::Stopped, A, 0))]);
        for _ in 0..3 {
            assert!(panes.supervise(&stopped).is_empty());
        }
        panes.with("window-1", |pane| {
            assert!(!pane.visible());
            assert_eq!(pane.target().1, "/w", "the selection stays");
            let generation = open(pane, 2, "/w");
            pane.expect(generation, &url(A), 2, true);
            pane.settle(generation, true);
            finish(pane, &url(A));
            assert!(pane.visible(), "opening again wakes it");
        });
    }

    #[test]
    fn sleep_fences_an_open_in_progress_and_a_failed_stop_wakes_quietly() {
        let mut pane = Pane::default();
        let opening = open(&mut pane, 1, "/w");
        let (sleeping, _) = pane.sleep("/w").unwrap();
        assert!(matches!(
            pane.expect(opening, &url(A), 1, false),
            Load::Stale
        ));
        assert!(!pane.busy(), "the supervisor and other windows see no open");

        let woken = pane.resume(sleeping, 2, None).unwrap();
        assert!(pane.reconnecting.is_none(), "shown as an ordinary open");
        pane.expect(woken, &url(A), 1, false);
        finish(&mut pane, &url(A));
        assert!(pane.visible());
    }

    #[test]
    fn deselecting_stops_supervision_and_clears_the_view() {
        let panes = Panes::default();
        panes.insert("window-1".into(), displayed(A));
        assert_eq!(panes.with("window-1", |p| p.deselect(2)), Some(true));
        assert!(panes.displayed().is_empty());
        let stopped = BTreeMap::from([("/w".to_string(), runtime(State::Stopped, A, 0))]);
        for _ in 0..3 {
            assert!(panes.supervise(&stopped).is_empty());
        }
        panes.with("window-1", |pane| {
            assert_eq!(*pane.expected.read().unwrap(), None);
            assert!(!pane.visible());
        });
    }

    #[test]
    fn an_open_that_displays_nothing_clears_the_view() {
        let mut pane = displayed(A);
        let generation = open(&mut pane, 2, "/w");
        assert!(pane.settle(generation, false), "trust or setup is required");
        assert_eq!(*pane.expected.read().unwrap(), None);
        let generation = open(&mut pane, 3, "/w");
        assert!(!pane.settle(generation, false), "nothing left to clear");
    }

    #[test]
    fn only_displayed_idle_worktrees_are_relaunched() {
        let stopped = runtime(State::Stopped, A, 0);
        let mut idle = Pane::default();
        let mut errored = Pane::default();
        let generation = open(&mut errored, 1, "/w");
        errored.settle(generation, false);
        let mut starting = displayed(A);
        let mut opened_elsewhere = displayed(A);
        for _ in 0..3 {
            assert_eq!(idle.observe(Some(&stopped), false), None);
            assert_eq!(errored.observe(Some(&stopped), false), None);
            assert_eq!(opened_elsewhere.observe(Some(&stopped), true), None);
            assert_eq!(
                starting.observe(Some(&runtime(State::Starting, A, 0)), false),
                None
            );
            assert_eq!(starting.observe(None, false), None);
        }
    }

    #[test]
    fn panes_report_the_worktrees_they_are_opening() {
        let panes = Panes::default();
        let mut opening = Pane::default();
        open(&mut opening, 1, "/opening");
        panes.insert("window-1".into(), opening);
        let shown = displayed(A);
        let generation = shown.generation;
        panes.insert("window-2".into(), shown);
        assert_eq!(panes.opening(), BTreeSet::from(["/opening".to_string()]));
        assert_eq!(
            panes.displayed(),
            BTreeSet::from(["/opening".to_string(), "/w".to_string()])
        );
        let runtimes = BTreeMap::from([("/w".to_string(), runtime(State::Running, B, 3))]);
        assert_eq!(
            panes.supervise(&runtimes),
            vec![(
                "window-2".to_string(),
                Supervise::Follow(generation, url(B), 3)
            )]
        );
    }
}
