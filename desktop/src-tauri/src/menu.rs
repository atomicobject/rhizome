//! The application menu bar and the shell's native popup menus.
use crate::{
    appearance,
    commands::Desktop,
    finder, page,
    pane::Panes,
    state::{Preferences, WindowSession},
    windows::{
        browse, content_label, create_from_event, focused_window, page_command, shell_label,
        visible_page_url, Browse, Sessions,
    },
};
use serde::Deserialize;
use serde_json::json;
use tauri::{
    menu::{CheckMenuItem, IsMenuItem, Menu, MenuItem, MenuItemKind, PredefinedMenuItem, Submenu},
    AppHandle, LogicalPosition, Manager, Window, Wry,
};
use tauri_plugin_clipboard_manager::ClipboardExt;

#[cfg(any(windows, test))]
#[path = "windows_keyboard.rs"]
mod windows_keyboard;

const SHORTCUTS: &[(&str, &str)] = &[
    ("settings", "CmdOrCtrl+,"),
    ("new-window", "CmdOrCtrl+N"),
    ("close-tab", "CmdOrCtrl+W"),
    ("close-window", "CmdOrCtrl+Shift+W"),
    ("copy-url", "CmdOrCtrl+Shift+C"),
    ("search", "CmdOrCtrl+K"),
    ("repositories", "CmdOrCtrl+O"),
    ("worktrees", "CmdOrCtrl+Shift+O"),
    ("find", "CmdOrCtrl+P"),
    ("reload", "CmdOrCtrl+R"),
    ("sidebar", SIDEBAR_SHORTCUT),
    ("fullscreen", "F11"),
];

struct ShortcutItems(Vec<MenuItem<Wry>>);

fn shortcut_enabled(app: &AppHandle, id: &str) -> bool {
    app.state::<ShortcutItems>()
        .0
        .iter()
        .find(|item| item.id().as_ref() == id)
        .is_some_and(|item| item.is_enabled().unwrap_or(false))
}

pub fn install_shortcuts(_view: &tauri::Webview) -> tauri::Result<()> {
    #[cfg(windows)]
    windows_keyboard::install(_view)?;
    Ok(())
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

/// Asks the web UI to close its active note tab; the Home tab stays open.
fn close_active_tab(window: &Window) {
    if visible_page_url(window).is_none() {
        return;
    }
    if let Some(content) = window.get_webview(&content_label(window.label())) {
        let _ = content.eval("window.dispatchEvent(new CustomEvent('rhizome:close-tab'))");
    }
}

/// Focuses the toolbar's search field, or the page's own field when the page
/// shows its own header and the toolbar has none.
fn focus_search(window: &Window) {
    let reported = window
        .app_handle()
        .state::<Panes>()
        .with(window.label(), |pane| pane.reported)
        .unwrap_or(false);
    if !reported {
        if let (Some(_), Some(content)) = (
            visible_page_url(window),
            window.get_webview(&content_label(window.label())),
        ) {
            let _ = content.set_focus();
            let _ = content.eval(page::LEGACY_SEARCH);
        }
        return;
    }
    // The search field is in the shell, which may not hold focus.
    if let Some(shell) = window.get_webview(&shell_label(window.label())) {
        let _ = shell.set_focus();
    }
    window
        .app_handle()
        .state::<Panes>()
        .with(window.label(), |pane| {
            pane.send(json!({"type": "command", "command": "focus-search"}))
        });
}

/// Opens the page's note and file finder (⌘P). The menu shortcut pre-empts
/// whichever webview has focus, so the content view takes focus first.
/// Closes the window's repository or worktree finder, if open, before an
/// action on the window behind it; the shell hears that it closed.
fn dismiss_finder(window: &Window) {
    let app = window.app_handle();
    let open = app
        .state::<Panes>()
        .with(window.label(), |pane| pane.finder_open)
        .unwrap_or(false);
    if open {
        finder::choose(app, window, None, finder::Choice::Close);
    }
}

fn find_in_page(window: &Window) {
    dismiss_finder(window);
    if visible_page_url(window).is_none() {
        return;
    }
    if let Some(content) = window.get_webview(&content_label(window.label())) {
        let _ = content.set_focus();
    }
    page_command(window, &page::Command::Find);
}

fn copy_page_url(window: &Window) -> Result<(), String> {
    let url = visible_page_url(window).ok_or("No Rhizome page is open in this window.")?;
    window
        .app_handle()
        .clipboard()
        .write_text(url.as_str())
        .map_err(|e| format!("Cannot copy the address: {e}"))
}

/// ⌃⌘S on macOS; ⌃⌥S elsewhere, where ⌘ is Ctrl and ⌃S would be Save.
const SIDEBAR_SHORTCUT: &str = if cfg!(target_os = "macos") {
    "Ctrl+CmdOrCtrl+S"
} else {
    "Ctrl+Alt+S"
};

fn sidebar_label(collapsed: bool) -> &'static str {
    if collapsed {
        "Show Sidebar"
    } else {
        "Hide Sidebar"
    }
}

struct SidebarItem(MenuItem<Wry>);

/// Names the View menu's sidebar item for the saved state, and enables it
/// while the focused window's page can toggle its sidebar.
pub fn refresh_sidebar(app: &AppHandle) {
    let Some(item) = app.try_state::<SidebarItem>() else {
        return;
    };
    let collapsed = Preferences::load(&app.state::<Desktop>().directory).sidebar_collapsed;
    let enabled = focused_window(app)
        .and_then(|window| {
            app.state::<Panes>()
                .with(window.label(), page::toggles_sidebar)
        })
        .unwrap_or(false);
    let _ = item.0.set_text(sidebar_label(collapsed));
    let _ = item.0.set_enabled(enabled);
}

fn toggle_sidebar(app: &AppHandle, window: Option<Window>) {
    let Some(window) = window else {
        return;
    };
    let toggles = app
        .state::<Panes>()
        .with(window.label(), page::toggles_sidebar)
        .unwrap_or(false);
    if !toggles {
        return;
    }
    let collapsed = Preferences::load(&app.state::<Desktop>().directory).sidebar_collapsed;
    if let Err(message) = page::set_sidebar(app, !collapsed) {
        app.state::<Panes>().deliver(
            window.label(),
            json!({"type": "alert", "code": "desktop_error", "message": message}),
        );
    }
}

fn command(app: &AppHandle, name: &str, window: Option<Window>) {
    match window {
        Some(window) => {
            app.state::<Panes>().with(window.label(), |pane| {
                pane.send(json!({"type": "command", "command": name}))
            });
        }
        None => {
            create_from_event(app, WindowSession::default());
        }
    }
}

pub fn install_menu(app: &AppHandle) -> tauri::Result<()> {
    let mut shortcuts = Vec::new();
    let mut item = |id: &str, label: &str, enabled: bool| -> tauri::Result<MenuItem<Wry>> {
        let accelerator = SHORTCUTS
            .iter()
            .find(|(name, _)| *name == id)
            .map(|(_, key)| *key);
        let item = MenuItem::with_id(app, id, label, enabled, accelerator)?;
        shortcuts.push(item.clone());
        Ok(item)
    };
    let settings = item("settings", "Settings…", true)?;
    let updates = MenuItem::with_id(
        app,
        "check-updates",
        "Check for Updates…",
        crate::updater::enabled_for(app),
        None::<&str>,
    )?;
    let new_window = item("new-window", "New Window", true)?;
    let close_tab = item("close-tab", "Close Tab", true)?;
    let close_window = item("close-window", "Close Window", true)?;
    let copy_url = item("copy-url", "Copy Page URL", true)?;
    let search = item("search", "Search This Project", true)?;
    // Add Repository has no shortcut: ⌘O switches repositories (SPEC-0126).
    let add = MenuItem::with_id(app, "add-repository", "Add Repository…", true, None::<&str>)?;
    let repositories = item("repositories", "Switch Repository…", true)?;
    let worktrees = item("worktrees", "Switch Worktree…", true)?;
    let find = item("find", "Go to Note or File…", true)?;
    let (appearance_menu, theme_menu) = appearance::submenus(app)?;
    // GTK does not implement the predefined quit, minimize, maximize, or
    // fullscreen items, and Windows needs fullscreen and minimize routed
    // through dispatch, so those platforms use ordinary items.
    #[cfg(not(target_os = "linux"))]
    let quit = PredefinedMenuItem::quit(app, None)?;
    #[cfg(target_os = "linux")]
    let quit = MenuItem::with_id(app, "quit", "Quit", true, Some("Ctrl+Q"))?;
    let app_menu = Submenu::with_items(
        app,
        &app.package_info().name,
        true,
        &[
            &PredefinedMenuItem::about(app, None, None)?,
            &updates,
            &PredefinedMenuItem::separator(app)?,
            &settings,
            &PredefinedMenuItem::separator(app)?,
            #[cfg(not(windows))]
            &PredefinedMenuItem::hide(app, None)?,
            #[cfg(not(windows))]
            &PredefinedMenuItem::hide_others(app, None)?,
            #[cfg(not(windows))]
            &PredefinedMenuItem::show_all(app, None)?,
            #[cfg(not(windows))]
            &PredefinedMenuItem::separator(app)?,
            &quit,
        ],
    )?;
    let file_menu = Submenu::with_items(
        app,
        "File",
        true,
        &[
            &new_window,
            &repositories,
            &worktrees,
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
            &find,
            &search,
            &copy_url,
        ],
    )?;
    let reload = item("reload", "Reload Page", true)?;
    // No shortcuts: the editor uses Cmd-[ and Cmd-] for indentation.
    let back = MenuItem::with_id(app, "back", "Back", true, None::<&str>)?;
    let forward = MenuItem::with_id(app, "forward", "Forward", true, None::<&str>)?;
    let sidebar = item(
        "sidebar",
        sidebar_label(Preferences::load(&app.state::<Desktop>().directory).sidebar_collapsed),
        false,
    )?;
    app.manage(SidebarItem(sidebar.clone()));
    #[cfg(target_os = "macos")]
    let fullscreen = PredefinedMenuItem::fullscreen(app, None)?;
    #[cfg(not(target_os = "macos"))]
    let fullscreen = item("fullscreen", "Toggle Full Screen", true)?;
    let view_menu = Submenu::with_items(
        app,
        "View",
        true,
        &[
            &reload,
            &back,
            &forward,
            &PredefinedMenuItem::separator(app)?,
            &appearance_menu,
            &theme_menu,
            &PredefinedMenuItem::separator(app)?,
            &sidebar,
            &fullscreen,
        ],
    )?;
    let minimize = PredefinedMenuItem::minimize(app, None)?;
    #[cfg(not(target_os = "macos"))]
    let minimize = MenuItem::with_id(app, "minimize", minimize.text()?, true, None::<&str>)?;
    #[cfg(not(target_os = "linux"))]
    let maximize = PredefinedMenuItem::maximize(app, None)?;
    #[cfg(target_os = "linux")]
    let maximize = MenuItem::with_id(app, "maximize", "Maximize", true, None::<&str>)?;
    let window_menu = Submenu::with_items(app, "Window", true, &[&minimize, &maximize])?;
    app.set_menu(Menu::with_items(
        app,
        &[&app_menu, &file_menu, &edit_menu, &view_menu, &window_menu],
    )?)?;
    app.manage(ShortcutItems(shortcuts));
    app.on_menu_event(|app, event| dispatch(app, event.id().as_ref(), focused_window(app)));
    Ok(())
}

fn dispatch(app: &AppHandle, id: &str, window: Option<Window>) {
    if SHORTCUTS.iter().any(|(name, _)| *name == id) && !shortcut_enabled(app, id) {
        return;
    }
    if let Some((window, item)) = id
        .strip_prefix("popup:")
        .and_then(|rest| rest.split_once(':'))
    {
        app.state::<Panes>().with(window, |pane| {
            pane.send(json!({"type": "menu", "id": item}))
        });
        return;
    }
    if appearance::menu_event(app, id) {
        return;
    }
    match id {
        "new-window" => {
            let session = match app.windows().is_empty() {
                true => app.state::<Sessions>().last_closed(),
                false => WindowSession::default(),
            };
            create_from_event(app, session);
        }
        "add-repository" | "repositories" | "worktrees" | "settings" => command(app, id, window),
        "find" => {
            if let Some(window) = window {
                find_in_page(&window);
            }
        }
        "check-updates" => crate::updater::check_from_menu(app),
        "sidebar" => toggle_sidebar(app, window),
        "search" => {
            if let Some(window) = window {
                dismiss_finder(&window);
                focus_search(&window);
            }
        }
        "close-tab" => {
            if let Some(window) = window {
                dismiss_finder(&window);
                close_active_tab(&window);
            }
        }
        "close-window" => {
            if let Some(window) = window {
                let _ = window.close();
            }
        }
        "copy-url" => {
            if let Some(window) = window {
                let message = match copy_page_url(&window) {
                    Ok(()) => json!({"type": "notice", "message": "Copied the page URL"}),
                    Err(message) => {
                        json!({"type": "alert", "code": "desktop_error", "message": message})
                    }
                };
                app.state::<Panes>()
                    .with(window.label(), |pane| pane.send(message));
            }
        }
        "reload" | "back" | "forward" => {
            let to = match id {
                "reload" => Browse::Reload,
                "back" => Browse::Back,
                _ => Browse::Forward,
            };
            if let Some(window) = window {
                dismiss_finder(&window);
                let _ = browse(&window, to);
            }
        }
        #[cfg(target_os = "linux")]
        "quit" => app.exit(0),
        #[cfg(not(target_os = "macos"))]
        "minimize" => {
            if let Some(window) = window {
                let _ = window.minimize();
            }
        }
        #[cfg(target_os = "linux")]
        "maximize" => {
            if let Some(window) = window {
                let _ = if window.is_maximized().unwrap_or(false) {
                    window.unmaximize()
                } else {
                    window.maximize()
                };
            }
        }
        #[cfg(not(target_os = "macos"))]
        "fullscreen" => {
            if let Some(window) = window {
                let _ = window
                    .is_fullscreen()
                    .and_then(|value| window.set_fullscreen(!value));
            }
        }
        _ => {}
    }
}

#[cfg(test)]
#[path = "menu_tests.rs"]
mod tests;
