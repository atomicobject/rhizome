#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]
mod bridge;
mod commands;
mod pane;
mod pipeline;
mod presence;
mod security;
mod state;
mod terminal;
mod windows;
use tauri::Manager;

fn main() {
    let app = tauri::Builder::default()
        .plugin(tauri_plugin_single_instance::init(|app, args, _| {
            if !terminal::handle(app, &args) {
                windows::reopen(app);
            }
        }))
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_opener::init())
        .invoke_handler(tauri::generate_handler![
            commands::desktop_request,
            commands::desktop_attach
        ])
        .setup(|app| {
            let directory = state::data_directory(app.path().app_data_dir()?)?;
            std::fs::create_dir_all(&directory)?;
            let session = state::Session::load(&directory);
            app.manage(commands::Desktop::new(directory.clone()));
            app.manage(windows::Sessions::new(directory));
            app.manage(pane::Panes::default());
            app.manage(presence::Presence::default());
            app.manage(terminal::Focus::default());
            windows::install_menu(app.handle())?;
            windows::restore(app.handle(), session)?;
            terminal::handle(app.handle(), &std::env::args().collect::<Vec<_>>());
            presence::start(app.handle().clone());
            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("Cannot launch Rhizome Desktop");
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
            windows::quit(app);
        }
        _ => {}
    });
}
