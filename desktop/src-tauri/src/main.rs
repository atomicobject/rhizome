#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]
mod appearance;
mod bridge;
mod commands;
mod credentials;
mod finder;
mod inbox;
mod installation;
#[cfg(target_os = "linux")]
mod linux_webviews;
mod menu;
mod page;
mod pane;
mod pipeline;
mod presence;
mod process;
mod security;
mod setup;
mod state;
mod tasks;
mod team;
mod terminal;
#[cfg(all(test, unix))]
mod test_support;
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
            startup_stage("restoring windows");
            windows::restore(app.handle(), session)?;
            startup_stage("windows restored");
            let args = std::env::args().collect::<Vec<_>>();
            terminal::handle(app.handle(), &args);
            let restarted = updater::take_restart_marker();
            updater::handle(app.handle(), &args, restarted);
            updater::start(app.handle());
            presence::start(app.handle().clone());
            Ok(())
        })
        .build(context)
        .expect("Cannot launch Rhizome Desktop");
    startup_stage("app ready");
    app.run(|app, event| match event {
        #[cfg(target_os = "macos")]
        tauri::RunEvent::Reopen { .. } => windows::reopen(app),
        tauri::RunEvent::ExitRequested {
            code: None, api, ..
        } => {
            // Closing every window keeps the application available from the Dock.
            #[cfg(target_os = "macos")]
            api.prevent_exit();
            #[cfg(not(target_os = "macos"))]
            let _ = api;
        }
        // Quitting does not close windows one by one, so the session keeps them.
        tauri::RunEvent::ExitRequested { .. } | tauri::RunEvent::Exit => {
            app.state::<credentials::Imports>().cancel(None);
            app.state::<team::Team>().cancel(None);
            windows::quit(app);
        }
        _ => {}
    });
}

fn startup_stage(stage: &str) {
    if std::env::var_os("RHIZOME_DESKTOP_STARTUP_TRACE").is_none() {
        return;
    }
    static START: std::sync::OnceLock<std::time::Instant> = std::sync::OnceLock::new();
    let elapsed = START.get_or_init(std::time::Instant::now).elapsed();
    eprintln!("Rhizome startup +{}ms: {stage}", elapsed.as_millis());
}
