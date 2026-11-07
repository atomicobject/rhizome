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
    let (send, receive) = sync_channel(1);
    view.with_webview(move |view| {
        let handler = AcceleratorKeyPressedEventHandler::create(Box::new(move |_, args| {
            let Some(args) = args else { return Ok(()) };
            let mut handled = Default::default();
            let mut kind = COREWEBVIEW2_KEY_EVENT_KIND_KEY_DOWN;
            let mut key = 0;
            unsafe {
                args.Handled(&mut handled)?;
                if handled.as_bool() {
                    return Ok(());
                }
                args.KeyEventKind(&mut kind)?;
                if kind != COREWEBVIEW2_KEY_EVENT_KIND_KEY_DOWN
                    && kind != COREWEBVIEW2_KEY_EVENT_KIND_SYSTEM_KEY_DOWN
                {
                    return Ok(());
                }
                args.VirtualKey(&mut key)?;
            }
            let modifiers = unsafe {
                (u8::from(GetKeyState(i32::from(VK_CONTROL)) < 0) * CTRL)
                    | (u8::from(GetKeyState(i32::from(VK_SHIFT)) < 0) * SHIFT)
                    | (u8::from(GetKeyState(i32::from(VK_MENU)) < 0) * ALT)
                    | (u8::from(
                        GetKeyState(i32::from(VK_LWIN)) < 0 || GetKeyState(i32::from(VK_RWIN)) < 0,
                    ) * META)
            };
            let Some(id) = action(key, modifiers, |id| {
                super::shortcut_enabled(window.app_handle(), id)
            }) else {
                return Ok(());
            };
            unsafe { args.SetHandled(true)? };
            let window = window.clone();
            // Leave the synchronous WebView2 callback before an action creates a webview.
            tauri::async_runtime::spawn_blocking(move || {
                let app = window.app_handle().clone();
                super::dispatch(&app, id, Some(window));
            });
            Ok(())
        }));
        let mut token = 0;
        // The controller retains its handler until the webview closes.
        let result = unsafe {
            view.controller()
                .add_AcceleratorKeyPressed(&handler, &mut token)
        }
        .map_err(|error| error.to_string());
        let _ = send.send(result);
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
