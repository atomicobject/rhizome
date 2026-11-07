use super::*;

fn fixture() -> (tempfile::TempDir, PathBuf, PathBuf, PathBuf) {
    let directory = tempfile::tempdir().unwrap();
    let app = directory.path().join("Rhizome.AppImage");
    let source = directory.path().join("rzm");
    fs::write(&app, "app").unwrap();
    fs::write(&source, "version one").unwrap();
    let root = directory.path().join("com.atomicobject.rhizome");
    (directory, root, app, source)
}

#[test]
fn materialized_image_survives_source_removal_and_rejects_tampering() {
    let (_directory, root, app, source) = fixture();
    let prepared = prepare_image(
        root.clone(),
        app.clone(),
        "com.atomicobject.rhizome".into(),
        "1.0.0".into(),
        &source,
        None,
    )
    .unwrap();
    fs::remove_file(&source).unwrap();
    assert_eq!(fs::read(&prepared.image).unwrap(), b"version one");
    fs::write(&source, "version one").unwrap();
    fs::write(&prepared.image, "tampered").unwrap();
    assert!(prepare_image(
        root,
        app,
        "com.atomicobject.rhizome".into(),
        "1.0.0".into(),
        &source,
        None,
    )
    .unwrap_err()
    .contains("changed"));
}

#[test]
fn startup_cannot_roll_back_registration_or_reenable_a_tombstone() {
    let (_directory, root, app, source) = fixture();
    let first = prepare_image(
        root.clone(),
        app.clone(),
        "com.atomicobject.rhizome".into(),
        "1.0.0".into(),
        &source,
        None,
    )
    .unwrap();
    fs::write(&source, "version two").unwrap();
    let second = prepare_image(
        root.clone(),
        app.clone(),
        "com.atomicobject.rhizome".into(),
        "1.1.0".into(),
        &source,
        None,
    )
    .unwrap();
    assert!(second.generation.load(Ordering::Acquire) > first.generation.load(Ordering::Acquire));
    fs::write(&source, "version one").unwrap();
    prepare_image(
        root.clone(),
        app.clone(),
        "com.atomicobject.rhizome".into(),
        "1.0.0".into(),
        &source,
        None,
    )
    .unwrap();
    let mut record = read_registration(&root).unwrap().unwrap();
    assert_eq!(record.version, "1.1.0");
    record.enabled = false;
    record.generation += 1;
    write_registration(&root, &record).unwrap();
    fs::write(&source, "version three").unwrap();
    prepare_image(
        root.clone(),
        app,
        "com.atomicobject.rhizome".into(),
        "1.2.0".into(),
        &source,
        None,
    )
    .unwrap();
    let after = read_registration(&root).unwrap().unwrap();
    assert!(!after.enabled);
    assert_eq!(after.generation, record.generation);
}

#[cfg(unix)]
#[test]
fn aliased_configuration_keeps_cli_entry_ownership_after_update() {
    let (directory, root, app, source) = fixture();
    let alias = directory.path().join("linked-config");
    std::os::unix::fs::symlink(directory.path(), &alias).unwrap();
    let aliased_root = alias.join(root.file_name().unwrap());
    let first = prepare_image(
        aliased_root.clone(),
        app.clone(),
        "com.atomicobject.rhizome".into(),
        "1.0.0".into(),
        &source,
        None,
    )
    .unwrap();
    let entry = directory.path().join("terminal-rzm");
    std::os::unix::fs::symlink(&first.image, &entry).unwrap();
    fs::write(&source, "version two").unwrap();
    let second = prepare_image(
        aliased_root,
        app,
        "com.atomicobject.rhizome".into(),
        "1.1.0".into(),
        &source,
        None,
    )
    .unwrap();
    entry::refresh(&second.root, &second.image, &entry).unwrap();
    assert_eq!(second.root, root.canonicalize().unwrap());
    assert_eq!(fs::read_link(&entry).unwrap(), second.image);
}

#[cfg(unix)]
#[test]
fn moving_registered_app_retargets_its_owned_desktop_launcher() {
    let (directory, root, app, source) = fixture();
    let first = prepare_image(
        root.clone(),
        app.clone(),
        "com.atomicobject.rhizome".into(),
        "1.0.0".into(),
        &source,
        Some(directory.path()),
    )
    .unwrap();
    let selected = directory.path().join("Moved Rhizome.AppImage");
    fs::rename(&app, &selected).unwrap();
    let moved = prepare_image(
        root.clone(),
        selected.clone(),
        "com.atomicobject.rhizome".into(),
        "1.0.0".into(),
        &source,
        Some(directory.path()),
    )
    .unwrap();
    assert!(moved.integration_error.lock().unwrap().is_none());
    let location = desktop_entry::location(directory.path(), &moved.identifier).unwrap();
    let launcher = fs::read_to_string(location.entry).unwrap();
    assert!(launcher.contains(&format!("TryExec={}\n", selected.display())));
    assert!(!launcher.contains(&format!("TryExec={}\n", app.display())));
    let current = read_registration(&root).unwrap().unwrap();
    assert_eq!(current.app_path, selected);
    assert_eq!(
        current.generation,
        first.generation.load(Ordering::Acquire) + 1
    );
    assert!(change_prepared_registration(&first, true)
        .unwrap_err()
        .contains("changed"));
    assert_eq!(
        fs::read_to_string(
            desktop_entry::location(directory.path(), &moved.identifier)
                .unwrap()
                .entry
        )
        .unwrap(),
        launcher
    );
    fs::write(&app, "old app process").unwrap();
    prepare_image(
        root.clone(),
        app,
        "com.atomicobject.rhizome".into(),
        "1.0.0".into(),
        &source,
        Some(directory.path()),
    )
    .unwrap();
    assert_eq!(
        read_registration(&root).unwrap().unwrap().app_path,
        selected
    );
    assert_eq!(
        fs::read_to_string(
            desktop_entry::location(directory.path(), &moved.identifier)
                .unwrap()
                .entry
        )
        .unwrap(),
        launcher
    );
}

#[cfg(unix)]
#[test]
fn failed_relocation_preserves_launcher_ownership_for_retry() {
    let (directory, root, app, source) = fixture();
    let first = prepare_image(
        root.clone(),
        app.clone(),
        "com.atomicobject.rhizome".into(),
        "1.0.0".into(),
        &source,
        Some(directory.path()),
    )
    .unwrap();
    let location = desktop_entry::location(directory.path(), &first.identifier).unwrap();
    let owned_launcher = fs::read(&location.entry).unwrap();
    let selected = directory.path().join("Moved Rhizome.AppImage");
    fs::rename(&app, &selected).unwrap();
    fs::write(&location.entry, b"[Desktop Entry]\nName=User entry\n").unwrap();
    let failed = prepare_image(
        root.clone(),
        selected.clone(),
        "com.atomicobject.rhizome".into(),
        "1.0.0".into(),
        &source,
        Some(directory.path()),
    )
    .unwrap();
    assert!(failed.integration_error.lock().unwrap().is_some());
    let unchanged = read_registration(&root).unwrap().unwrap();
    assert_eq!(unchanged.app_path, app);
    assert_eq!(
        unchanged.generation,
        first.generation.load(Ordering::Acquire)
    );
    assert_eq!(
        fs::read(&location.entry).unwrap(),
        b"[Desktop Entry]\nName=User entry\n"
    );
    fs::write(&location.entry, owned_launcher).unwrap();
    let retried = prepare_image(
        root.clone(),
        selected.clone(),
        "com.atomicobject.rhizome".into(),
        "1.0.0".into(),
        &source,
        Some(directory.path()),
    )
    .unwrap();
    assert!(retried.integration_error.lock().unwrap().is_none());
    assert_eq!(
        read_registration(&root).unwrap().unwrap().app_path,
        selected
    );
}

#[cfg(unix)]
#[test]
fn moving_a_disabled_app_does_not_recreate_its_desktop_launcher() {
    let (directory, root, app, source) = fixture();
    let first = prepare_image(
        root.clone(),
        app.clone(),
        "com.atomicobject.rhizome".into(),
        "1.0.0".into(),
        &source,
        Some(directory.path()),
    )
    .unwrap();
    desktop_entry::remove(directory.path(), &first.identifier, &app).unwrap();
    let mut disabled = read_registration(&root).unwrap().unwrap();
    disabled.enabled = false;
    disabled.generation += 1;
    write_registration(&root, &disabled).unwrap();
    let selected = directory.path().join("Moved Rhizome.AppImage");
    fs::rename(&app, &selected).unwrap();
    prepare_image(
        root.clone(),
        selected,
        "com.atomicobject.rhizome".into(),
        "1.0.0".into(),
        &source,
        Some(directory.path()),
    )
    .unwrap();
    let after = read_registration(&root).unwrap().unwrap();
    assert!(!after.enabled);
    assert_eq!(after.app_path, disabled.app_path);
    assert_eq!(after.generation, disabled.generation);
    assert!(
        !desktop_entry::location(directory.path(), &first.identifier)
            .unwrap()
            .entry
            .exists()
    );
}
