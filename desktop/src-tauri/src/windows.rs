pub use crate::sessions::{number, Geometry, Launch, Sessions};
use crate::{
    appearance,
    commands::Desktop,
    page,
    pane::{Pane, Panes},
    presence::Presence,
    security,
    state::{Preferences, Session, WindowSession},
    terminal::Focus,
};
use serde::Deserialize;
use std::sync::atomic::{AtomicU64, Ordering};
use tauri::{
    webview::{NewWindowResponse, PageLoadEvent, WebviewBuilder},
    window::WindowBuilder,
    AppHandle, LogicalPosition, LogicalSize, Manager, Rect, Url, WebviewUrl, Window, WindowEvent,
};
use tauri_plugin_opener::OpenerExt;

const WIDTH: f64 = 1320.0;
const HEIGHT: f64 = 860.0;
const MIN_WIDTH: f64 = if cfg!(target_os = "linux") {
    100.0
} else {
    760.0
};

static NEXT: AtomicU64 = AtomicU64::new(1);

pub fn shell_label(window: &str) -> String {
    window.replacen("window-", "shell-", 1)
}

pub fn content_label(window: &str) -> String {
    window.replacen("window-", "content-", 1)
}

fn record(app: &AppHandle, window: &Window) {
    if let Some(geometry) = Geometry::read(window) {
        app.state::<Sessions>()
            .update(app, window.label(), |saved| geometry.apply(saved));
    }
}

/// Saves the session on quit, including windows that were never moved.
pub fn quit(app: &AppHandle) {
    app.state::<Sessions>().quit(crate::sessions::geometry(app));
}

/// Opens the windows `launch` restores from the last session: all of them,
/// or one new window, unless the launch keeps them for the next opener.
pub fn restore(app: &AppHandle, session: Session, launch: Launch) -> tauri::Result<()> {
    for window in app.state::<Sessions>().begin(session, launch) {
        create(app, window)?;
    }
    Ok(())
}

/// Quit Rhizome: saves every open window as the session to resume, then
/// closes them. The menu bar host keeps running.
#[cfg(target_os = "macos")]
pub fn stash(app: &AppHandle) {
    app.state::<Sessions>()
        .stash(crate::sessions::geometry(app));
    for window in app.windows().into_values() {
        let _ = window.close();
    }
}

/// Restores every window Quit Rhizome stashed, or a launch kept, and returns
/// them; none when nothing waits. Every window opener calls this first.
pub fn resume(app: &AppHandle) -> Vec<Window> {
    let mut opened = vec![];
    for session in app.state::<Sessions>().unstash() {
        match create(app, session) {
            Ok(window) => opened.push(window),
            Err(error) => eprintln!("Rhizome Desktop could not restore a window: {error}"),
        }
    }
    opened
}

/// [`resume`] from an event callback, which cannot create windows itself.
/// Returns whether any windows were waiting.
pub fn resume_from_event(app: &AppHandle) -> bool {
    let stashed = app.state::<Sessions>().unstash();
    let resumed = !stashed.is_empty();
    for session in stashed {
        create_from_event(app, session);
    }
    resumed
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
    // A window brings back the Dock icon the menu bar host dropped.
    #[cfg(target_os = "macos")]
    app.set_activation_policy(tauri::ActivationPolicy::Regular)?;
    let label = format!("window-{}", NEXT.fetch_add(1, Ordering::SeqCst));
    let width = session.width.unwrap_or(WIDTH).max(MIN_WIDTH);
    let height = session.height.unwrap_or(HEIGHT).max(520.0);
    let mut builder = WindowBuilder::new(app, &label)
        .title(&app.package_info().name)
        .inner_size(width, height)
        .min_inner_size(MIN_WIDTH, 520.0);
    #[cfg(target_os = "macos")]
    {
        builder = builder
            .title_bar_style(tauri::TitleBarStyle::Overlay)
            .hidden_title(true);
    }
    #[cfg(windows)]
    {
        builder = builder.visible(false);
    }
    if let (Some(x), Some(y)) = (session.x, session.y) {
        if on_screen(app, x + 40.0, y + 20.0) {
            builder = builder.position(x, y);
        }
    }
    let settings = appearance::current(&app.state::<Desktop>().directory);
    let window = builder.build()?;
    crate::startup_stage("native window created");
    // Keep GTK accelerators registered, but expose the menu through the shell.
    #[cfg(target_os = "linux")]
    window.hide_menu()?;
    // Before any webview paints: the theme's chrome behind the title bar, and
    // the content panel's colour under the page, in the window's appearance.
    let (chrome, surface) = appearance::colors(settings.theme, appearance::dark(&window));
    window.set_background_color(Some(chrome))?;
    app.state::<Sessions>()
        .update(app, &label, |saved| *saved = session);
    record(app, &window);
    let pane = Pane::default();
    let expected = pane.expected.clone();
    app.state::<Panes>().insert(label.clone(), pane);

    let shell = shell_label(&label);
    let shell_view = window.add_child(
        WebviewBuilder::new(&shell, WebviewUrl::App("index.html".into()))
            .disable_drag_drop_handler()
            .auto_resize()
            // The shell paints the saved theme before it can ask for settings.
            .initialization_script(format!(
                "window.__RHIZOME_SHELL_SETTINGS__ = {}; window.__RHIZOME_MACOS__ = {}; window.__RHIZOME_PLATFORM__ = {};",
                serde_json::json!(settings),
                cfg!(target_os = "macos"),
                serde_json::json!(std::env::consts::OS)
            ))
            .background_color(chrome)
            .on_navigation(move |url| {
                security::shell_authorized(&shell, url, cfg!(debug_assertions))
            }),
        LogicalPosition::new(0.0, 0.0),
        LogicalSize::new(width, height),
    )?;
    crate::startup_stage("shell webview created");
    crate::menu::install_shortcuts(&shell_view).inspect_err(|_| discard_failed_window(&window))?;
    #[cfg(target_os = "linux")]
    crate::linux_webviews::mount_shell(&shell_view)
        .inspect_err(|_| discard_failed_window(&window))?;
    let popup_app = app.clone();
    let mut view = WebviewBuilder::new(
        content_label(&label),
        WebviewUrl::External("about:blank".parse().unwrap()),
    )
    .initialization_script(CLICK_GUARD)
    .initialization_script(page::MARKER)
    // The page's first paint takes the theme current when the window opened,
    // stamped so a newer theme the page remembers wins on a reload
    // (web/index.html); later changes arrive as page commands.
    .initialization_script(format!(
        "window.__RHIZOME_DESKTOP_THEME__ = {}; window.__RHIZOME_DESKTOP_THEME_AT__ = {};",
        serde_json::json!(settings.theme),
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .map_or(0, |since| since.as_millis())
    ))
    .background_color(surface);
    #[cfg(windows)]
    {
        view = view.focused(false);
    }
    // Scripts are fixed when the view is created, so a changed setting
    // reaches only windows opened afterward.
    #[cfg(target_os = "linux")]
    {
        view = view.initialization_script(page::LINUX_SCROLL_CANVAS);
    }
    if page::profiling_for(app).enabled {
        view = view.initialization_script(page::RENDER_PROFILING);
    }
    let content = window.add_child(
        view
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
            // The page reports the state the toolbar shows through its title.
            .on_document_title_changed(|webview, title| {
                let app = webview.app_handle();
                let (first, toggled) = app
                    .state::<Panes>()
                    .with(webview.window().label(), |pane| page::titled(pane, &title))
                    .unwrap_or_default();
                // Each runtime document takes the current theme and sidebar
                // state once it reports.
                if first {
                    let saved = Preferences::load(&app.state::<Desktop>().directory);
                    for command in [
                        page::Command::Theme { theme: saved.theme },
                        page::Command::Sidebar {
                            collapsed: saved.sidebar_collapsed,
                        },
                    ] {
                        let _ = webview.eval(command.script().unwrap_or_default());
                    }
                }
                if let Some(collapsed) = toggled {
                    page::sidebar_reported(app, collapsed);
                }
                // A page waiting on its first report shows now, with the
                // theme and sidebar it was just sent.
                if first {
                    let window = webview.window();
                    let label = window.label();
                    if let Some(generation) = app
                        .state::<Panes>()
                        .with(label, |pane| pane.first_report())
                        .flatten()
                    {
                        crate::pipeline::reveal(app, label, generation);
                    }
                }
                let label = webview.window().label().to_string();
                if let Some(ask) = app
                    .state::<Panes>()
                    .with(&label, |pane| pane.asked.take())
                    .flatten()
                {
                    crate::tasks::handle(app, &label, ask);
                }
                crate::inbox::reported(app, webview.window().label());
                crate::menu::refresh_panels(app);
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
                    webview
                        .app_handle()
                        .state::<Panes>()
                        .with(webview.window().label(), |pane| {
                            page::loading(pane, payload.url())
                        });
                    crate::inbox::refresh(webview.app_handle());
                    crate::menu::refresh_panels(webview.app_handle());
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
    crate::startup_stage("content webview created");
    #[cfg(target_os = "linux")]
    crate::linux_webviews::mount_overlay(&content)?;
    content.hide()?;
    crate::menu::install_shortcuts(&content).inspect_err(|_| discard_failed_window(&window))?;

    let app = app.clone();
    let events = window.clone();
    window.on_window_event(move |event| match event {
        WindowEvent::Focused(true) => {
            app.state::<Focus>().focused(events.label());
            app.state::<Presence>().rediscover();
            if let Some(host) = app.try_state::<crate::host::Host>() {
                host.refresh();
            }
            crate::menu::refresh_panels(&app);
        }
        WindowEvent::Moved(_) | WindowEvent::Resized(_) => record(&app, &events),
        // Match System follows the OS; repaint in the new appearance.
        WindowEvent::ThemeChanged(_) => appearance::paint(
            &events,
            appearance::current(&app.state::<Desktop>().directory).theme,
        ),
        WindowEvent::CloseRequested { .. } => {
            crate::inbox::closing(&app, events.label());
            app.state::<Sessions>()
                .close(events.label(), Geometry::read(&events));
        }
        WindowEvent::Destroyed => {
            app.state::<Panes>().remove(events.label());
            crate::inbox::refresh(&app);
            app.state::<Focus>().closed(events.label());
            app.state::<crate::team::Team>()
                .cancel(Some(events.label()));
            app.state::<crate::credentials::Imports>()
                .cancel(Some(events.label()));
        }
        _ => {}
    });
    #[cfg(windows)]
    {
        window.show()?;
        window.set_focus()?;
        shell_view.set_focus()?;
    }
    crate::startup_stage("window ready");
    Ok(window)
}

fn discard_failed_window(window: &Window) {
    let app = window.app_handle();
    app.state::<Panes>().remove(window.label());
    app.state::<Sessions>().close(window.label(), None);
    if let Err(error) = window.destroy() {
        eprintln!("Rhizome Desktop could not discard a failed window: {error}");
    }
}

/// Applies the shell's content region and whether the shell is drawing over it.
pub fn layout(window: &Window, bounds: Rect, covered: bool) -> Result<(), String> {
    let content = window
        .get_webview(&content_label(window.label()))
        .ok_or("This window has no content view.")?;
    #[cfg(target_os = "linux")]
    crate::linux_webviews::layout(&content, bounds).map_err(|e| e.to_string())?;
    #[cfg(not(target_os = "linux"))]
    content.set_bounds(bounds).map_err(|e| e.to_string())?;
    window
        .app_handle()
        .state::<Panes>()
        .with(window.label(), |pane| pane.cover(covered));
    let synced = sync_visibility(window);
    crate::menu::refresh_panels(window.app_handle());
    synced
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

/// The address of the runtime page the content view shows, while it is visible.
pub fn visible_page_url(window: &Window) -> Option<Url> {
    page_url_if(window, |pane| pane.visible())
}

/// The address of the content view's page while it holds `worktree`'s runtime.
pub fn worktree_page_url(window: &Window, worktree: &str) -> Option<Url> {
    page_url_if(window, |pane| pane.shows(worktree))
}

fn page_url_if(window: &Window, shown: impl FnOnce(&mut Pane) -> bool) -> Option<Url> {
    let panes = window.app_handle().state::<Panes>();
    if !panes.with(window.label(), shown)? {
        return None;
    }
    window
        .get_webview(&content_label(window.label()))?
        .url()
        .ok()
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

/// Hands a toolbar command to the workspace page while the content view shows
/// one on the verified runtime origin; otherwise there is no page to receive it.
pub fn page_command(window: &Window, command: &page::Command) {
    let shown = window
        .app_handle()
        .state::<Panes>()
        .with(window.label(), |pane| match command {
            page::Command::Pane {} | page::Command::Chat {} => page::toggles_pane(pane),
            _ => page::on_runtime(pane),
        })
        .unwrap_or(false);
    let content = window.get_webview(&content_label(window.label()));
    if let (true, Some(content), Some(script)) = (shown, content, command.script()) {
        let _ = content.eval(script);
    }
}

pub fn focused_window(app: &AppHandle) -> Option<Window> {
    let windows = app.windows();
    windows
        .values()
        .find(|w| w.is_focused().unwrap_or(false))
        .or_else(|| windows.values().next())
        .cloned()
}

/// Brings an existing window forward, or opens one when none remain: the
/// stashed session when one waits, else the last window closed.
pub fn reopen(app: &AppHandle) {
    if resume_from_event(app) {
        return;
    }
    match focused_window(app) {
        Some(window) => {
            let _ = window.unminimize();
            let _ = window.set_focus();
        }
        None => {
            create_from_event(app, app.state::<Sessions>().last_closed());
        }
    }
}

/// Windows child webviews cannot be created inside a synchronous event callback.
pub fn create_from_event(app: &AppHandle, session: WindowSession) {
    let app = app.clone();
    tauri::async_runtime::spawn_blocking(move || {
        if let Err(error) = create(&app, session) {
            eprintln!("Rhizome Desktop could not create a window: {error}");
        }
    });
}
