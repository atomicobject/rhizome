#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]
mod appearance;
mod bridge;
mod commands;
mod credentials;
mod finder;
mod host;
mod inbox;
mod installation;
#[cfg(target_os = "linux")]
mod linux_webviews;
#[cfg(target_os = "macos")]
mod login;
mod menu;
mod page;
mod pane;
mod pipeline;
mod presence;
mod process;
#[cfg(target_os = "macos")]
mod quit;
mod security;
mod sessions;
mod setup;
mod state;
mod tasks;
mod team;
mod terminal;
#[cfg(all(test, unix))]
mod test_support;
mod tray;
mod updater;
mod windows;
use tauri::Manager;

fn main() {
    startup_stage("main");
    let context = tauri::generate_context!();
    startup_stage("context ready");
    let arguments = std::env::args_os().skip(1).collect::<Vec<_>>();
    let integration = if installation::install::requested(&arguments) {
        Some(installation::install::run(
            &context.config().identifier,
            &context.package_info().version.to_string(),
            &arguments,
        ))
    } else if installation::uninstall::requested(&arguments) {
        Some(installation::uninstall::run(
            &context.config().identifier,
            &arguments,
        ))
    } else {
        None
    };
    if let Some(result) = integration {
        if let Err(message) = result {
            eprintln!("Cannot change Rhizome desktop integration: {message}");
            std::process::exit(1);
        }
        return;
    }
    let app = tauri::Builder::default()
        .plugin(tauri_plugin_single_instance::init(|app, args, _| {
            // A login launch while Rhizome already runs changes nothing.
            #[cfg(target_os = "macos")]
            if login::requested(&args) {
                return;
            }
            if !terminal::handle(app, &args) && !updater::handle(app, &args, false) {
                windows::reopen(app);
            }
        }))
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_clipboard_manager::init())
        .plugin(tauri_plugin_notification::init())
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_updater::Builder::new().build())
        .invoke_handler(tauri::generate_handler![
            commands::desktop_request,
            commands::desktop_attach
        ])
        .setup(|app| {
            startup_stage("setup started");
            installation::prepare(app.handle())?;
            startup_stage("installation prepared");
            let directory = state::data_directory(app.path().app_data_dir()?)?;
            std::fs::create_dir_all(&directory)?;
            let session = state::Session::load(&directory);
            app.manage(commands::Desktop::new(directory.clone()));
            app.manage(windows::Sessions::new(directory));
            app.manage(pane::Panes::default());
            app.manage(presence::Presence::default());
            app.manage(terminal::Focus::default());
            app.manage(updater::Updates::default());
            app.manage(credentials::Imports::default());
            app.manage(team::Team::default());
            app.manage(inbox::Inbox::default());
            inbox::listen(app.handle());
            menu::install_menu(app.handle())?;
            let settings = appearance::current(&app.state::<commands::Desktop>().directory);
            app.handle().set_theme(settings.appearance.native());
            let args = std::env::args().collect::<Vec<_>>();
            let restarted = updater::take_restart_marker();
            startup_stage("restoring windows");
            windows::restore(app.handle(), session, launch(&args, restarted))?;
            startup_stage("windows restored");
            terminal::handle(app.handle(), &args);
            updater::handle(app.handle(), &args, restarted);
            updater::start(app.handle());
            presence::start(app.handle().clone());
            #[cfg(target_os = "macos")]
            {
                app.manage(host::Host::default());
                tray::install(app.handle())?;
                host::start(app.handle().clone());
                // A launch that opened no window lives in the menu bar.
                quit::to_menu_bar(app.handle());
            }
            Ok(())
        })
        .build(context)
        .expect("Cannot launch Rhizome Desktop");
    startup_stage("app ready");
    app.run(|app, event| match event {
        #[cfg(target_os = "macos")]
        tauri::RunEvent::Reopen { .. } => windows::reopen(app),
        tauri::RunEvent::ExitRequested { code, api, .. } if keeps_running(code) => {
            // The last window closed: Rhizome stays available, from the menu
            // bar on macOS. Every other exit request proceeds.
            api.prevent_exit();
            #[cfg(target_os = "macos")]
            quit::to_menu_bar(app);
        }
        // Quitting does not close windows one by one, so the session keeps them.
        tauri::RunEvent::ExitRequested { .. } => quitting(app),
        // The native terminate path: logout, restart, shutdown, Dock Quit, an
        // updater restart, and the end of every quit. It cannot be
        // prevented and never asks; scheduled task hosting stops here.
        tauri::RunEvent::Exit => {
            quitting(app);
            host::terminate(app);
        }
        _ => {}
    });
}

fn quitting(app: &tauri::AppHandle) {
    app.state::<credentials::Imports>().cancel(None);
    app.state::<team::Team>().cancel(None);
    windows::quit(app);
}

/// Only closing the last window, on macOS, leaves the app running; an exit
/// the app or the OS requested is never prevented (SPEC-0104 US7).
fn keeps_running(code: Option<i32>) -> bool {
    cfg!(target_os = "macos") && code.is_none()
}

/// A login launch opens no window. After an updater restart, the windows
/// that were open come back, but none is forced. Elsewhere the app has no
/// menu bar host, so every launch opens windows.
#[cfg_attr(not(target_os = "macos"), allow(unused_variables))]
fn launch(args: &[String], restarted: bool) -> windows::Launch {
    #[cfg(target_os = "macos")]
    if login::requested(args) {
        return windows::Launch::Login;
    }
    match restarted && cfg!(target_os = "macos") {
        true => windows::Launch::Restarted,
        false => windows::Launch::Open,
    }
}

fn startup_stage(stage: &str) {
    if std::env::var_os("RHIZOME_DESKTOP_STARTUP_TRACE").is_none() {
        return;
    }
    static START: std::sync::OnceLock<std::time::Instant> = std::sync::OnceLock::new();
    let elapsed = START.get_or_init(std::time::Instant::now).elapsed();
    eprintln!("Rhizome startup +{}ms: {stage}", elapsed.as_millis());
}

#[cfg(test)]
mod tests {
    #[test]
    fn only_the_last_window_closing_keeps_the_app_running() {
        assert_eq!(super::keeps_running(None), cfg!(target_os = "macos"));
        // app.exit, app.restart (the updater), and Quit Completely.
        for code in [0, 1, i32::MAX] {
            assert!(!super::keeps_running(Some(code)));
        }
    }
}
