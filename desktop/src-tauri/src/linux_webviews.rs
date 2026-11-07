//! GTK owns Linux webview layout. Wry's Wayland set_bounds does not position
//! views created in Tauri's default GtkBox, so they must share an overlay host.
use gtk::prelude::*;
use tauri::{Rect, Webview};

pub fn mount_shell(shell: &Webview) -> tauri::Result<()> {
    shell.with_webview(|platform| {
        let widget = platform.inner();
        let parent = widget.parent().expect("shell GTK parent");
        let parent = parent.downcast::<gtk::Box>().expect("Tauri GTK box");
        for child in parent.children() {
            if let Ok(bar) = child.downcast::<gtk::MenuBar>() {
                prepare_hidden_menu(&bar);
            }
        }
        if let Some(window) = widget
            .toplevel()
            .and_then(|w| w.downcast::<gtk::Window>().ok())
        {
            // Run app accelerators before WebKit's focused-widget key handler.
            window.connect_key_press_event(|window, event| {
                if activate_shortcut(window, *event.keyval(), event.state()) {
                    gtk::glib::Propagation::Stop
                } else {
                    gtk::glib::Propagation::Proceed
                }
            });
        }
        let host = gtk::Overlay::new();
        parent.remove(&widget);
        host.add(&widget);
        parent.pack_start(&host, true, true, 0);
        host.show();
    })
}

pub fn mount_overlay(view: &Webview) -> tauri::Result<()> {
    view.with_webview(|platform| {
        let widget = platform.inner();
        let parent = widget.parent().expect("overlay GTK parent");
        if parent.is::<gtk::Overlay>() {
            return;
        }
        let parent = parent.downcast::<gtk::Box>().expect("Tauri GTK box");
        let host = parent
            .children()
            .into_iter()
            .find_map(|child| child.downcast::<gtk::Overlay>().ok())
            .expect("shell overlay host");
        parent.remove(&widget);
        widget.set_halign(gtk::Align::Fill);
        widget.set_valign(gtk::Align::Fill);
        // Overlay children do not constrain the window's minimum size.
        widget.set_size_request(1, 1);
        host.add_overlay(&widget);
        host.set_overlay_pass_through(&widget, false);
    })
}

pub fn layout(content: &Webview, bounds: Rect) -> tauri::Result<()> {
    // DOM CSS pixels and GTK widget allocations use the same logical units;
    // do not apply the monitor's physical scale a second time.
    let position = bounds.position.to_logical::<i32>(1.0);
    content.with_webview(move |platform| {
        let widget = platform.inner();
        widget.set_margin_start(position.x.max(0));
        widget.set_margin_top(position.y.max(0));
        // The content region extends to the bottom/right. GTK follows window
        // resizing immediately, even before the shell's next layout report.
        widget.set_margin_end(0);
        widget.set_margin_bottom(0);
    })
}

fn activate_shortcut(window: &gtk::Window, key: u32, modifiers: gtk::gdk::ModifierType) -> bool {
    use gtk::gdk::{keys::constants, ModifierType};
    let modifiers = modifiers & gtk::accelerator_get_default_mod_mask();
    let lower = gtk::gdk::keys::Key::from(key).to_lower();
    // Let the focused editor implement its own select/copy/paste/undo history.
    // Every other key still needs a matching registered accelerator to stop.
    if modifiers == ModifierType::CONTROL_MASK
        && [
            constants::a,
            constants::c,
            constants::v,
            constants::x,
            constants::y,
            constants::z,
        ]
        .contains(&lower)
    {
        return false;
    }
    gtk::accel_groups_activate(window, *lower, modifiers)
}

fn prepare_hidden_menu(bar: &gtk::MenuBar) {
    // GTK normally requires mapped ancestors before activating a menu item.
    // The registered menu is deliberately hidden; only sensitivity should
    // gate its accelerators. The window's accel group still owns dispatch.
    fn allow_shortcuts(widget: &gtk::Widget) {
        if let Some(item) = widget.downcast_ref::<gtk::MenuItem>() {
            item.connect_can_activate_accel(|item, _| item.is_sensitive());
            if let Some(submenu) = item.submenu() {
                allow_shortcuts(&submenu);
            }
        }
        if let Some(container) = widget.downcast_ref::<gtk::Container>() {
            for child in container.children() {
                allow_shortcuts(&child);
            }
        }
    }
    allow_shortcuts(bar.upcast_ref());
    // GtkWindow::show_all must not bring the menu row back on terminal opens.
    bar.set_no_show_all(true);
    bar.hide();
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::{cell::Cell, rc::Rc};

    #[test]
    fn hidden_menu_keeps_shortcuts_and_respects_disabled_items() {
        if std::env::var("RZM_GTK_SHORTCUT_TEST").as_deref() != Ok("1") {
            return;
        }
        gtk::init().expect("native GTK display");
        let window = gtk::Window::new(gtk::WindowType::Toplevel);
        let container = gtk::Box::new(gtk::Orientation::Vertical, 0);
        window.add(&container);
        let group = gtk::AccelGroup::new();
        window.add_accel_group(&group);
        let bar = gtk::MenuBar::new();
        container.pack_start(&bar, false, false, 0);
        let file = gtk::MenuItem::with_label("File");
        let submenu = gtk::Menu::new();
        file.set_submenu(Some(&submenu));
        bar.append(&file);
        let fired = Rc::new(Cell::new(0));
        let item = gtk::MenuItem::with_label("Switch Repository");
        let calls = fired.clone();
        item.connect_activate(move |_| calls.set(calls.get() + 1));
        item.add_accelerator(
            "activate",
            &group,
            *gtk::gdk::keys::constants::o,
            gtk::gdk::ModifierType::CONTROL_MASK,
            gtk::AccelFlags::VISIBLE,
        );
        submenu.append(&item);
        container.show_all();
        bar.hide();
        let activate = || {
            activate_shortcut(
                &window,
                *gtk::gdk::keys::constants::o,
                gtk::gdk::ModifierType::CONTROL_MASK,
            )
        };
        assert!(!activate(), "reproduce GTK rejecting a hidden ancestor");
        prepare_hidden_menu(&bar);
        assert!(activate(), "Ctrl+O must activate the hidden menu item");
        assert_eq!(fired.get(), 1);
        let copy = gtk::MenuItem::with_label("Copy");
        copy.add_accelerator(
            "activate",
            &group,
            *gtk::gdk::keys::constants::c,
            gtk::gdk::ModifierType::CONTROL_MASK,
            gtk::AccelFlags::VISIBLE,
        );
        submenu.append(&copy);
        assert!(
            !activate_shortcut(
                &window,
                *gtk::gdk::keys::constants::c,
                gtk::gdk::ModifierType::CONTROL_MASK
            ),
            "editor copy must not be intercepted"
        );
        item.set_sensitive(false);
        assert!(!activate(), "disabled actions must stay disabled");
        assert_eq!(fired.get(), 1);
        container.show_all();
        assert!(
            !bar.is_visible(),
            "bringing a window forward must not reveal its menu bar"
        );
        window.close();
    }
}
