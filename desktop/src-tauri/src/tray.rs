//! The menu bar menu (SPEC-0104 US8): for each hosted repository, the next
//! task and when it runs, a running indicator, the inbox count, and the last
//! failure, all from the runtime's host stream, with Run now, Pause tasks,
//! Open Rhizome, and Quit Rhizome Completely. The model is plain data so it
//! can be tested; only macOS draws it.
// The menu bar host runs only on macOS; other platforms compile it for tests.
#![cfg_attr(not(target_os = "macos"), allow(dead_code))]
use crate::host::{api::label, View};
use chrono::{DateTime, Days, Local};

/// One line of the menu.
#[derive(Clone, Debug, PartialEq)]
pub enum Entry {
    Text(String),
    Item { id: String, label: String },
    Submenu { label: String, items: Vec<Entry> },
    Separator,
}

#[derive(Clone, Debug, Default, PartialEq)]
pub struct Model {
    pub entries: Vec<Entry>,
    /// The repository key and task each `tray:run:<n>` item runs.
    pub runs: Vec<(String, String)>,
    /// Shown beside the icon: the inbox count, when any.
    pub title: Option<String>,
}

const LONGEST: usize = 64;

fn short(text: &str) -> String {
    let text = text.split_whitespace().collect::<Vec<_>>().join(" ");
    match text.char_indices().nth(LONGEST) {
        Some((end, _)) => format!("{}…", &text[..end]),
        None => text,
    }
}

/// When an RFC 3339 time falls, in this Mac's time zone, as the menu says it.
pub fn when(at: &str, now: DateTime<Local>) -> String {
    let Ok(at) = DateTime::parse_from_rfc3339(at) else {
        return at.to_string();
    };
    let at = at.with_timezone(&Local);
    if at <= now {
        return "now".into();
    }
    let time = at.format("%-I:%M %p");
    let (day, today) = (at.date_naive(), now.date_naive());
    if day == today {
        format!("at {time}")
    } else if Some(day) == today.checked_add_days(Days::new(1)) {
        format!("tomorrow at {time}")
    } else if Some(day) < today.checked_add_days(Days::new(7)) {
        format!("{} at {time}", at.format("%A"))
    } else {
        format!("{} at {time}", at.format("%b %-d"))
    }
}

/// The menu for the hosts' latest statuses.
pub fn model(
    views: &[View],
    listing: Option<&str>,
    keep_in_menu_bar: bool,
    quitting: bool,
    now: DateTime<Local>,
) -> Model {
    let mut model = Model::default();
    let text = |s: String| Entry::Text(short(&s));
    if let Some(problem) = listing {
        model.entries.push(text(problem.to_string()));
    } else if views.is_empty() {
        model
            .entries
            .push(text("No repository runs tasks on this Mac".into()));
    }
    let mut attention = 0;
    let mut paused = false;
    for view in views {
        if !model.entries.is_empty() {
            model.entries.push(Entry::Separator);
        }
        model.entries.push(text(view.name.clone()));
        let Some(status) = &view.status else {
            let line = view.problem.clone().unwrap_or_else(|| "Connecting…".into());
            model.entries.push(text(line));
            continue;
        };
        attention += status.attention;
        paused |= status.paused;
        if let Some(running) = &status.running {
            let task = label(&running.name, &running.task);
            model.entries.push(text(match &running.stage {
                Some(stage) if !stage.is_empty() => format!("● Running {task} ({stage})"),
                _ => format!("● Running {task}"),
            }));
        }
        if status.paused {
            model.entries.push(text(match &status.paused_until {
                Some(until) => format!(
                    "Paused until {}",
                    when(until, now).trim_start_matches("at ")
                ),
                None => "Paused".into(),
            }));
        } else if status.draining {
            model.entries.push(text("Starting no new tasks".into()));
        }
        if let Some(next) = &status.next {
            let task = label(&next.name, &next.task);
            model
                .entries
                .push(text(format!("Next: {task} {}", when(&next.at, now))));
        }
        match status.attention {
            0 => {}
            1 => model.entries.push(text("1 item needs you".into())),
            n => model.entries.push(text(format!("{n} items need you"))),
        }
        if let Some(failure) = &status.last_failure {
            let task = label(&failure.name, &failure.task);
            model
                .entries
                .push(text(format!("Last failure: {task}: {}", failure.message)));
        }
        if let (Some(next), None, false) = (&status.next, &status.running, status.draining) {
            model.entries.push(Entry::Item {
                id: format!("tray:run:{}", model.runs.len()),
                label: short(&format!("Run {} Now", label(&next.name, &next.task))),
            });
            model.runs.push((view.key.clone(), next.task.clone()));
        }
    }
    model.entries.push(Entry::Separator);
    if quitting {
        model
            .entries
            .push(text("Quitting when the running task finishes".into()));
    } else if paused {
        model.entries.push(Entry::Item {
            id: "tray:resume".into(),
            label: "Resume Tasks".into(),
        });
    } else if !views.is_empty() {
        model.entries.push(Entry::Submenu {
            label: "Pause Tasks".into(),
            items: vec![
                Entry::Item {
                    id: "tray:pause-hour".into(),
                    label: "For 1 Hour".into(),
                },
                Entry::Item {
                    id: "tray:pause-tomorrow".into(),
                    label: "Until Tomorrow".into(),
                },
            ],
        });
    }
    model.entries.push(Entry::Item {
        id: "tray:open".into(),
        label: "Open Rhizome".into(),
    });
    model.entries.push(Entry::Separator);
    model.entries.push(Entry::Item {
        id: "tray:quit".into(),
        label: match keep_in_menu_bar {
            true => "Quit Rhizome Completely".into(),
            false => "Quit Rhizome".into(),
        },
    });
    model.title = (attention > 0).then(|| attention.to_string());
    model
}

/// When "Until Tomorrow" ends: the start of the next local day.
pub fn tomorrow(now: DateTime<Local>) -> Option<String> {
    let midnight = now
        .date_naive()
        .checked_add_days(Days::new(1))?
        .and_hms_opt(0, 0, 0)?;
    Some(midnight.and_local_timezone(Local).earliest()?.to_rfc3339())
}

#[cfg(target_os = "macos")]
#[path = "tray/native.rs"]
mod native;
#[cfg(target_os = "macos")]
pub use native::{install, refresh};

/// Other platforms have no menu bar host.
#[cfg(not(target_os = "macos"))]
pub fn refresh(_app: &tauri::AppHandle) {}

#[cfg(test)]
#[path = "tray/tests.rs"]
mod tests;
