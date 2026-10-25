//! The chrome theme and the light or dark appearance (SPEC-0126 US1, US2). Both
//! are app-wide settings kept in the lenient preferences file. Appearance is
//! applied natively, so every webview's `prefers-color-scheme` follows it; the
//! theme reaches each shell as a message and each workspace page as a page
//! command. Windows and webviews take the theme's colours as their backgrounds,
//! so nothing flashes white before a page paints.
use crate::{
    commands::Desktop,
    page,
    pane::Panes,
    state::Preferences,
    windows::{content_label, shell_label},
};
use serde::{Deserialize, Serialize};
use serde_json::json;
use tauri::{
    menu::{CheckMenuItem, IsMenuItem, Submenu},
    window::Color,
    AppHandle, Manager, Window, Wry,
};

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Deserialize, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum Theme {
    #[default]
    Graphite,
    Slate,
    Spruce,
    Oxblood,
    Indigo,
}

impl Theme {
    const ALL: [(Theme, &'static str); 5] = [
        (Theme::Graphite, "Graphite"),
        (Theme::Slate, "Slate"),
        (Theme::Spruce, "Spruce"),
        (Theme::Oxblood, "Oxblood"),
        (Theme::Indigo, "Indigo"),
    ];

    fn id(self) -> String {
        json!(self).as_str().unwrap_or_default().to_string()
    }
}

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Deserialize, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum Appearance {
    /// Follows the operating system.
    #[default]
    Auto,
    Light,
    Dark,
}

impl Appearance {
    const ALL: [(Appearance, &'static str); 3] = [
        (Appearance::Auto, "Match System"),
        (Appearance::Light, "Light"),
        (Appearance::Dark, "Dark"),
    ];

    pub fn native(self) -> Option<tauri::Theme> {
        match self {
            Appearance::Auto => None,
            Appearance::Light => Some(tauri::Theme::Light),
            Appearance::Dark => Some(tauri::Theme::Dark),
        }
    }

    fn id(self) -> String {
        json!(self).as_str().unwrap_or_default().to_string()
    }
}

/// What the shell and its Settings show.
#[derive(Clone, Copy, Debug, Default, PartialEq, Serialize)]
pub struct Settings {
    pub theme: Theme,
    pub appearance: Appearance,
}

pub fn current(directory: &std::path::Path) -> Settings {
    let saved = Preferences::load(directory);
    Settings {
        theme: saved.theme,
        appearance: saved.appearance,
    }
}

/// Saves a change to either setting and applies it to every window.
pub fn update(
    app: &AppHandle,
    theme: Option<Theme>,
    appearance: Option<Appearance>,
) -> Result<Settings, String> {
    let desktop = app.state::<Desktop>();
    let _saving = desktop.preferences.lock().unwrap();
    let mut saved = Preferences::load(&desktop.directory);
    saved.theme = theme.unwrap_or(saved.theme);
    saved.appearance = appearance.unwrap_or(saved.appearance);
    saved.save(&desktop.directory)?;
    let settings = Settings {
        theme: saved.theme,
        appearance: saved.appearance,
    };
    apply(app, settings);
    Ok(settings)
}

/// Applies the settings natively, to every window, shell, page, and the menu.
pub fn apply(app: &AppHandle, settings: Settings) {
    app.set_theme(settings.appearance.native());
    for window in app.windows().values() {
        paint(window, settings.theme);
        send_theme(window, settings.theme);
    }
    check_menu(app, settings);
    app.state::<Panes>()
        .broadcast(&json!({"type": "settings", "settings": settings}));
}

/// Hands the page its theme. A page that is loading or covered still takes it.
pub fn send_theme(window: &Window, theme: Theme) {
    let shown = window
        .app_handle()
        .state::<Panes>()
        .with(window.label(), |pane| page::on_runtime_document(pane))
        .unwrap_or(false);
    if let (true, Some(content)) = (shown, window.get_webview(&content_label(window.label()))) {
        let _ = content.eval(page::Command::Theme { theme }.script().unwrap_or_default());
    }
}

const TOKENS: &str = include_str!("../../../web/src/tokens.css");

/// The light and dark values of `name: light-dark(#rrggbb, #rrggbb)` in the
/// first rule of `tokens.css` whose selector ends with `selector`.
// ponytail: reads the token file's own format; the test fails if it changes.
fn token(selector: &str, name: &str) -> Option<(Color, Color)> {
    let rule = TOKENS
        .split(&format!("{selector} {{"))
        .nth(1)?
        .split('}')
        .next()?;
    let pair = rule
        .split(&format!("{name}: light-dark("))
        .nth(1)?
        .split(')')
        .next()?;
    let (light, dark) = pair.split_once(',')?;
    Some((hex(light)?, hex(dark)?))
}

fn hex(value: &str) -> Option<Color> {
    let digits = value.trim().strip_prefix('#')?;
    let channel = |at: usize| u8::from_str_radix(digits.get(at..at + 2)?, 16).ok();
    (digits.len() == 6).then_some(Color(channel(0)?, channel(2)?, channel(4)?, 255))
}

/// The theme's top-bar colour and the content panel colour for an appearance.
pub fn colors(theme: Theme, dark: bool) -> (Color, Color) {
    let pick = |pair: Option<(Color, Color)>| {
        pair.map(|(light, dark_value)| if dark { dark_value } else { light })
    };
    let chrome = pick(token(
        &format!(":root[data-theme=\"{}\"]", theme.id()),
        "--chrome-top",
    ));
    let surface = pick(token(":root", "--surface"));
    (
        chrome.unwrap_or(Color(59, 57, 54, 255)),
        surface.unwrap_or(Color(255, 255, 255, 255)),
    )
}

/// Whether a window renders dark: its own effective appearance.
pub fn dark(window: &Window) -> bool {
    window.theme().is_ok_and(|t| t == tauri::Theme::Dark)
}

/// Gives a window and its webviews the theme's background colours. Always a
/// colour: a webview given none falls back to white.
pub fn paint(window: &Window, theme: Theme) {
    let (chrome, surface) = colors(theme, dark(window));
    let _ = window.set_background_color(Some(chrome));
    if let Some(shell) = window.get_webview(&shell_label(window.label())) {
        let _ = shell.set_background_color(Some(chrome));
    }
    if let Some(content) = window.get_webview(&content_label(window.label())) {
        let _ = content.set_background_color(Some(surface));
    }
}

/// The View menu's Appearance and Theme submenus. Item ids carry the choice.
pub fn submenus(app: &AppHandle) -> tauri::Result<(Submenu<Wry>, Submenu<Wry>)> {
    let settings = current(&app.state::<Desktop>().directory);
    let mut checks = vec![];
    let mut appearance = vec![];
    for (value, label) in Appearance::ALL {
        let id = format!("appearance:{}", value.id());
        let item = CheckMenuItem::with_id(
            app,
            &id,
            label,
            true,
            value == settings.appearance,
            None::<&str>,
        )?;
        appearance.push(item.clone());
        checks.push((id, item));
    }
    let mut theme = vec![];
    for (value, label) in Theme::ALL {
        let id = format!("theme:{}", value.id());
        let item =
            CheckMenuItem::with_id(app, &id, label, true, value == settings.theme, None::<&str>)?;
        theme.push(item.clone());
        checks.push((id, item));
    }
    app.manage(MenuChecks(checks));
    fn refs(items: &[CheckMenuItem<Wry>]) -> Vec<&dyn IsMenuItem<Wry>> {
        items.iter().map(|i| i as &dyn IsMenuItem<Wry>).collect()
    }
    Ok((
        Submenu::with_items(app, "Appearance", true, &refs(&appearance))?,
        Submenu::with_items(app, "Theme", true, &refs(&theme))?,
    ))
}

struct MenuChecks(Vec<(String, CheckMenuItem<Wry>)>);

/// Marks the chosen appearance and theme; a click toggles only its own item.
fn check_menu(app: &AppHandle, settings: Settings) {
    let chosen = [
        format!("appearance:{}", settings.appearance.id()),
        format!("theme:{}", settings.theme.id()),
    ];
    if let Some(checks) = app.try_state::<MenuChecks>() {
        for (id, item) in &checks.0 {
            let _ = item.set_checked(chosen.contains(id));
        }
    }
}

/// Handles an Appearance or Theme menu item. Returns false for other items.
pub fn menu_event(app: &AppHandle, id: &str) -> bool {
    let Some((theme, appearance)) = menu_choice(id) else {
        return false;
    };
    if theme.is_some() || appearance.is_some() {
        let _ = update(app, theme, appearance);
    } else {
        check_menu(app, current(&app.state::<Desktop>().directory));
    }
    true
}

/// The setting an Appearance or Theme item id names; `None` for other items.
fn menu_choice(id: &str) -> Option<(Option<Theme>, Option<Appearance>)> {
    match id.split_once(':')? {
        ("theme", value) => Some((serde_json::from_value(json!(value)).ok(), None)),
        ("appearance", value) => Some((None, serde_json::from_value(json!(value)).ok())),
        _ => None,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn menu_items_name_the_setting_they_choose() {
        assert_eq!(
            menu_choice("theme:spruce"),
            Some((Some(Theme::Spruce), None))
        );
        assert_eq!(
            menu_choice("appearance:auto"),
            Some((None, Some(Appearance::Auto)))
        );
        assert_eq!(menu_choice("theme:teal"), Some((None, None)));
        assert_eq!(menu_choice("popup:window-1:m3"), None);
        assert_eq!(menu_choice("settings"), None);
    }

    #[test]
    fn every_theme_has_chrome_and_surface_colours_in_both_appearances() {
        for (theme, _) in Theme::ALL {
            let selector = format!(":root[data-theme=\"{}\"]", theme.id());
            assert!(token(&selector, "--chrome-top").is_some(), "{theme:?}");
        }
        assert!(token(":root", "--surface").is_some());
        assert_eq!(
            colors(Theme::Spruce, false),
            (Color(0x28, 0x3f, 0x3d, 255), Color(255, 255, 255, 255))
        );
        assert_eq!(
            colors(Theme::Spruce, true),
            (Color(0x1a, 0x2a, 0x28, 255), Color(0x1c, 0x1b, 0x1a, 255))
        );
    }

    #[test]
    fn settings_ids_round_trip_through_menu_ids() {
        assert_eq!(Theme::Oxblood.id(), "oxblood");
        assert_eq!(Appearance::Auto.id(), "auto");
        assert_eq!(Appearance::Dark.native(), Some(tauri::Theme::Dark));
        assert_eq!(Appearance::Auto.native(), None);
    }
}
