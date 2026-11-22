use super::*;
use crate::host::api::{HostStatus, LastFailure, Next, Running};
use chrono::TimeZone;

fn now() -> DateTime<Local> {
    Local.with_ymd_and_hms(2026, 11, 22, 14, 30, 0).unwrap()
}

fn at(day: u32, hour: u32) -> String {
    Local
        .with_ymd_and_hms(2026, 11, day, hour, 0, 0)
        .unwrap()
        .to_rfc3339()
}

fn texts(model: &Model) -> Vec<String> {
    model
        .entries
        .iter()
        .map(|entry| match entry {
            Entry::Text(text) => text.clone(),
            Entry::Item { id, label } => format!("[{label}] {id}"),
            Entry::Submenu { label, items } => format!("{label} ▸ {}", items.len()),
            Entry::Separator => "—".into(),
        })
        .collect()
}

fn view(name: &str, status: Option<HostStatus>) -> View {
    View {
        key: format!("/work/{name}/.git"),
        name: name.into(),
        status,
        problem: None,
    }
}

#[test]
fn says_when_the_next_task_runs_in_local_time() {
    assert_eq!(when(&at(22, 17), now()), "at 5:00 PM");
    assert_eq!(when(&at(23, 5), now()), "tomorrow at 5:00 AM");
    assert_eq!(when(&at(26, 9), now()), "Thursday at 9:00 AM");
    assert_eq!(when(&at(30, 9), now()), "Nov 30 at 9:00 AM");
    assert_eq!(when(&at(22, 9), now()), "now", "a due task runs now");
    assert_eq!(when("soon", now()), "soon");
    let until = tomorrow(now()).unwrap();
    assert_eq!(when(&until, now()), "tomorrow at 12:00 AM");
}

#[test]
fn each_repository_shows_its_next_task_running_task_inbox_and_last_failure() {
    let busy = HostStatus {
        run_here: true,
        running: Some(Running {
            task: "digest".into(),
            name: Some("Daily digest".into()),
            stage: Some("agent".into()),
            ..Running::default()
        }),
        next: Some(Next {
            task: "inbox".into(),
            name: None,
            at: at(23, 5),
        }),
        attention: 2,
        last_failure: Some(LastFailure {
            task: "sync".into(),
            name: Some("Sync".into()),
            message: "push rejected".into(),
            ..LastFailure::default()
        }),
        ..HostStatus::default()
    };
    let idle = HostStatus {
        run_here: true,
        next: Some(Next {
            task: "weekly".into(),
            name: Some("Weekly review".into()),
            at: at(22, 17),
        }),
        attention: 1,
        ..HostStatus::default()
    };
    let model = model(
        &[view("notes", Some(busy)), view("site", Some(idle))],
        None,
        true,
        false,
        now(),
    );
    assert_eq!(
        texts(&model),
        [
            "notes",
            "● Running Daily digest (agent)",
            "Next: inbox tomorrow at 5:00 AM",
            "2 items need you",
            "Last failure: Sync: push rejected",
            "—",
            "site",
            "Next: Weekly review at 5:00 PM",
            "1 item needs you",
            "[Run Weekly review Now] tray:run:0",
            "—",
            "Pause Tasks ▸ 2",
            "[Open Rhizome] tray:open",
            "—",
            "[Quit Rhizome Completely] tray:quit",
        ]
    );
    assert_eq!(
        model.runs,
        [("/work/site/.git".to_string(), "weekly".to_string())]
    );
    assert_eq!(model.title.as_deref(), Some("3"));
}

#[test]
fn paused_draining_and_unreachable_hosts_say_so() {
    let paused = HostStatus {
        run_here: true,
        paused: true,
        paused_until: Some(at(23, 0)),
        ..HostStatus::default()
    };
    let draining = HostStatus {
        run_here: true,
        draining: true,
        next: Some(Next {
            task: "inbox".into(),
            name: None,
            at: at(22, 17),
        }),
        ..HostStatus::default()
    };
    let mut broken = view("broken", None);
    broken.problem = Some("Confirm trust for this folder.".into());
    let model = model(
        &[
            view("notes", Some(paused)),
            view("site", Some(draining)),
            broken,
            view("new", None),
        ],
        None,
        false,
        false,
        now(),
    );
    let texts = texts(&model);
    assert!(texts.contains(&"Paused until tomorrow at 12:00 AM".to_string()));
    assert!(texts.contains(&"Starting no new tasks".to_string()));
    assert!(texts.contains(&"Confirm trust for this folder.".to_string()));
    assert!(texts.contains(&"Connecting…".to_string()));
    assert!(model.runs.is_empty(), "a draining runtime starts nothing");
    assert!(texts.contains(&"[Resume Tasks] tray:resume".to_string()));
    // With "Keep running in the menu bar" off there is one quit.
    assert_eq!(texts.last().unwrap(), "[Quit Rhizome] tray:quit");
    assert_eq!(model.title, None);
}

#[test]
fn an_empty_or_failed_listing_and_a_pending_quit_are_shown() {
    let none = texts(&model(&[], None, true, false, now()));
    assert_eq!(none[0], "No repository runs tasks on this Mac");
    assert!(!none.iter().any(|t| t.starts_with("Pause")));
    let failed = texts(&model(&[], Some("Update Rhizome."), true, false, now()));
    assert_eq!(failed[0], "Update Rhizome.");
    let quitting = texts(&model(&[view("notes", None)], None, true, true, now()));
    assert!(quitting.contains(&"Quitting when the running task finishes".to_string()));
    let long = texts(&model(&[], Some(&"x".repeat(100)), true, false, now()));
    assert_eq!(long[0].chars().count(), 81);
    // Nothing to pause until a runtime reports.
    assert!(!quitting.iter().any(|t| t.starts_with("Pause")));
}
