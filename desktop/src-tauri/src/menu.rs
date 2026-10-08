//! The application menu bar and the shell's native popup menus.
use crate::{
    page,
    pane::Panes,
    state::WindowSession,
    windows::{
        browse, content_label, create, focused_window, shell_label, visible_page_url, Browse,
        Sessions,
    },
};
use serde::Deserialize;
use serde_json::json;
use tauri::{
    menu::{CheckMenuItem, IsMenuItem, Menu, MenuItem, MenuItemKind, PredefinedMenuItem, Submenu},
    AppHandle, LogicalPosition, Manager, Window, Wry,
};

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

fn copy_page_url(window: &Window) -> Result<(), String> {
    let url = visible_page_url(window).ok_or("No Rhizome page is open in this window.")?;
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
    let status = child.wait().map_err(|e| e.to_string())?;
    if !status.success() {
        return Err("Cannot copy the address.".into());
    }
    Ok(())
}

#[cfg(not(target_os = "macos"))]
fn copy_text(_: &str) -> Result<(), String> {
    Err("Copying the page address is available only on macOS.".into())
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
    let search = MenuItem::with_id(
        app,
        "search",
        "Search This Project",
        true,
        Some("CmdOrCtrl+K"),
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
            &search,
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
            "search" => {
                if let Some(window) = focused_window(app) {
                    focus_search(&window);
                }
            }
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
}
