use super::*;

#[test]
fn shell_menu_entries_deserialize() {
    let entries: Vec<MenuEntry> = serde_json::from_str(
        r#"[{"label":"Primary worktree","disabled":false,"id":"m0","items":[
            {"label":"Automatic (main)","checked":true,"id":"m1"},
            {"separator":true,"id":"m2"}]},
           {"label":"Remove from Library","id":"m3"}]"#,
    )
    .unwrap();
    assert_eq!(entries[0].items.len(), 2);
    assert_eq!(entries[0].items[0].checked, Some(true));
    assert!(entries[0].items[1].separator);
    assert_eq!(entries[1].id, "m3");
    assert!(serde_json::from_str::<Vec<MenuEntry>>(r#"[{"label":"x","run":1}]"#).is_err());
}

#[test]
fn the_sidebar_item_names_what_it_will_do() {
    assert_eq!(sidebar_label(false), "Hide Sidebar");
    assert_eq!(sidebar_label(true), "Show Sidebar");
    #[cfg(target_os = "macos")]
    assert_eq!(SIDEBAR_SHORTCUT, "Ctrl+CmdOrCtrl+S");
}
