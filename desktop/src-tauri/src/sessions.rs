//! The saved window session: each open window's layout and selection, the
//! windows Quit Rhizome stashed, and the last window closed (SPEC-0104 US8).
use crate::state::{Session, WindowSession};
use std::{
    collections::BTreeMap,
    path::PathBuf,
    sync::{
        atomic::{AtomicBool, Ordering},
        Mutex,
    },
    time::Duration,
};
use tauri::{AppHandle, Manager, Window};

/// Each open window's layout and selection, saved so a relaunch restores them.
/// Quitting keeps every window; only closing a window removes it. Quit
/// Rhizome stashes them instead, and the next window opener resumes them.
pub struct Sessions {
    directory: PathBuf,
    windows: Mutex<BTreeMap<u64, WindowSession>>,
    /// Windows to restore when the app next opens one: stashed by Quit
    /// Rhizome, or kept by a launch that opened none. Saved as the session
    /// until then, so quitting before that keeps them.
    stashed: Mutex<Vec<WindowSession>>,
    closed: Mutex<Option<WindowSession>>,
    /// Held from snapshot to write, so a delayed save cannot replace a newer
    /// snapshot, such as one written when a window closed.
    writing: Mutex<()>,
    quitting: AtomicBool,
    dirty: AtomicBool,
}

pub fn number(window: &str) -> Option<u64> {
    window.strip_prefix("window-")?.parse().ok()
}

/// How the app was started.
#[cfg_attr(not(target_os = "macos"), allow(dead_code))]
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Launch {
    /// By the person: restore the session, or open one window.
    Open,
    /// At login: menu bar only; the session waits for Open Rhizome.
    Login,
    /// By the updater: reopen windows that were open, but never force one.
    Restarted,
}

impl Launch {
    /// Whether this launch opens windows from `session`.
    pub fn opens(self, session: &Session) -> bool {
        match self {
            Launch::Open => true,
            Launch::Login => false,
            Launch::Restarted => !session.pending && !session.windows.is_empty(),
        }
    }
}

impl Sessions {
    pub fn new(directory: PathBuf) -> Self {
        Self {
            directory,
            windows: Mutex::default(),
            stashed: Mutex::default(),
            closed: Mutex::default(),
            writing: Mutex::default(),
            quitting: AtomicBool::new(false),
            dirty: AtomicBool::new(false),
        }
    }

    /// Takes the saved session at launch and returns the windows to open.
    /// A launch that opens none keeps the session's windows pending.
    pub fn begin(&self, session: Session, launch: Launch) -> Vec<WindowSession> {
        let opens = launch.opens(&session);
        *self.closed.lock().unwrap() = session.closed.clone();
        if !opens {
            *self.stashed.lock().unwrap() = session.windows;
            return vec![];
        }
        match session.windows.is_empty() {
            true => vec![session.closed.unwrap_or_default()],
            false => session.windows,
        }
    }

    pub fn get(&self, window: &str) -> WindowSession {
        number(window)
            .and_then(|n| self.windows.lock().unwrap().get(&n).cloned())
            .unwrap_or_default()
    }

    pub fn update(&self, app: &AppHandle, window: &str, f: impl FnOnce(&mut WindowSession)) {
        if self.apply(window, f) {
            self.schedule(app);
        }
    }

    fn apply(&self, window: &str, f: impl FnOnce(&mut WindowSession)) -> bool {
        let Some(n) = number(window) else {
            return false;
        };
        f(self.windows.lock().unwrap().entry(n).or_default());
        true
    }

    /// Forgets a closed window. The last one is kept, with its geometry, for
    /// the next window opened without a saved layout.
    pub fn close(&self, window: &str, geometry: Option<Geometry>) {
        if self.quitting.load(Ordering::SeqCst) {
            return;
        }
        let Some(n) = number(window) else {
            return;
        };
        let mut windows = self.windows.lock().unwrap();
        if let Some(mut saved) = windows.remove(&n) {
            if windows.is_empty() {
                if let Some(geometry) = geometry {
                    geometry.apply(&mut saved);
                }
                *self.closed.lock().unwrap() = Some(saved);
            }
        }
        drop(windows);
        self.save();
    }

    /// The layout for a window opened when none remain.
    pub fn last_closed(&self) -> WindowSession {
        self.closed.lock().unwrap().clone().unwrap_or_default()
    }

    /// Saves every open window, with its current geometry, before exit.
    /// Stashed windows stay in the session too.
    pub fn quit(&self, geometry: impl IntoIterator<Item = (String, Geometry)>) {
        self.quitting.store(true, Ordering::SeqCst);
        for (window, geometry) in geometry {
            self.apply(&window, |saved| geometry.apply(saved));
        }
        self.save();
    }

    pub fn cancel_quit(&self) {
        self.quitting.store(false, Ordering::SeqCst);
        self.save();
    }

    /// Quit Rhizome: saves every open window, with its geometry, as the
    /// session to resume, and stops tracking them as open, so closing them
    /// next keeps them. The app is not quitting.
    #[cfg_attr(not(target_os = "macos"), allow(dead_code))]
    pub fn stash(&self, geometry: impl IntoIterator<Item = (String, Geometry)>) {
        for (window, geometry) in geometry {
            self.apply(&window, |saved| geometry.apply(saved));
        }
        let open = std::mem::take(&mut *self.windows.lock().unwrap());
        self.stashed.lock().unwrap().extend(open.into_values());
        self.save();
    }

    /// Takes the windows waiting to be resumed.
    pub fn unstash(&self) -> Vec<WindowSession> {
        std::mem::take(&mut *self.stashed.lock().unwrap())
    }

    pub fn save(&self) {
        let _writing = self.writing.lock().unwrap();
        self.dirty.store(false, Ordering::SeqCst);
        let open: Vec<_> = self.windows.lock().unwrap().values().cloned().collect();
        let mut windows = self.stashed.lock().unwrap().clone();
        let pending = open.is_empty() && !windows.is_empty();
        windows.extend(open);
        let closed = self.closed.lock().unwrap().clone();
        let _ = Session {
            windows,
            closed,
            pending,
        }
        .save(&self.directory);
    }

    fn schedule(&self, app: &AppHandle) {
        if self.dirty.swap(true, Ordering::SeqCst) {
            return;
        }
        let app = app.clone();
        tauri::async_runtime::spawn(async move {
            tokio::time::sleep(Duration::from_millis(500)).await;
            let sessions = app.state::<Sessions>();
            if sessions.dirty.load(Ordering::SeqCst) && !sessions.quitting.load(Ordering::SeqCst) {
                sessions.save();
            }
        });
    }
}

/// A window's logical position and inner size.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Geometry {
    pub x: f64,
    pub y: f64,
    pub width: f64,
    pub height: f64,
}

impl Geometry {
    pub fn read(window: &Window) -> Option<Self> {
        if window.is_minimized().unwrap_or(false) {
            return None;
        }
        let scale = window.scale_factor().ok()?;
        let position = window.outer_position().ok()?.to_logical::<f64>(scale);
        let size = window.inner_size().ok()?.to_logical::<f64>(scale);
        Some(Self {
            x: position.x,
            y: position.y,
            width: size.width,
            height: size.height,
        })
    }

    pub fn apply(self, saved: &mut WindowSession) {
        saved.x = Some(self.x);
        saved.y = Some(self.y);
        saved.width = Some(self.width);
        saved.height = Some(self.height);
    }
}

/// The geometry of every open window that is not minimized.
pub fn geometry(app: &AppHandle) -> Vec<(String, Geometry)> {
    app.windows()
        .values()
        .filter_map(|w| Some((w.label().to_string(), Geometry::read(w)?)))
        .collect()
}

#[cfg(test)]
#[path = "sessions_tests.rs"]
mod tests;
