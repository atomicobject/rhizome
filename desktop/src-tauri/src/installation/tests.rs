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
        &source
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
    )
    .unwrap();
    fs::write(&source, "version two").unwrap();
    let second = prepare_image(
        root.clone(),
        app.clone(),
        "com.atomicobject.rhizome".into(),
        "1.1.0".into(),
        &source,
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
    )
    .unwrap();
    entry::refresh(&second.root, &second.image, &entry).unwrap();
    assert_eq!(second.root, root.canonicalize().unwrap());
    assert_eq!(fs::read_link(&entry).unwrap(), second.image);
}
