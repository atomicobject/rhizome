//! Draws the menu bar model as a macOS status item and carries out its
//! commands.
use super::{model, tomorrow, Entry, Model};
use crate::{commands::Desktop, host::Host, state::Preferences, windows};
use std::{sync::Mutex, time::Duration};
use tauri::{
    image::Image,
    menu::{IsMenuItem, Menu, MenuItem, MenuItemKind, PredefinedMenuItem, Submenu},
    tray::TrayIconBuilder,
    AppHandle, Manager, Wry,
};
use tauri_plugin_dialog::DialogExt;

const ID: &str = "host";
const ICON: &[u8] = include_bytes!("../../icons/menu-bar-template.png");
const REQUEST: Duration = Duration::from_secs(10);

/// The model the menu shows now. `drawing` serializes redraws; it is never
/// taken on the main thread after launch, which builds the menu items.
#[derive(Default)]
struct Shown {
    model: Mutex<Model>,
    drawing: Mutex<()>,
}

pub fn install(app: &AppHandle) -> tauri::Result<()> {
    app.manage(Shown::default());
    TrayIconBuilder::with_id(ID)
        .icon(Image::from_bytes(ICON)?)
        .icon_as_template(true)
        .tooltip(&app.package_info().name)
        .show_menu_on_left_click(true)
        .on_menu_event(|app, event| dispatch(app, event.id().as_ref()))
        .build(app)?;
    refresh(app);
    Ok(())
}

fn current(app: &AppHandle) -> Model {
    let host = app.state::<Host>();
    let keep = Preferences::load(&app.state::<Desktop>().directory)
        .keep_in_menu_bar
        .0;
    model(
        &host.views(),
        host.listing_problem().as_deref(),
        keep,
        crate::quit::waiting(),
        chrono::Local::now(),
    )
}

/// Redraws the menu when what it shows changed. An unchanged menu is left
/// alone, so a status update does not close it while it is open.
pub fn refresh(app: &AppHandle) {
    let (Some(tray), Some(shown)) = (app.tray_by_id(ID), app.try_state::<Shown>()) else {
        return;
    };
    let _drawing = shown.drawing.lock().unwrap();
    let next = current(app);
    if *shown.model.lock().unwrap() == next {
        return;
    }
    let menu = items(app, &next.entries).and_then(|items| {
        let items: Vec<&dyn IsMenuItem<Wry>> =
            items.iter().map(|i| i as &dyn IsMenuItem<Wry>).collect();
        Menu::with_items(app, &items)
    });
    match menu {
        Ok(menu) => {
            let _ = tray.set_menu(Some(menu));
            let _ = tray.set_title(next.title.as_deref());
            *shown.model.lock().unwrap() = next;
        }
        Err(error) => eprintln!("Rhizome Desktop could not draw the menu bar menu: {error}"),
    }
}

fn items(app: &AppHandle, entries: &[Entry]) -> tauri::Result<Vec<MenuItemKind<Wry>>> {
    let mut built = vec![];
    for entry in entries {
        built.push(match entry {
            Entry::Separator => MenuItemKind::Predefined(PredefinedMenuItem::separator(app)?),
            Entry::Text(text) => {
                MenuItemKind::MenuItem(MenuItem::new(app, text, false, None::<&str>)?)
            }
            Entry::Item { id, label } => {
                MenuItemKind::MenuItem(MenuItem::with_id(app, id, label, true, None::<&str>)?)
            }
            Entry::Submenu {
                label,
                items: children,
            } => {
                let children = items(app, children)?;
                let children: Vec<&dyn IsMenuItem<Wry>> =
                    children.iter().map(|c| c as &dyn IsMenuItem<Wry>).collect();
                MenuItemKind::Submenu(Submenu::with_items(app, label, true, &children)?)
            }
        });
    }
    Ok(built)
}

/// Brings Rhizome forward: the stashed windows, else an open or new window.
pub fn open(app: &AppHandle) {
    let _ = app.show();
    windows::reopen(app);
}

fn dispatch(app: &AppHandle, id: &str) {
    let Some(action) = id.strip_prefix("tray:") else {
        return;
    };
    match action {
        "open" => open(app),
        "quit" => crate::quit::quit_completely(app),
        "resume" => each(app, "resume tasks", |api| async move {
            api.resume(REQUEST).await
        }),
        "pause-hour" => {
            let until = (chrono::Local::now() + chrono::Duration::hours(1)).to_rfc3339();
            each(app, "pause tasks", move |api| {
                let until = until.clone();
                async move { api.pause(&until, REQUEST).await }
            });
        }
        "pause-tomorrow" => {
            if let Some(until) = tomorrow(chrono::Local::now()) {
                each(app, "pause tasks", move |api| {
                    let until = until.clone();
                    async move { api.pause(&until, REQUEST).await }
                });
            }
        }
        run => {
            let index = run
                .strip_prefix("run:")
                .and_then(|n| n.parse::<usize>().ok());
            let chosen = index.and_then(|n| {
                app.state::<Shown>()
                    .model
                    .lock()
                    .unwrap()
                    .runs
                    .get(n)
                    .cloned()
            });
            let Some((key, task)) = chosen else {
                return;
            };
            let Some(api) = app.state::<Host>().api(&key) else {
                return;
            };
            let app = app.clone();
            tauri::async_runtime::spawn(async move {
                if let Err(message) = api.run_now(&task, REQUEST).await {
                    tell(&app, format!("Rhizome could not run {task}: {message}"));
                }
            });
        }
    }
}

/// Sends one request to every hosted runtime, reporting the first failure.
fn each<F, Fut>(app: &AppHandle, what: &'static str, request: F)
where
    F: Fn(crate::host::Api) -> Fut + Send + 'static,
    Fut: std::future::Future<Output = Result<(), String>> + Send,
{
    let app = app.clone();
    tauri::async_runtime::spawn(async move {
        for api in app.state::<Host>().apis() {
            if let Err(message) = request(api).await {
                tell(&app, format!("Rhizome could not {what}: {message}"));
                return;
            }
        }
    });
}

fn tell(app: &AppHandle, message: String) {
    app.dialog().message(message).title("Rhizome").show(|_| {});
}
