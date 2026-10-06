use crate::{
    pane::{Pane, Panes},
    presence::Presence,
    security,
    state::{Session, WindowSession},
    terminal::Focus,
};
use serde::Deserialize;
use serde_json::json;
use std::{
    collections::BTreeMap,
    path::PathBuf,
    sync::{
        atomic::{AtomicBool, AtomicU64, Ordering},
        Mutex,
    },
    time::Duration,
};
use tauri::{
    menu::{CheckMenuItem, IsMenuItem, Menu, MenuItem, MenuItemKind, PredefinedMenuItem, Submenu},
    webview::{NewWindowResponse, PageLoadEvent, WebviewBuilder},
    window::WindowBuilder,
    AppHandle, LogicalPosition, LogicalSize, Manager, Rect, Url, WebviewUrl, Window, WindowEvent,
    Wry,
};
use tauri_plugin_opener::OpenerExt;

const WIDTH: f64 = 1320.0;
const HEIGHT: f64 = 860.0;

static NEXT: AtomicU64 = AtomicU64::new(1);

pub fn shell_label(window: &str) -> String {
    window.replacen("window-", "shell-", 1)
}

pub fn content_label(window: &str) -> String {
    window.replacen("window-", "content-", 1)
}

/// Each open window's layout and selection, saved so a relaunch restores them.
/// Quitting keeps every window; only closing a window removes it.
pub struct Sessions {
    directory: PathBuf,
    windows: Mutex<BTreeMap<u64, WindowSession>>,
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

impl Sessions {
    pub fn new(directory: PathBuf) -> Self {
        Self {
            directory,
            windows: Mutex::default(),
            closed: Mutex::default(),
            writing: Mutex::default(),
            quitting: AtomicBool::new(false),
            dirty: AtomicBool::new(false),
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
    pub fn quit(&self, geometry: impl IntoIterator<Item = (String, Geometry)>) {
        self.quitting.store(true, Ordering::SeqCst);
        for (window, geometry) in geometry {
            self.apply(&window, |saved| geometry.apply(saved));
        }
        self.save();
    }

    pub fn save(&self) {
        let _writing = self.writing.lock().unwrap();
        self.dirty.store(false, Ordering::SeqCst);
        let windows = self.windows.lock().unwrap().values().cloned().collect();
        let closed = self.closed.lock().unwrap().clone();
        let _ = Session { windows, closed }.save(&self.directory);
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
    fn read(window: &Window) -> Option<Self> {
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

    fn apply(self, saved: &mut WindowSession) {
        saved.x = Some(self.x);
        saved.y = Some(self.y);
        saved.width = Some(self.width);
        saved.height = Some(self.height);
    }
}

fn record(app: &AppHandle, window: &Window) {
    if let Some(geometry) = Geometry::read(window) {
        app.state::<Sessions>()
            .update(app, window.label(), |saved| geometry.apply(saved));
    }
}

/// Saves the session on quit, including windows that were never moved.
pub fn quit(app: &AppHandle) {
    let geometry: Vec<_> = app
        .windows()
        .values()
        .filter_map(|w| Some((w.label().to_string(), Geometry::read(w)?)))
        .collect();
    app.state::<Sessions>().quit(geometry);
}

/// Restores the windows from the last session, or one new window.
pub fn restore(app: &AppHandle, session: Session) -> tauri::Result<()> {
    let mut windows = session.windows;
    *app.state::<Sessions>().closed.lock().unwrap() = session.closed.clone();
    if windows.is_empty() {
        windows.push(session.closed.unwrap_or_default());
    }
    for window in windows {
        create(app, window)?;
    }
    Ok(())
}

fn on_screen(app: &AppHandle, x: f64, y: f64) -> bool {
    app.available_monitors()
        .unwrap_or_default()
        .iter()
        .any(|m| {
            let scale = m.scale_factor();
            let (position, size) = (m.position(), m.size());
            let (left, top) = (position.x as f64 / scale, position.y as f64 / scale);
            x >= left
                && y >= top
                && x < left + size.width as f64 / scale
                && y < top + size.height as f64 / scale
        })
}

const CLICK_GUARD: &str = r#"
    document.addEventListener('click', event => {
        const anchor = event.target.closest?.('a[href]');
        if (!anchor || event.defaultPrevented) return;
        const url = new URL(anchor.href, location.href);
        if ((url.protocol === 'http:' || url.protocol === 'https:') && url.origin !== location.origin) {
            event.preventDefault();
            window.open(url.href, '_blank');
        }
    });
"#;

/// A window holds the bundled shell, filling the window, and a repository
/// content webview the shell positions over its content region. The content
/// webview has no capabilities and may only show the verified runtime.
pub fn create(app: &AppHandle, session: WindowSession) -> tauri::Result<Window> {
    let label = format!("window-{}", NEXT.fetch_add(1, Ordering::SeqCst));
    let width = session.width.unwrap_or(WIDTH).max(760.0);
    let height = session.height.unwrap_or(HEIGHT).max(520.0);
    let mut builder = WindowBuilder::new(app, &label)
        .title(&app.package_info().name)
        .inner_size(width, height)
        .min_inner_size(760.0, 520.0);
    #[cfg(target_os = "macos")]
    {
        builder = builder
            .title_bar_style(tauri::TitleBarStyle::Overlay)
            .hidden_title(true);
    }
    if let (Some(x), Some(y)) = (session.x, session.y) {
        if on_screen(app, x + 40.0, y + 20.0) {
            builder = builder.position(x, y);
        }
    }
    let window = builder.build()?;
    app.state::<Sessions>()
        .update(app, &label, |saved| *saved = session);
    record(app, &window);
    let pane = Pane::default();
    let expected = pane.expected.clone();
    app.state::<Panes>().insert(label.clone(), pane);

    let shell = shell_label(&label);
    window.add_child(
        WebviewBuilder::new(&shell, WebviewUrl::App("index.html".into()))
            .auto_resize()
            // The sidebar reorders repositories with HTML5 drag and drop.
            .disable_drag_drop_handler()
            .on_navigation(move |url| {
                security::shell_authorized(&shell, url, cfg!(debug_assertions))
            }),
        LogicalPosition::new(0.0, 0.0),
        LogicalSize::new(width, height),
    )?;
    let popup_app = app.clone();
    let content = window.add_child(
        WebviewBuilder::new(
            content_label(&label),
            WebviewUrl::External("about:blank".parse().unwrap()),
        )
        .initialization_script(CLICK_GUARD)
        // Tauri's native file-drop handler claims every drag, so the page never
        // sees dragover or drop; the runtime UI needs HTML5 drag and drop.
        .disable_drag_drop_handler()
        .on_navigation(move |target| {
            expected.read().is_ok_and(|origin| match origin.as_ref() {
                Some(origin) => {
                    security::same_origin(origin, target)
                        || security::runtime_content_origin(origin, target)
                        || security::empty_frame_document(target)
                }
                None => security::empty_frame_document(target),
            })
        })
        .on_new_window(move |target, _| {
            if security::external_url(&target) {
                let _ = popup_app.opener().open_url(target.as_str(), None::<&str>);
            }
            NewWindowResponse::Deny
        })
        .on_page_load(|webview, payload| {
            let shown = || {
                webview
                    .app_handle()
                    .state::<Panes>()
                    .with(webview.window().label(), |pane| pane.visible())
                    .unwrap_or(false)
            };
            // Going back past the runtime's first page reaches the blank page
            // the view started on; return to the workspace instead.
            if payload.event() == PageLoadEvent::Finished
                && payload.url().as_str() == "about:blank"
                && shown()
            {
                let _ = webview.eval("history.forward()");
                return;
            }
            if payload.event() == PageLoadEvent::Started {
                crate::pipeline::navigation_started(
                    webview.app_handle(),
                    webview.window().label(),
                    payload.url(),
                );
            }
            if payload.event() == PageLoadEvent::Finished {
                crate::pipeline::loaded(
                    webview.app_handle(),
                    webview.window().label(),
                    payload.url(),
                );
            }
        }),
        LogicalPosition::new(0.0, 0.0),
        LogicalSize::new(1.0, 1.0),
    )?;
    content.hide()?;

    let app = app.clone();
    let events = window.clone();
    window.on_window_event(move |event| match event {
        WindowEvent::Focused(true) => {
            app.state::<Focus>().focused(events.label());
            app.state::<Presence>().rediscover();
        }
        WindowEvent::Moved(_) | WindowEvent::Resized(_) => record(&app, &events),
        WindowEvent::CloseRequested { .. } => app
            .state::<Sessions>()
            .close(events.label(), Geometry::read(&events)),
        WindowEvent::Destroyed => {
            app.state::<Panes>().remove(events.label());
            app.state::<Focus>().closed(events.label());
        }
        _ => {}
    });
    Ok(window)
}

/// Applies the shell's content region and whether the shell is drawing over it.
pub fn layout(window: &Window, bounds: Rect, covered: bool) -> Result<(), String> {
    let content = window
        .get_webview(&content_label(window.label()))
        .ok_or("This window has no content view.")?;
    content.set_bounds(bounds).map_err(|e| e.to_string())?;
    window
        .app_handle()
        .state::<Panes>()
        .with(window.label(), |pane| pane.cover(covered));
    sync_visibility(window)
}

/// Leaves the content view's runtime page, closing its event streams.
pub fn blank(window: &Window) {
    if let Some(content) = window.get_webview(&content_label(window.label())) {
        let _ = content.navigate("about:blank".parse().unwrap());
    }
}

pub fn sync_visibility(window: &Window) -> Result<(), String> {
    let Some(content) = window.get_webview(&content_label(window.label())) else {
        return Ok(());
    };
    let visible = window
        .app_handle()
        .state::<Panes>()
        .with(window.label(), |pane| pane.visible())
        .unwrap_or(false);
    if visible {
        content.show()
    } else {
        content.hide()
    }
    .map_err(|e| e.to_string())
}

/// The address of the runtime page the content view shows: any page, or only
/// one of `worktree`'s runtime when a worktree is named.
pub fn page_url(window: &Window, worktree: Option<&str>) -> Option<Url> {
    let shown =
        window
            .app_handle()
            .state::<Panes>()
            .with(window.label(), |pane| match worktree {
                Some(worktree) => pane.shows(worktree),
                None => pane.visible(),
            })?;
    if !shown {
        return None;
    }
    window
        .get_webview(&content_label(window.label()))?
        .url()
        .ok()
}

/// Asks the web UI to close its active note tab; the Home tab stays open.
fn close_active_tab(window: &Window) {
    if page_url(window, None).is_none() {
        return;
    }
    if let Some(content) = window.get_webview(&content_label(window.label())) {
        let _ = content.eval("window.dispatchEvent(new CustomEvent('rhizome:close-tab'))");
    }
}

fn copy_page_url(window: &Window) -> Result<(), String> {
    let url = page_url(window, None).ok_or("No Rhizome page is open in this window.")?;
    copy_text(url.as_str())
}

#[cfg(target_os = "macos")]
fn copy_text(text: &str) -> Result<(), String> {
    use std::{io::Write, process::Stdio};
    let mut child = std::process::Command::new("/usr/bin/pbcopy")
        .stdin(Stdio::piped())
        .spawn()
        .map_err(|e| format!("Cannot copy the address: {e}"))?;
    child
        .stdin
        .take()
        .ok_or("Cannot copy the address.")?
        .write_all(text.as_bytes())
        .map_err(|e| format!("Cannot copy the address: {e}"))?;
    child.wait().map_err(|e| e.to_string())?;
    Ok(())
}

#[cfg(not(target_os = "macos"))]
fn copy_text(_: &str) -> Result<(), String> {
    Err("Copying the page address is available only on macOS.".into())
}

#[derive(Clone, Copy, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Browse {
    Back,
    Forward,
    Reload,
}

/// Moves the content view through its history, or reloads it, while it shows
/// a runtime page.
pub fn browse(window: &Window, to: Browse) -> Result<(), String> {
    let shown = window
        .app_handle()
        .state::<Panes>()
        .with(window.label(), |pane| pane.visible())
        .unwrap_or(false);
    let Some(content) = window.get_webview(&content_label(window.label())) else {
        return Ok(());
    };
    if !shown {
        return Ok(());
    }
    match to {
        Browse::Back => content.eval("history.back()"),
        Browse::Forward => content.eval("history.forward()"),
        Browse::Reload => content.reload(),
    }
    .map_err(|e| e.to_string())
}

/// A native popup entry. The shell supplies its own item identifiers, which
/// return to that shell when chosen. Native menus draw above the content view.
#[derive(Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct MenuEntry {
    #[serde(default)]
    id: String,
    #[serde(default)]
    label: String,
    #[serde(default)]
    checked: Option<bool>,
    #[serde(default)]
    disabled: bool,
    #[serde(default)]
    separator: bool,
    #[serde(default)]
    items: Vec<MenuEntry>,
}

fn menu_items(
    app: &AppHandle,
    window: &str,
    entries: &[MenuEntry],
) -> tauri::Result<Vec<MenuItemKind<Wry>>> {
    let mut items = vec![];
    for entry in entries {
        let id = format!("popup:{window}:{}", entry.id);
        items.push(if entry.separator {
            MenuItemKind::Predefined(PredefinedMenuItem::separator(app)?)
        } else if !entry.items.is_empty() {
            let children = menu_items(app, window, &entry.items)?;
            let children: Vec<&dyn IsMenuItem<Wry>> =
                children.iter().map(|c| c as &dyn IsMenuItem<Wry>).collect();
            MenuItemKind::Submenu(Submenu::with_items(
                app,
                &entry.label,
                !entry.disabled,
                &children,
            )?)
        } else if let Some(checked) = entry.checked {
            MenuItemKind::Check(CheckMenuItem::with_id(
                app,
                id,
                &entry.label,
                !entry.disabled,
                checked,
                None::<&str>,
            )?)
        } else {
            MenuItemKind::MenuItem(MenuItem::with_id(
                app,
                id,
                &entry.label,
                !entry.disabled,
                None::<&str>,
            )?)
        });
    }
    Ok(items)
}

pub fn popup(window: &Window, x: f64, y: f64, entries: &[MenuEntry]) -> Result<(), String> {
    let app = window.app_handle();
    let items = menu_items(app, window.label(), entries).map_err(|e| e.to_string())?;
    let items: Vec<&dyn IsMenuItem<Wry>> =
        items.iter().map(|i| i as &dyn IsMenuItem<Wry>).collect();
    let menu = Menu::with_items(app, &items).map_err(|e| e.to_string())?;
    window
        .popup_menu_at(&menu, LogicalPosition::new(x, y))
        .map_err(|e| e.to_string())
}

fn focused_window(app: &AppHandle) -> Option<Window> {
    let windows = app.windows();
    windows
        .values()
        .find(|w| w.is_focused().unwrap_or(false))
        .or_else(|| windows.values().next())
        .cloned()
}

/// Brings an existing window forward, or opens one when none remain.
pub fn reopen(app: &AppHandle) {
    match focused_window(app) {
        Some(window) => {
            let _ = window.unminimize();
            let _ = window.set_focus();
        }
        None => {
            let _ = create(app, app.state::<Sessions>().last_closed());
        }
    }
}

fn command(app: &AppHandle, name: &str) {
    match focused_window(app) {
        Some(window) => {
            app.state::<Panes>().with(window.label(), |pane| {
                pane.send(json!({"type": "command", "command": name}))
            });
        }
        None => {
            let _ = create(app, WindowSession::default());
        }
    }
}

pub fn install_menu(app: &AppHandle) -> tauri::Result<()> {
    let settings = MenuItem::with_id(app, "settings", "Settings…", true, Some("CmdOrCtrl+,"))?;
    let new_window = MenuItem::with_id(app, "new-window", "New Window", true, Some("CmdOrCtrl+N"))?;
    let close_tab = MenuItem::with_id(app, "close-tab", "Close Tab", true, Some("CmdOrCtrl+W"))?;
    let close_window = MenuItem::with_id(
        app,
        "close-window",
        "Close Window",
        true,
        Some("CmdOrCtrl+Shift+W"),
    )?;
    let copy_url = MenuItem::with_id(
        app,
        "copy-url",
        "Copy Page URL",
        true,
        Some("CmdOrCtrl+Shift+C"),
    )?;
    let add = MenuItem::with_id(
        app,
        "add-repository",
        "Add Repository…",
        true,
        Some("CmdOrCtrl+O"),
    )?;
    let sidebar = MenuItem::with_id(
        app,
        "toggle-sidebar",
        "Toggle Sidebar",
        true,
        Some("Ctrl+CmdOrCtrl+S"),
    )?;
    let app_menu = Submenu::with_items(
        app,
        &app.package_info().name,
        true,
        &[
            &PredefinedMenuItem::about(app, None, None)?,
            &PredefinedMenuItem::separator(app)?,
            &settings,
            &PredefinedMenuItem::separator(app)?,
            &PredefinedMenuItem::hide(app, None)?,
            &PredefinedMenuItem::hide_others(app, None)?,
            &PredefinedMenuItem::show_all(app, None)?,
            &PredefinedMenuItem::separator(app)?,
            &PredefinedMenuItem::quit(app, None)?,
        ],
    )?;
    let file_menu = Submenu::with_items(
        app,
        "File",
        true,
        &[
            &new_window,
            &add,
            &PredefinedMenuItem::separator(app)?,
            &close_tab,
            &close_window,
        ],
    )?;
    let edit_menu = Submenu::with_items(
        app,
        "Edit",
        true,
        &[
            &PredefinedMenuItem::undo(app, None)?,
            &PredefinedMenuItem::redo(app, None)?,
            &PredefinedMenuItem::separator(app)?,
            &PredefinedMenuItem::cut(app, None)?,
            &PredefinedMenuItem::copy(app, None)?,
            &PredefinedMenuItem::paste(app, None)?,
            &PredefinedMenuItem::select_all(app, None)?,
            &PredefinedMenuItem::separator(app)?,
            &copy_url,
        ],
    )?;
    let reload = MenuItem::with_id(app, "reload", "Reload Page", true, Some("CmdOrCtrl+R"))?;
    // No shortcuts: the editor uses Cmd-[ and Cmd-] for indentation.
    let back = MenuItem::with_id(app, "back", "Back", true, None::<&str>)?;
    let forward = MenuItem::with_id(app, "forward", "Forward", true, None::<&str>)?;
    let view_menu = Submenu::with_items(
        app,
        "View",
        true,
        &[
            &reload,
            &back,
            &forward,
            &PredefinedMenuItem::separator(app)?,
            &sidebar,
            &PredefinedMenuItem::fullscreen(app, None)?,
        ],
    )?;
    let window_menu = Submenu::with_items(
        app,
        "Window",
        true,
        &[
            &PredefinedMenuItem::minimize(app, None)?,
            &PredefinedMenuItem::maximize(app, None)?,
        ],
    )?;
    app.set_menu(Menu::with_items(
        app,
        &[&app_menu, &file_menu, &edit_menu, &view_menu, &window_menu],
    )?)?;
    app.on_menu_event(|app, event| {
        let id = event.id().as_ref();
        if let Some((window, item)) = id
            .strip_prefix("popup:")
            .and_then(|rest| rest.split_once(':'))
        {
            app.state::<Panes>().with(window, |pane| {
                pane.send(json!({"type": "menu", "id": item}))
            });
            return;
        }
        match id {
            "new-window" => {
                let session = match app.windows().is_empty() {
                    true => app.state::<Sessions>().last_closed(),
                    false => WindowSession::default(),
                };
                let _ = create(app, session);
            }
            "add-repository" | "toggle-sidebar" | "settings" => command(app, id),
            "close-tab" => {
                if let Some(window) = focused_window(app) {
                    close_active_tab(&window);
                }
            }
            "close-window" => {
                if let Some(window) = focused_window(app) {
                    let _ = window.close();
                }
            }
            "copy-url" => {
                if let Some(window) = focused_window(app) {
                    if let Err(message) = copy_page_url(&window) {
                        app.state::<Panes>().with(window.label(), |pane| {
                            pane.send(json!({"type": "alert", "code": "desktop_error", "message": message}))
                        });
                    }
                }
            }
            "reload" | "back" | "forward" => {
                let to = match id {
                    "reload" => Browse::Reload,
                    "back" => Browse::Back,
                    _ => Browse::Forward,
                };
                if let Some(window) = focused_window(app) {
                    let _ = browse(&window, to);
                }
            }
            _ => {}
        }
    });
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn shell_menu_entries_deserialize() {
        let entries: Vec<MenuEntry> = serde_json::from_str(
            r#"[{"label":"Primary worktree","disabled":false,"id":"m0","items":[
                {"label":"Automatic (main)","checked":true,"id":"m1"},
                {"separator":true,"id":"m2"}]},
               {"label":"Remove from Library","id":"m3"}]"#,
        )
        .unwrap();
        assert_eq!(entries[0].items.len(), 2);
        assert_eq!(entries[0].items[0].checked, Some(true));
        assert!(entries[0].items[1].separator);
        assert_eq!(entries[1].id, "m3");
        assert!(serde_json::from_str::<Vec<MenuEntry>>(r#"[{"label":"x","run":1}]"#).is_err());
    }

    #[test]
    fn quitting_saves_geometry_of_windows_that_never_moved() {
        let directory = tempfile::tempdir().unwrap();
        let sessions = Sessions::new(directory.path().into());
        sessions.apply("window-1", |saved| saved.sidebar_collapsed = true);
        sessions.apply("window-2", |_| {});
        let geometry = Geometry {
            x: 40.0,
            y: 60.0,
            width: 1000.0,
            height: 700.0,
        };
        sessions.quit([("window-1".to_string(), geometry)]);
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
                sidebar_collapsed: true,
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
}
