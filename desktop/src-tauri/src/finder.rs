//! The repository (⌘O) and worktree (⇧⌘O) finders (SPEC-0126 US9, US10). They
//! draw in a transparent overlay webview above the content view, so the page
//! stays visible under the scrim. The overlay loads the bundled shell in its
//! finder mode; its label starts with `shell-`, so it holds the shell's
//! capabilities and nothing more. The shell builds what the finder lists from
//! the library and presence it already has; the finder sends the user's choice
//! back, and the app relays it to the shell.
use crate::{
    pane::Panes,
    security,
    state::WindowSession,
    windows::{content_label, create, shell_label},
};
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use tauri::{
    webview::WebviewBuilder, AppHandle, LogicalPosition, LogicalSize, Manager, WebviewUrl, Window,
};

/// Puts the bundled UI in finder mode.
const MARKER: &str = "window.__RHIZOME_FINDER__ = true;";

pub fn label(window: &str) -> String {
    window.replacen("window-", "shell-finder-", 1)
}

/// What the user chose in the finder.
#[derive(Clone, Debug, PartialEq, Deserialize, Serialize)]
#[serde(tag = "choice", rename_all = "kebab-case", deny_unknown_fields)]
pub enum Choice {
    Close,
    /// A repository without `worktree` opens its primary worktree.
    #[serde(rename_all = "camelCase")]
    Open {
        repository: String,
        worktree: Option<String>,
        new_window: bool,
    },
    Add,
    /// A row's actions menu, at window coordinates.
    Menu {
        repository: String,
        x: f64,
        y: f64,
    },
}

/// Shows the shell's `revision`th `model` in the window's finder, creating the
/// overlay on first use. `return_to_content` names where focus goes back when
/// the finder closes.
pub fn show(
    window: &Window,
    model: Value,
    revision: u64,
    return_to_content: bool,
) -> Result<(), String> {
    let finder = label(window.label());
    let opened = model["opened"]
        .as_f64()
        .ok_or("The finder model has no opening stamp.")?;
    let Some(starts) = window
        .app_handle()
        .state::<Panes>()
        .with(window.label(), |pane| {
            pane.open_finder(model, opened, revision, return_to_content)
        })
        .flatten()
    else {
        return Ok(());
    };
    if window.get_webview(&finder).is_none() {
        let size = window
            .inner_size()
            .map_err(|e| e.to_string())?
            .to_logical::<f64>(window.scale_factor().map_err(|e| e.to_string())?);
        let allowed = finder.clone();
        window
            .add_child(
                WebviewBuilder::new(&finder, WebviewUrl::App("index.html".into()))
                    // Transparent outside the palette, so the scrim lets the page show.
                    .transparent(true)
                    .auto_resize()
                    .initialization_script(MARKER)
                    .on_navigation(move |url| {
                        security::shell_authorized(&allowed, url, cfg!(debug_assertions))
                    }),
                LogicalPosition::new(0.0, 0.0),
                LogicalSize::new(size.width, size.height),
            )
            // A concurrent request may have created it first.
            .or_else(|error| window.get_webview(&finder).ok_or(error.to_string()))?;
    }
    // A live update leaves focus alone, so it never pulls focus back from
    // another window.
    sync(window, if starts { Focus::Finder } else { Focus::Stay });
    Ok(())
}

/// Hides the finder for opening `opening`, or the one on screen. Focus returns
/// where it was when the finder opened, or to the shell when `to_shell`, as
/// when the shell closes it to show something of its own.
pub fn hide(window: &Window, opening: Option<f64>, to_shell: bool) {
    let closed = window
        .app_handle()
        .state::<Panes>()
        .with(window.label(), |pane| pane.close_finder(opening))
        .unwrap_or(false);
    sync(
        window,
        if closed {
            Focus::Return { to_shell }
        } else {
            Focus::Stay
        },
    );
}

/// Where focus goes when the overlay syncs.
#[derive(Clone, Copy)]
enum Focus {
    /// Left alone: a live update, or a close that closed nothing.
    Stay,
    /// To the finder, as an opening starts.
    Finder,
    /// After a close: to the shell when `to_shell`, else back where it was
    /// when the finder opened.
    Return { to_shell: bool },
}

/// Shows or hides the overlay as the pane says. Show and hide requests race,
/// so each queues this on the main thread, which runs tasks in order, and it
/// reads the pane when it runs: the last one leaves the overlay right.
fn sync(window: &Window, focus: Focus) {
    let window = window.clone();
    let app = window.app_handle().clone();
    let _ = app.run_on_main_thread(move || {
        let Some((open, to_content, visible)) = window
            .app_handle()
            .state::<Panes>()
            .with(window.label(), |pane| {
                (pane.finder_open, pane.return_to_content, pane.visible())
            })
        else {
            return;
        };
        let view = window.get_webview(&label(window.label()));
        if open {
            if let Some(view) = view {
                let _ = view.show();
                if matches!(focus, Focus::Finder) {
                    let _ = view.set_focus();
                }
            }
            return;
        }
        if let Some(view) = view {
            let _ = view.hide();
        }
        let Focus::Return { to_shell } = focus else {
            return;
        };
        let back = match to_content && visible && !to_shell {
            true => content_label(window.label()),
            false => shell_label(window.label()),
        };
        if let Some(view) = window.get_webview(&back) {
            let _ = view.set_focus();
        }
    });
}

/// Acts on the finder's choice: a new window opens here; everything else goes
/// to the shell, which owns selection and the repository actions.
pub fn choose(app: &AppHandle, window: &Window, choice: Choice) {
    if !matches!(choice, Choice::Menu { .. }) {
        hide(window, None, false);
    }
    if let Choice::Open {
        repository,
        worktree,
        new_window: true,
    } = &choice
    {
        let _ = create(
            app,
            WindowSession {
                repository: Some(repository.clone()),
                worktree: worktree.clone(),
                ..WindowSession::default()
            },
        );
    }
    let mut message = json!(choice);
    message["type"] = "finder".into();
    app.state::<Panes>().deliver(window.label(), message);
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn finder_webviews_get_the_shells_capabilities_only_by_label() {
        let local: url::Url = "tauri://localhost/index.html".parse().unwrap();
        assert_eq!(label("window-3"), "shell-finder-3");
        assert!(security::shell_authorized(
            &label("window-3"),
            &local,
            false
        ));
    }

    #[test]
    fn choices_deserialize_strictly() {
        let open: Choice = serde_json::from_value(json!({
            "choice": "open", "repository": "r", "worktree": "/w", "newWindow": true
        }))
        .unwrap();
        assert_eq!(
            open,
            Choice::Open {
                repository: "r".into(),
                worktree: Some("/w".into()),
                new_window: true
            }
        );
        assert_eq!(
            json!(Choice::Menu {
                repository: "r".into(),
                x: 1.0,
                y: 2.0
            }),
            json!({"choice": "menu", "repository": "r", "x": 1.0, "y": 2.0})
        );
        for invalid in [
            json!({"choice": "open", "repository": "r"}),
            json!({"choice": "navigate", "url": "https://example.com"}),
        ] {
            assert!(serde_json::from_value::<Choice>(invalid).is_err());
        }
    }
}
