//! The Dock badge and system notifications for scheduled-task items that need
//! the person (EFF-2026-10-31-14-31, phase 3). Each runtime page reports its
//! repository's items in its title report (page.rs); the app sums the counts
//! across repositories and notifies once per item and state while it runs.
use crate::{page, pane::Panes, windows};
use serde::{Deserialize, Serialize};
use std::{
    collections::{BTreeMap, HashMap, HashSet},
    sync::{
        atomic::{AtomicI32, Ordering},
        Mutex,
    },
};
use tauri::{AppHandle, Manager};
use tauri_plugin_notification::NotificationExt;

const ITEMS: usize = 50;
const FIELD: usize = 200;
const TEXT: usize = 500;

#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash, Deserialize)]
#[serde(rename_all = "kebab-case")]
pub enum State {
    Due,
    Ready,
    NeedsAttention,
}

impl State {
    fn label(self) -> &'static str {
        match self {
            State::Due => "Due",
            State::Ready => "Ready for review",
            State::NeedsAttention => "Needs attention",
        }
    }
}

#[derive(Clone, Debug, PartialEq, Deserialize)]
pub struct Item {
    pub id: String,
    pub task: String,
    #[serde(default)]
    pub name: Option<String>,
    pub state: State,
    /// The item's summary or reason, shortened by the page.
    #[serde(default)]
    pub text: String,
}

/// The page's open items that need the person, unsnoozed. The shell sees
/// only the count.
#[derive(Clone, Debug, PartialEq, Deserialize, Serialize)]
pub struct Tasks {
    pub needs: u32,
    #[serde(default, skip_serializing)]
    pub items: Vec<Item>,
}

impl Tasks {
    pub fn valid(&self) -> bool {
        let fits = |text: &str, limit: usize| text.chars().count() <= limit;
        self.items.len() <= ITEMS
            && self.items.len() <= self.needs as usize
            && self.items.iter().all(|item| {
                !item.id.is_empty()
                    && !item.task.is_empty()
                    && fits(&item.id, FIELD)
                    && fits(&item.task, FIELD)
                    && item.name.as_deref().map_or(true, |name| fits(name, FIELD))
                    && fits(&item.text, TEXT)
            })
    }
}

/// The badge count. Windows showing worktrees of one repository share its
/// task state, so each repository counts once, at the larger of its reports.
pub fn total<'a>(counts: impl IntoIterator<Item = (&'a str, u32)>) -> u32 {
    let mut repositories = BTreeMap::new();
    for (repository, needs) in counts {
        let count = repositories.entry(repository).or_insert(0);
        *count = needs.max(*count);
    }
    repositories.values().sum()
}

/// Every (repository, item, state) the app has seen while running.
#[derive(Default)]
pub struct Seen(HashSet<(String, String, State)>);

impl Seen {
    /// The items that newly need the person. A page's first report only
    /// records what it shows, so a window that opens notifies nothing.
    pub fn fresh<'a>(&mut self, repository: &str, items: &'a [Item], first: bool) -> Vec<&'a Item> {
        let mut fresh = Vec::new();
        for item in items {
            let new = self
                .0
                .insert((repository.to_owned(), item.id.clone(), item.state));
            if new && !first {
                fresh.push(item);
            }
        }
        fresh
    }
}

pub fn title(item: &Item) -> &str {
    item.name.as_deref().unwrap_or(&item.task)
}

pub fn body(item: &Item) -> String {
    match item.text.as_str() {
        "" => item.state.label().to_owned(),
        text => format!("{}: {text}", item.state.label()),
    }
}

/// Where a notification's click leads.
#[derive(Clone)]
struct Click {
    window: String,
    repository: String,
    task: String,
}

#[derive(Default)]
pub struct Inbox {
    seen: Mutex<Seen>,
    clicks: Mutex<HashMap<i32, Click>>,
    next: AtomicI32,
}

/// Opens the item's task page when the person clicks its notification. The
/// plugin reports clicks only on notifications shown after this is called.
pub fn listen(app: &AppHandle) {
    let handle = app.clone();
    let _ = app.notification().on_action(move |performed| {
        if performed.action_id() == "tap" {
            if let Some(notification) = performed.notification() {
                open(&handle, notification.id());
            }
        }
    });
}

/// Handles a window's new report: notifies for items that newly need the
/// person, then updates the badge.
pub fn reported(app: &AppHandle, window: &str) {
    let inbox = app.state::<Inbox>();
    let fresh = app
        .state::<Panes>()
        .with(window, |pane| {
            pane.tasks.as_ref()?;
            let first = !std::mem::replace(&mut pane.tasks_reported, true);
            let (repository, _) = pane.target();
            let items = &pane.tasks.as_ref()?.items;
            let fresh = inbox.seen.lock().unwrap().fresh(&repository, items, first);
            Some((repository, fresh.into_iter().cloned().collect::<Vec<_>>()))
        })
        .flatten();
    if let Some((repository, items)) = fresh {
        for item in items {
            let id = inbox.next.fetch_add(1, Ordering::Relaxed) + 1;
            inbox.clicks.lock().unwrap().insert(
                id,
                Click {
                    window: window.to_owned(),
                    repository: repository.clone(),
                    task: item.task.clone(),
                },
            );
            let _ = app
                .notification()
                .builder()
                .id(id)
                .title(title(&item))
                .body(body(&item))
                .show();
        }
    }
    refresh(app);
}

/// A window is closing; its count leaves the badge while a window remains to
/// set it.
pub fn closing(app: &AppHandle, window: &str) {
    app.state::<Panes>().with(window, |pane| pane.tasks = None);
    refresh(app);
}

/// Sets the Dock badge to the total across windows, clearing it at 0.
pub fn refresh(app: &AppHandle) {
    let counts = app.state::<Panes>().task_needs();
    let needs = total(counts.iter().map(|(r, n)| (r.as_str(), *n)));
    // The badge belongs to the app; any window sets it.
    if let Some(window) = app.windows().values().next() {
        let _ = window.set_badge_count((needs > 0).then_some(needs.into()));
    }
}

/// Focuses the notification's window and, when it still shows the item's
/// repository, opens the task's page.
fn open(app: &AppHandle, id: i32) {
    let Some(click) = app
        .state::<Inbox>()
        .clicks
        .lock()
        .unwrap()
        .get(&id)
        .cloned()
    else {
        return;
    };
    let handle = app.clone();
    let _ = app.run_on_main_thread(move || {
        let Some(window) = handle.get_window(&click.window) else {
            return;
        };
        let _ = window.unminimize();
        let _ = window.set_focus();
        let shown = handle
            .state::<Panes>()
            .with(&click.window, |pane| {
                pane.target().0 == click.repository && page::on_runtime(pane) && pane.reported
            })
            .unwrap_or(false);
        let content = window.get_webview(&windows::content_label(&click.window));
        let script = page::Command::Task { task: click.task }.script();
        if let (true, Some(content), Some(script)) = (shown, content, script) {
            let _ = content.eval(script);
        }
    });
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn item(id: &str, state: State) -> Item {
        Item {
            id: id.into(),
            task: "docs-drift".into(),
            name: None,
            state,
            text: String::new(),
        }
    }

    #[test]
    fn sums_each_repository_once() {
        assert_eq!(total([]), 0);
        assert_eq!(total([("a", 2), ("b", 3)]), 5);
        // Two worktrees of one repository share its task state.
        assert_eq!(total([("a", 2), ("a", 1), ("b", 0)]), 2);
        assert_eq!(total([("a", 1), ("a", 4)]), 4);
    }

    #[test]
    fn notifies_once_per_item_and_state_and_never_on_a_first_report() {
        let mut seen = Seen::default();
        let ids = |fresh: Vec<&Item>| fresh.iter().map(|i| i.id.clone()).collect::<Vec<_>>();
        let opened = [item("1", State::Due)];
        assert!(seen.fresh("a", &opened, true).is_empty());
        let later = [
            item("1", State::Due),
            item("2", State::Ready),
            item("3", State::NeedsAttention),
        ];
        assert_eq!(ids(seen.fresh("a", &later, false)), ["2", "3"]);
        assert!(seen.fresh("a", &later, false).is_empty());
        // A new state notifies again; an earlier one never does.
        let changed = [item("1", State::NeedsAttention), item("2", State::Ready)];
        assert_eq!(ids(seen.fresh("a", &changed, false)), ["1"]);
        assert!(seen.fresh("a", &[item("1", State::Due)], false).is_empty());
        // Item ids belong to their repository.
        assert_eq!(
            ids(seen.fresh("b", &[item("2", State::Ready)], false)),
            ["2"]
        );
        // A window opening later records what it shows without notifying.
        assert!(seen.fresh("c", &[item("9", State::Due)], true).is_empty());
        assert!(seen.fresh("c", &[item("9", State::Due)], false).is_empty());
    }

    #[test]
    fn titles_and_bodies_name_the_task_and_state() {
        let mut ready = item("1", State::Ready);
        assert_eq!(title(&ready), "docs-drift");
        assert_eq!(body(&ready), "Ready for review");
        ready.name = Some("Docs drift check".into());
        ready.text = "Three notes still reference…".into();
        assert_eq!(title(&ready), "Docs drift check");
        assert_eq!(
            body(&ready),
            "Ready for review: Three notes still reference…"
        );
    }

    #[test]
    fn reads_only_bounded_reports_and_shows_the_shell_only_the_count() {
        let tasks: Tasks = serde_json::from_value(json!({
            "needs": 3,
            "items": [{"id": "8", "task": "flaky", "name": "Flaky tests", "state": "needs-attention", "text": "go test failed"}],
        }))
        .unwrap();
        assert!(tasks.valid());
        assert_eq!(tasks.items[0].state, State::NeedsAttention);
        assert_eq!(serde_json::to_value(&tasks).unwrap(), json!({"needs": 3}));
        let many = Tasks {
            needs: 51,
            items: vec![item("1", State::Due); 51],
        };
        assert!(!many.valid());
        for bad in [
            Tasks {
                needs: 0,
                items: vec![item("1", State::Due)],
            },
            Tasks {
                needs: 1,
                items: vec![item("", State::Due)],
            },
            Tasks {
                needs: 1,
                items: vec![Item {
                    text: "a".repeat(501),
                    ..item("1", State::Due)
                }],
            },
        ] {
            assert!(!bad.valid());
        }
        for invalid in [
            json!({"needs": -1, "items": []}),
            json!({"needs": 1, "items": [{"id": "1", "task": "t", "state": "done"}]}),
        ] {
            assert!(serde_json::from_value::<Tasks>(invalid).is_err());
        }
    }
}
