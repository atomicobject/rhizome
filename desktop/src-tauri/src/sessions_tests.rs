use super::*;

fn at(x: f64) -> Geometry {
    Geometry {
        x,
        y: 60.0,
        width: 1000.0,
        height: 700.0,
    }
}

fn worktree(path: &str) -> WindowSession {
    WindowSession {
        worktree: Some(path.into()),
        ..WindowSession::default()
    }
}

fn worktrees(windows: &[WindowSession]) -> Vec<Option<&str>> {
    windows.iter().map(|w| w.worktree.as_deref()).collect()
}

#[test]
fn failed_update_allows_later_window_closes_and_session_changes() {
    let directory = tempfile::tempdir().unwrap();
    let sessions = Sessions::new(directory.path().into());
    sessions.apply("window-1", |saved| saved.worktree = Some("/w/one".into()));
    sessions.apply("window-2", |saved| saved.worktree = Some("/w/two".into()));
    sessions.quit([]);
    sessions.dirty.store(true, Ordering::SeqCst);
    sessions.cancel_quit();
    assert!(
        !sessions.dirty.load(Ordering::SeqCst),
        "a save deferred during installation must not suppress later saves"
    );
    sessions.close("window-1", None);
    sessions.apply("window-2", |saved| saved.width = Some(900.0));
    sessions.save();
    let saved = Session::load(directory.path()).windows;
    assert_eq!(saved.len(), 1);
    assert_eq!(saved[0].worktree.as_deref(), Some("/w/two"));
    assert_eq!(saved[0].width, Some(900.0));
}

#[test]
fn quitting_saves_geometry_of_windows_that_never_moved() {
    let directory = tempfile::tempdir().unwrap();
    let sessions = Sessions::new(directory.path().into());
    sessions.apply("window-1", |saved| saved.worktree = Some("/w/one".into()));
    sessions.apply("window-2", |_| {});
    sessions.quit([("window-1".to_string(), at(40.0))]);
    sessions.close("window-1", None);
    let saved = Session::load(directory.path()).windows;
    assert_eq!(saved.len(), 2, "quitting keeps every window");
    assert_eq!(
        saved[0],
        WindowSession {
            x: Some(40.0),
            y: Some(60.0),
            width: Some(1000.0),
            height: Some(700.0),
            worktree: Some("/w/one".into()),
            ..WindowSession::default()
        }
    );
}

#[test]
fn closing_the_last_window_keeps_its_layout_for_the_next_one() {
    let directory = tempfile::tempdir().unwrap();
    let sessions = Sessions::new(directory.path().into());
    sessions.apply("window-1", |saved| saved.worktree = Some("/w/one".into()));
    sessions.apply("window-2", |saved| saved.worktree = Some("/w/two".into()));
    let geometry = Geometry {
        x: 1.0,
        y: 2.0,
        width: 900.0,
        height: 600.0,
    };
    sessions.close("window-1", Some(geometry));
    assert_eq!(sessions.last_closed(), WindowSession::default());
    sessions.close("window-2", Some(geometry));
    let session = Session::load(directory.path());
    assert!(session.windows.is_empty());
    let closed = session.closed.unwrap();
    assert_eq!(closed.worktree.as_deref(), Some("/w/two"));
    assert_eq!((closed.width, closed.height), (Some(900.0), Some(600.0)));
    assert_eq!(sessions.last_closed(), closed);
}

#[test]
fn quit_rhizome_stashes_every_window_and_one_resume_restores_them_all() {
    let directory = tempfile::tempdir().unwrap();
    let sessions = Sessions::new(directory.path().into());
    sessions.apply("window-1", |saved| saved.worktree = Some("/w/one".into()));
    sessions.apply("window-2", |saved| saved.worktree = Some("/w/two".into()));
    sessions.stash([("window-2".to_string(), at(300.0))]);
    // Closing the stashed windows afterwards changes nothing.
    sessions.close("window-1", Some(at(5.0)));
    sessions.close("window-2", Some(at(5.0)));
    assert_eq!(sessions.last_closed(), WindowSession::default());
    let saved = Session::load(directory.path());
    assert!(saved.pending, "no window is open while the session waits");
    assert_eq!(worktrees(&saved.windows), [Some("/w/one"), Some("/w/two")]);
    assert_eq!(saved.windows[1].x, Some(300.0));

    let resumed = sessions.unstash();
    assert_eq!(worktrees(&resumed), [Some("/w/one"), Some("/w/two")]);
    assert!(sessions.unstash().is_empty(), "one resume restores them");
    // Resumed windows open again under new labels.
    for (label, window) in ["window-3", "window-4"].iter().zip(resumed) {
        sessions.apply(label, |saved| *saved = window);
    }
    sessions.save();
    let saved = Session::load(directory.path());
    assert!(!saved.pending);
    assert_eq!(worktrees(&saved.windows), [Some("/w/one"), Some("/w/two")]);
}

#[test]
fn quit_rhizome_is_not_quitting_so_later_closes_still_count() {
    let directory = tempfile::tempdir().unwrap();
    let sessions = Sessions::new(directory.path().into());
    sessions.apply("window-1", |saved| saved.worktree = Some("/w/one".into()));
    sessions.stash([]);
    assert!(!sessions.quitting.load(Ordering::SeqCst));
    let resumed = sessions.unstash();
    sessions.apply("window-2", |saved| *saved = resumed[0].clone());
    sessions.apply("window-3", |saved| saved.worktree = Some("/w/new".into()));
    sessions.close("window-3", None);
    let saved = Session::load(directory.path()).windows;
    assert_eq!(worktrees(&saved), [Some("/w/one")]);
}

#[test]
fn a_complete_quit_before_the_next_open_keeps_the_stashed_layout() {
    let directory = tempfile::tempdir().unwrap();
    let sessions = Sessions::new(directory.path().into());
    sessions.apply("window-1", |saved| saved.worktree = Some("/w/one".into()));
    sessions.apply("window-2", |saved| saved.worktree = Some("/w/two".into()));
    sessions.stash([]);
    // Quit Completely, logout, or an update with no window open.
    sessions.quit([]);
    let saved = Session::load(directory.path());
    assert!(saved.pending);
    assert_eq!(worktrees(&saved.windows), [Some("/w/one"), Some("/w/two")]);
}

#[test]
fn a_launch_at_login_opens_nothing_and_keeps_the_saved_layout() {
    let directory = tempfile::tempdir().unwrap();
    let sessions = Sessions::new(directory.path().into());
    let session = Session {
        windows: vec![worktree("/w/one"), worktree("/w/two")],
        closed: Some(worktree("/w/closed")),
        pending: false,
    };
    assert!(sessions.begin(session, Launch::Login).is_empty());
    sessions.quit([]);
    let saved = Session::load(directory.path());
    assert_eq!(worktrees(&saved.windows), [Some("/w/one"), Some("/w/two")]);
    assert_eq!(saved.closed, Some(worktree("/w/closed")));
    assert!(saved.pending);
    assert_eq!(
        worktrees(&sessions.unstash()),
        [Some("/w/one"), Some("/w/two")]
    );
}

#[test]
fn an_updater_restart_reopens_open_windows_but_never_forces_one() {
    let open = Session {
        windows: vec![worktree("/w/one")],
        closed: None,
        pending: false,
    };
    let stashed = Session {
        windows: vec![worktree("/w/one")],
        closed: None,
        pending: true,
    };
    let none = Session {
        windows: vec![],
        closed: Some(worktree("/w/closed")),
        pending: false,
    };
    assert!(Launch::Restarted.opens(&open));
    assert!(!Launch::Restarted.opens(&stashed));
    assert!(!Launch::Restarted.opens(&none));

    let directory = tempfile::tempdir().unwrap();
    let sessions = Sessions::new(directory.path().into());
    assert!(sessions.begin(none, Launch::Restarted).is_empty());
    assert_eq!(sessions.last_closed(), worktree("/w/closed"));
    assert!(sessions.unstash().is_empty());
}

#[test]
fn an_ordinary_launch_restores_every_window_or_the_last_one_closed() {
    let directory = tempfile::tempdir().unwrap();
    let sessions = Sessions::new(directory.path().into());
    let stashed = Session {
        windows: vec![worktree("/w/one"), worktree("/w/two")],
        closed: None,
        pending: true,
    };
    assert_eq!(
        worktrees(&sessions.begin(stashed, Launch::Open)),
        [Some("/w/one"), Some("/w/two")]
    );
    let none = Session {
        windows: vec![],
        closed: Some(worktree("/w/closed")),
        pending: false,
    };
    assert_eq!(
        worktrees(&sessions.begin(none, Launch::Open)),
        [Some("/w/closed")]
    );
}
