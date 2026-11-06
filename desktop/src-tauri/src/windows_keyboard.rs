//! WebView2 consumes child-window keys before the native menu sees them.

const CTRL: u8 = 1;
const SHIFT: u8 = 2;
const ALT: u8 = 4;
const META: u8 = 8;

fn binding(accelerator: &str) -> Option<(u32, u8)> {
    let (modifiers, key) = accelerator.rsplit_once('+').unwrap_or(("", accelerator));
    let key = match key {
        "," => 0xbc,
        "F11" => 0x7a,
        key if key.len() == 1 && key.as_bytes()[0].is_ascii_uppercase() => {
            u32::from(key.as_bytes()[0])
        }
        _ => return None,
    };
    let modifiers = modifiers.split('+').try_fold(0, |value, modifier| {
        Some(
            value
                | match modifier {
                    "CmdOrCtrl" | "Ctrl" => CTRL,
                    "Shift" => SHIFT,
                    "Alt" => ALT,
                    "" => 0,
                    _ => return None,
                },
        )
    })?;
    Some((key, modifiers))
}

fn action(key: u32, modifiers: u8, enabled: impl Fn(&str) -> bool) -> Option<&'static str> {
    super::SHORTCUTS.iter().find_map(|(id, accelerator)| {
        (binding(accelerator) == Some((key, modifiers)) && enabled(id)).then_some(*id)
    })
}

fn trace_mode(version: &str, mode: Option<&str>) -> bool {
    version == "0.0.0" && mode == Some("ctrl-n")
}

fn trace_candidate(key: u32) -> bool {
    matches!(key, 0x4e | 0x11 | 0xa2 | 0xa3)
}

#[cfg(windows)]
fn tracing(app: &tauri::AppHandle) -> bool {
    trace_mode(
        &app.package_info().version.to_string(),
        std::env::var("RZM_DESKTOP_KEY_TRACE").ok().as_deref(),
    )
}

#[cfg(windows)]
pub(crate) fn trace(app: &tauri::AppHandle, stage: &str, window: &str, data: serde_json::Value) {
    if !tracing(app) {
        return;
    }
    eprintln!(
        "[desktop-key-trace] {}",
        serde_json::json!({
            "utc_ms": std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).unwrap_or_default().as_millis(),
            "thread": format!("{:?}", std::thread::current().id()),
            "stage": stage, "window": window, "data": data,
        })
    );
}

#[cfg(windows)]
pub(crate) fn trace_menu(
    app: &tauri::AppHandle,
    id: &str,
    window: Option<&tauri::Window>,
    enabled: bool,
) {
    if id == "new-window" && tracing(app) {
        trace(
            app,
            "menu-dispatch",
            window.map_or("", |window| window.label()),
            serde_json::json!({
                "enabled": enabled,
            }),
        );
    }
}

#[cfg(windows)]
fn trace_event(
    window: &tauri::Window,
    view: &str,
    args: &webview2_com::Microsoft::Web::WebView2::Win32::ICoreWebView2AcceleratorKeyPressedEventArgs,
    handled: bool,
) {
    use tauri::Manager;
    use webview2_com::Microsoft::Web::WebView2::Win32::{
        COREWEBVIEW2_KEY_EVENT_KIND_KEY_DOWN, COREWEBVIEW2_PHYSICAL_KEY_STATUS,
    };
    use windows_sys::Win32::UI::Input::KeyboardAndMouse::{
        GetAsyncKeyState, GetKeyState, VK_CONTROL, VK_LWIN, VK_MENU, VK_RWIN, VK_SHIFT,
    };
    let mut key = 0;
    if let Err(error) = unsafe { args.VirtualKey(&mut key) } {
        trace(
            window.app_handle(),
            "argument-error",
            window.label(),
            serde_json::json!({
                "view": view, "field": "trace-virtual-key", "hresult": error.code().0,
            }),
        );
        return;
    }
    if !trace_candidate(key) {
        return;
    }
    let mut kind = COREWEBVIEW2_KEY_EVENT_KIND_KEY_DOWN;
    let mut physical = COREWEBVIEW2_PHYSICAL_KEY_STATUS::default();
    let kind_ok = unsafe { args.KeyEventKind(&mut kind) }.is_ok();
    let physical_ok = unsafe { args.PhysicalKeyStatus(&mut physical) }.is_ok();
    let flags = |read: unsafe extern "system" fn(i32) -> i16| unsafe {
        (u8::from(read(i32::from(VK_CONTROL)) < 0) * CTRL)
            | (u8::from(read(i32::from(VK_SHIFT)) < 0) * SHIFT)
            | (u8::from(read(i32::from(VK_MENU)) < 0) * ALT)
            | (u8::from(read(i32::from(VK_LWIN)) < 0 || read(i32::from(VK_RWIN)) < 0) * META)
    };
    trace(
        window.app_handle(),
        "event",
        window.label(),
        serde_json::json!({
            "view": view, "vk": key, "kind": kind.0, "kind_ok": kind_ok, "handled": handled,
            "queue_modifiers": flags(GetKeyState), "async_modifiers": flags(GetAsyncKeyState),
            "physical_ok": physical_ok, "repeat": physical.RepeatCount,
            "was_down": physical.WasKeyDown.as_bool(), "menu_down": physical.IsMenuKeyDown.as_bool(),
            "released": physical.IsKeyReleased.as_bool(),
        }),
    );
}

#[cfg(windows)]
pub fn install(view: &tauri::Webview) -> tauri::Result<()> {
    use std::sync::mpsc::sync_channel;
    use tauri::Manager;
    use webview2_com::{
        AcceleratorKeyPressedEventHandler,
        Microsoft::Web::WebView2::Win32::{
            COREWEBVIEW2_KEY_EVENT_KIND_KEY_DOWN, COREWEBVIEW2_KEY_EVENT_KIND_SYSTEM_KEY_DOWN,
        },
    };
    use windows_sys::Win32::UI::Input::KeyboardAndMouse::{
        GetKeyState, VK_CONTROL, VK_LWIN, VK_MENU, VK_RWIN, VK_SHIFT,
    };

    let window = view.window();
    let view_label = view.label().to_owned();
    let handler_view = view_label.clone();
    let trace_window = window.clone();
    let enabled_trace = tracing(window.app_handle());
    let (send, receive) = sync_channel(1);
    view.with_webview(move |view| {
        let handler = AcceleratorKeyPressedEventHandler::create(Box::new(move |_, args| {
            let failure = |field: &str, hresult: i32| {
                if enabled_trace {
                    trace(window.app_handle(), "argument-error", window.label(), serde_json::json!({
                        "view": handler_view, "field": field, "hresult": hresult,
                    }));
                }
            };
            let Some(args) = args else {
                if enabled_trace {
                    trace(window.app_handle(), "callback-no-args", window.label(), serde_json::json!({ "view": handler_view }));
                }
                return Ok(())
            };
            let mut handled = Default::default();
            let mut kind = COREWEBVIEW2_KEY_EVENT_KIND_KEY_DOWN;
            let mut key = 0;
            unsafe {
                args.Handled(&mut handled).inspect_err(|error| failure("handled", error.code().0))?;
                if enabled_trace { trace_event(&window, &handler_view, &args, handled.as_bool()); }
                if handled.as_bool() {
                    return Ok(());
                }
                args.KeyEventKind(&mut kind).inspect_err(|error| failure("kind", error.code().0))?;
                if kind != COREWEBVIEW2_KEY_EVENT_KIND_KEY_DOWN
                    && kind != COREWEBVIEW2_KEY_EVENT_KIND_SYSTEM_KEY_DOWN
                {
                    return Ok(());
                }
                args.VirtualKey(&mut key).inspect_err(|error| failure("virtual-key", error.code().0))?;
            }
            let modifiers = unsafe {
                (u8::from(GetKeyState(i32::from(VK_CONTROL)) < 0) * CTRL)
                    | (u8::from(GetKeyState(i32::from(VK_SHIFT)) < 0) * SHIFT)
                    | (u8::from(GetKeyState(i32::from(VK_MENU)) < 0) * ALT)
                    | (u8::from(
                        GetKeyState(i32::from(VK_LWIN)) < 0 || GetKeyState(i32::from(VK_RWIN)) < 0,
                    ) * META)
            };
            let matched = action(key, modifiers, |id| {
                let enabled = super::shortcut_enabled(window.app_handle(), id);
                if enabled_trace && trace_candidate(key) {
                    trace(window.app_handle(), "enabled", window.label(), serde_json::json!({
                        "view": handler_view, "id": id, "enabled": enabled,
                    }));
                }
                enabled
            });
            if enabled_trace && trace_candidate(key) {
                trace(window.app_handle(), "match", window.label(), serde_json::json!({
                    "view": handler_view, "vk": key, "modifiers": modifiers, "matched": matched,
                }));
            }
            let Some(id) = matched else {
                return Ok(());
            };
            unsafe { args.SetHandled(true).inspect_err(|error| {
                if id == "new-window" { failure("set-handled", error.code().0); }
            })? };
            if enabled_trace && id == "new-window" {
                trace(window.app_handle(), "dispatch-queued", window.label(), serde_json::json!({ "view": handler_view }));
            }
            let window = window.clone();
            // Leave the synchronous WebView2 callback before an action creates a webview.
            tauri::async_runtime::spawn_blocking(move || {
                let app = window.app_handle().clone();
                if id == "new-window" { trace(&app, "dispatch-begin", window.label(), serde_json::json!({})); }
                let label = window.label().to_owned();
                super::dispatch(&app, id, Some(window));
                if id == "new-window" { trace(&app, "dispatch-end", &label, serde_json::json!({})); }
            });
            Ok(())
        }));
        let mut token = 0;
        // The controller retains its handler until the webview closes.
        let result = unsafe {
            view.controller()
                .add_AcceleratorKeyPressed(&handler, &mut token)
        };
        if enabled_trace {
            let mut parent = Default::default();
            let parent_result = unsafe { view.controller().ParentWindow(&mut parent) };
            trace(trace_window.app_handle(), "parent-window", trace_window.label(), serde_json::json!({
                "view": view_label, "hwnd": format!("{:p}", parent.0), "ok": parent_result.is_ok(),
                "hresult": parent_result.as_ref().err().map_or(0, |error| error.code().0),
            }));
        }
        trace(trace_window.app_handle(), "install", trace_window.label(), serde_json::json!({
            "view": view_label, "ok": result.is_ok(), "hresult": result.as_ref().err().map(|error| error.code().0), "token": token,
        }));
        let _ = send.send(result.map_err(|error| error.to_string()));
    })?;
    receive
        .recv()
        .map_err(std::io::Error::other)?
        .map_err(std::io::Error::other)?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn key_trace_requires_the_exact_local_test_mode() {
        assert!(trace_mode("0.0.0", Some("ctrl-n")));
        assert!(!trace_mode("0.0.0", None));
        assert!(!trace_mode("0.0.0", Some("all")));
        assert!(!trace_mode("1.0.0", Some("ctrl-n")));
        assert!(!trace_mode("0.0.0-dev", Some("ctrl-n")));
        for key in [0x4e, 0x11, 0xa2, 0xa3] {
            assert!(trace_candidate(key));
        }
        for key in [0x41, 0x43, 0x10, 0x12, 0x7a] {
            assert!(!trace_candidate(key));
        }
    }

    #[test]
    fn menu_shortcuts_resolve_to_their_actions() {
        for (key, modifiers, expected) in [
            (0xbc, CTRL, "settings"),
            (b'N' as u32, CTRL, "new-window"),
            (b'W' as u32, CTRL, "close-tab"),
            (b'W' as u32, CTRL | SHIFT, "close-window"),
            (b'C' as u32, CTRL | SHIFT, "copy-url"),
            (b'K' as u32, CTRL, "search"),
            (b'O' as u32, CTRL, "repositories"),
            (b'O' as u32, CTRL | SHIFT, "worktrees"),
            (b'P' as u32, CTRL, "find"),
            (b'R' as u32, CTRL, "reload"),
            (
                b'S' as u32,
                if cfg!(target_os = "macos") {
                    CTRL
                } else {
                    CTRL | ALT
                },
                "sidebar",
            ),
            (0x7a, 0, "fullscreen"),
        ] {
            assert_eq!(action(key, modifiers, |_| true), Some(expected));
        }
        assert!(super::super::SHORTCUTS
            .iter()
            .all(|(_, key)| binding(key).is_some()));
    }

    #[test]
    fn disabled_actions_and_other_keys_are_left_to_the_webview() {
        assert_eq!(action(b'N' as u32, CTRL, |_| false), None);
        for key in *b"CVXZYAHM" {
            assert_eq!(action(u32::from(key), CTRL, |_| true), None);
        }
        assert_eq!(action(b'N' as u32, 0, |_| true), None);
        assert_eq!(action(b'N' as u32, CTRL | SHIFT, |_| true), None);
        assert_eq!(action(b'N' as u32, CTRL | ALT, |_| true), None);
        assert_eq!(action(b'N' as u32, CTRL | META, |_| true), None);
        assert_eq!(action(0x7a, 0, |_| false), None);
    }
}
