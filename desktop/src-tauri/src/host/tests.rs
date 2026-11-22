use super::*;
use crate::state::Repository;
use api::Notice;

fn listed(dir: &str, checkout: &str, run_here: bool) -> Listed {
    Listed {
        common_dir: dir.into(),
        checkout: checkout.into(),
        run_here,
    }
}

fn library() -> Library {
    Library {
        repositories: vec![Repository {
            id: "a".repeat(64),
            root: "/work/notes/.git".into(),
            name: "notes".into(),
            ..Repository::default()
        }],
        ..Library::default()
    }
}

fn status(attention: u32, notices: &[&str]) -> HostStatus {
    HostStatus {
        run_here: true,
        attention,
        notices: notices
            .iter()
            .map(|id| Notice {
                id: id.to_string(),
                title: format!("Notice {id}"),
                ..Notice::default()
            })
            .collect(),
        ..HostStatus::default()
    }
}

#[test]
fn hosts_follow_the_listing_and_name_their_repository() {
    let host = Host::default();
    let due = host.reconcile(
        &[
            listed("/work/notes/.git", "/work/notes", true),
            listed("/work/site/.git", "/work/site", true),
            listed("/work/off/.git", "/work/off", false),
        ],
        &library(),
    );
    assert_eq!(
        due,
        [
            ("/work/notes/.git".to_string(), "/work/notes".to_string()),
            ("/work/site/.git".to_string(), "/work/site".to_string())
        ]
    );
    let views = host.views();
    assert_eq!(views[0].name, "notes", "the library names it");
    assert_eq!(views[1].name, "site", "else its checkout does");

    // A repository no longer listed, or listed off, is dropped.
    host.reconcile(
        &[
            listed("/work/notes/.git", "/work/notes", true),
            listed("/work/site/.git", "/work/site", false),
        ],
        &library(),
    );
    assert_eq!(host.views().len(), 1);
    // A moved checkout is held anew.
    let due = host.reconcile(
        &[listed("/work/notes/.git", "/work/notes-host", true)],
        &library(),
    );
    assert_eq!(due[0].1, "/work/notes-host");
}

#[test]
fn a_failed_host_waits_before_it_is_tried_again() {
    let host = Host::default();
    let listing = [listed("/work/notes/.git", "/work/notes", true)];
    assert_eq!(host.reconcile(&listing, &library()).len(), 1);
    assert_eq!(
        host.failed("/work/notes/.git", None, "trust required".into()),
        Duration::from_secs(2)
    );
    assert!(host.reconcile(&listing, &library()).is_empty());
    assert_eq!(host.views()[0].problem.as_deref(), Some("trust required"));
    assert_eq!(backoff(2), Duration::from_secs(4));
    assert_eq!(backoff(30), Duration::from_secs(60));
}

#[test]
fn statuses_from_a_replaced_stream_are_ignored_and_run_here_off_drops_the_host() {
    let host = Host::default();
    host.reconcile(
        &[listed("/work/notes/.git", "/work/notes", true)],
        &library(),
    );
    host.hosts
        .lock()
        .unwrap()
        .get_mut("/work/notes/.git")
        .unwrap()
        .generation = 2;
    assert_eq!(host.record("/work/notes/.git", 1, status(1, &[])), None);
    assert!(host.views()[0].status.is_none());
    let notices = host
        .record("/work/notes/.git", 2, status(3, &["n1"]))
        .unwrap();
    assert_eq!(notices[0].id, "n1");
    assert_eq!(host.attention(), BTreeMap::from([("a".repeat(64), 3)]));
    // A stream that ends from an older generation changes nothing.
    host.failed("/work/notes/.git", Some(1), "closed".into());
    assert!(host.views()[0].status.is_some());

    let off = HostStatus {
        run_here: false,
        ..HostStatus::default()
    };
    assert_eq!(host.record("/work/notes/.git", 2, off), None);
    assert!(host.views().is_empty());
}

#[test]
fn an_unreadable_status_replaces_the_stale_one_until_a_status_arrives() {
    let host = Host::default();
    host.reconcile(
        &[listed("/work/notes/.git", "/work/notes", true)],
        &library(),
    );
    host.record("/work/notes/.git", 0, status(2, &[])).unwrap();
    host.unreadable("/work/notes/.git", 1, "older stream".into());
    assert!(
        host.views()[0].status.is_some(),
        "an older stream changes nothing"
    );
    host.unreadable(
        "/work/notes/.git",
        0,
        "Can't read task status: denied".into(),
    );
    let view = &host.views()[0];
    assert!(view.status.is_none());
    assert_eq!(
        view.problem.as_deref(),
        Some("Can't read task status: denied")
    );
    host.record("/work/notes/.git", 0, status(1, &[])).unwrap();
    assert_eq!(host.views()[0].problem, None);
}

#[test]
fn releasing_reports_what_each_host_held_and_ends_hosting() {
    let host = Host::default();
    host.reconcile(
        &[listed("/work/notes/.git", "/work/notes", true)],
        &library(),
    );
    {
        let mut hosts = host.hosts.lock().unwrap();
        let held = hosts.get_mut("/work/notes/.git").unwrap();
        held.headless = true;
        held.status = Some(HostStatus {
            running: Some(api::Running::default()),
            ..status(0, &[])
        });
    }
    assert_eq!(host.running(), [("notes".to_string(), String::new())]);
    let released = host.release();
    assert_eq!(released.len(), 1);
    assert!(released[0].headless && released[0].running);
    assert_eq!(released[0].checkout, "/work/notes");
    assert!(host.closing());
    assert!(host.views().is_empty());
    assert!(
        host.release().is_empty(),
        "a second release has nothing left"
    );
}

#[test]
fn each_notice_is_shown_once_even_after_a_relaunch() {
    let directory = tempfile::tempdir().unwrap();
    let notices = status(0, &["n1", "n2"]).notices;
    let mut notified = notices::Notified::load(directory.path());
    let ids = |list: Vec<Notice>| list.into_iter().map(|n| n.id).collect::<Vec<_>>();
    assert_eq!(ids(notified.fresh("/a/.git", &notices)), ["n1", "n2"]);
    assert!(notified.fresh("/a/.git", &notices).is_empty());
    // Notice ids belong to their repository.
    assert_eq!(ids(notified.fresh("/b/.git", &notices[..1])), ["n1"]);
    notified.save(directory.path());
    let mut relaunched = notices::Notified::load(directory.path());
    assert!(relaunched.fresh("/a/.git", &notices).is_empty());
    assert_eq!(
        ids(relaunched.fresh("/a/.git", &status(0, &["n3"]).notices)),
        ["n3"]
    );
}

#[test]
fn more_visible_notices_than_the_record_keeps_are_still_shown_once() {
    let directory = tempfile::tempdir().unwrap();
    let ids: Vec<String> = (0..600).map(|i| format!("n{i}")).collect();
    let visible = status(0, &ids.iter().map(String::as_str).collect::<Vec<_>>()).notices;
    let mut notified = notices::Notified::load(directory.path());
    let gone = status(0, &["gone"]).notices;
    let other = status(0, &["other"]).notices;
    notified.fresh("/a/.git", &gone);
    notified.fresh("/b/.git", &other);
    assert_eq!(notified.fresh("/a/.git", &visible).len(), 600);
    assert!(
        notified.fresh("/a/.git", &visible).is_empty(),
        "no visible notice repeats"
    );
    assert!(
        notified.fresh("/b/.git", &other).is_empty(),
        "another repository's notices are kept"
    );
    // What gets forgotten is this repository's ids no longer listed.
    assert_eq!(notified.fresh("/a/.git", &gone).len(), 1);
}

#[test]
fn hosts_reads_the_cli_listing() {
    let listing: Vec<Listed> = serde_json::from_str(
        r#"[{"commonDir":"/work/notes/.git","checkout":"/work/notes","runHere":true,"extra":1},
            {"commonDir":"/work/site/.git","checkout":"/work/site"}]"#,
    )
    .unwrap();
    assert!(listing[0].run_here);
    assert!(!listing[1].run_here);
}
