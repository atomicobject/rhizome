fn main() {
    tauri_build::try_build(tauri_build::Attributes::new().app_manifest(
        tauri_build::AppManifest::new().commands(&["desktop_request", "desktop_attach"]),
    ))
    .expect("failed to build desktop resources");
}
