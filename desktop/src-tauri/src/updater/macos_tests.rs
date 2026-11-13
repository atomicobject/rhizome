use super::*;
use std::{
    os::unix::fs::{symlink, PermissionsExt},
    path::PathBuf,
};

struct Fixture {
    root: TempDir,
    _stage: TempDir,
    app: PathBuf,
    bundle: PathBuf,
}
impl Fixture {
    fn new() -> Self {
        let root = tempfile::tempdir().unwrap();
        let stage = tempfile::tempdir().unwrap();
        let app = root.path().join("Installed.app");
        let bundle = stage.path().join("Replacement.app");
        for (path, version) in [(&app, "old"), (&bundle, "new")] {
            fs::create_dir_all(path.join("Contents/MacOS")).unwrap();
            fs::write(path.join("Contents/MacOS/rzm"), version).unwrap();
        }
        Self {
            root,
            _stage: stage,
            app,
            bundle,
        }
    }
}
impl Drop for Fixture {
    fn drop(&mut self) {
        fs::set_permissions(self.root.path(), fs::Permissions::from_mode(0o755)).unwrap();
    }
}

#[derive(Default)]
struct Faults {
    swaps: usize,
    fail_swap: Option<usize>,
    fail_verify: bool,
    fail_cleanup: bool,
}
impl Operations for Faults {
    fn swap(&mut self, left: &Path, right: &Path) -> io::Result<()> {
        self.swaps += 1;
        if self.fail_swap == Some(self.swaps) {
            Err(io::Error::from_raw_os_error(libc::EPERM))
        } else {
            swap(left, right)
        }
    }
    fn verify(&mut self, _app: &Path) -> Result<()> {
        if self.fail_verify {
            Err("injected post-swap verification failure".into())
        } else {
            Ok(())
        }
    }
    fn cleanup(&mut self, stage: TempDir) -> io::Result<()> {
        if self.fail_cleanup {
            let _ = stage.keep();
            Err(io::Error::from_raw_os_error(libc::EACCES))
        } else {
            stage.close()
        }
    }
}

#[test]
fn protected_parent_contents_swap_preserves_root_and_cli_link() {
    let f = Fixture::new();
    let original = fs::metadata(&f.app).unwrap();
    let link = f.root.path().join("rzm");
    symlink(f.app.join("Contents/MacOS/rzm"), &link).unwrap();
    fs::set_permissions(f.root.path(), fs::Permissions::from_mode(0o555)).unwrap();
    assert!(eligible(&f.app, &f.bundle));
    // This is the old installer's failing operation under the same protected parent.
    assert_eq!(
        swap(&f.app, &f.bundle).unwrap_err().raw_os_error(),
        Some(libc::EACCES)
    );
    let stage = tempfile::tempdir().unwrap();
    fs::rename(&f.bundle, stage.path().join("Replacement.app")).unwrap();
    let bundle = stage.path().join("Replacement.app");
    assert_eq!(
        transact(&f.app, &bundle, stage, &mut Faults::default()).unwrap(),
        Outcome::Installed
    );
    assert_eq!(fs::read_to_string(&link).unwrap(), "new");
    assert_eq!(
        fs::read_link(&link).unwrap(),
        f.app.join("Contents/MacOS/rzm")
    );
    assert_eq!(fs::metadata(&f.app).unwrap().ino(), original.ino());
    assert_eq!(fs::metadata(&f.app).unwrap().uid(), original.uid());
}

#[test]
fn eligibility_rejects_symlinks_nonwritable_contents_and_root_detritus() {
    let f = Fixture::new();
    let alias = f.root.path().join("Alias.app");
    symlink(&f.app, &alias).unwrap();
    assert!(!eligible(&alias, &f.bundle));
    fs::write(f.app.join("stray"), "stray").unwrap();
    assert!(!eligible(&f.app, &f.bundle));
    fs::remove_file(f.app.join("stray")).unwrap();
    fs::set_permissions(f.app.join("Contents"), fs::Permissions::from_mode(0o555)).unwrap();
    assert!(!eligible(&f.app, &f.bundle));
    fs::set_permissions(f.app.join("Contents"), fs::Permissions::from_mode(0o755)).unwrap();
}

#[test]
fn post_swap_failure_rolls_back_and_does_not_fallback() {
    let f = Fixture::new();
    let stage = tempfile::tempdir().unwrap();
    let bundle = stage.path().join("Replacement.app");
    fs::rename(&f.bundle, &bundle).unwrap();
    let mut ops = Faults {
        fail_verify: true,
        ..Default::default()
    };
    let error = transact(&f.app, &bundle, stage, &mut ops).unwrap_err();
    assert!(error.contains("previous Contents restored"));
    assert_eq!(ops.swaps, 2);
    assert_eq!(
        fs::read_to_string(f.app.join("Contents/MacOS/rzm")).unwrap(),
        "old"
    );
}

#[test]
fn failed_rollback_preserves_old_contents_at_reported_recovery_path() {
    let f = Fixture::new();
    let stage = tempfile::tempdir().unwrap();
    let recovery = stage.path().to_path_buf();
    let bundle = recovery.join("Replacement.app");
    fs::rename(&f.bundle, &bundle).unwrap();
    let error = transact(
        &f.app,
        &bundle,
        stage,
        &mut Faults {
            fail_verify: true,
            fail_swap: Some(2),
            ..Default::default()
        },
    )
    .unwrap_err();
    assert!(error.contains(bundle.join("Contents").to_str().unwrap()));
    assert_eq!(
        fs::read_to_string(bundle.join("Contents/MacOS/rzm")).unwrap(),
        "old"
    );
    assert_eq!(
        fs::read_to_string(f.app.join("Contents/MacOS/rzm")).unwrap(),
        "new"
    );
    fs::remove_dir_all(recovery).unwrap();
}

#[test]
fn pre_swap_permission_failure_can_fallback_without_mutation() {
    let f = Fixture::new();
    let stage = tempfile::tempdir().unwrap();
    assert_eq!(
        transact(
            &f.app,
            &f.bundle,
            stage,
            &mut Faults {
                fail_swap: Some(1),
                ..Default::default()
            }
        )
        .unwrap(),
        Outcome::Fallback
    );
    assert_eq!(
        fs::read_to_string(f.app.join("Contents/MacOS/rzm")).unwrap(),
        "old"
    );
    for errno in [
        libc::EACCES,
        libc::EPERM,
        libc::ENOTSUP,
        libc::ENOSYS,
        libc::EINVAL,
        libc::EXDEV,
    ] {
        assert!(can_fallback(&io::Error::from_raw_os_error(errno)));
    }
    assert!(!can_fallback(&io::Error::from_raw_os_error(libc::EIO)));
}

#[test]
fn cleanup_failure_is_success_after_verified_commit() {
    let f = Fixture::new();
    let stage = tempfile::tempdir().unwrap();
    let leftover = stage.path().to_path_buf();
    let bundle = leftover.join("Replacement.app");
    fs::rename(&f.bundle, &bundle).unwrap();
    assert_eq!(
        transact(
            &f.app,
            &bundle,
            stage,
            &mut Faults {
                fail_cleanup: true,
                ..Default::default()
            }
        )
        .unwrap(),
        Outcome::Installed
    );
    assert_eq!(
        fs::read_to_string(f.app.join("Contents/MacOS/rzm")).unwrap(),
        "new"
    );
    fs::remove_dir_all(leftover).unwrap();
}

fn tar(entries: &[(&str, u8, &str)]) -> Vec<u8> {
    let encoder = flate2::write::GzEncoder::new(Vec::new(), flate2::Compression::default());
    let mut archive = tar::Builder::new(encoder);
    for (path, kind, data) in entries {
        let mut header = tar::Header::new_gnu();
        // Write raw paths to exercise names that tar::Builder would refuse to create.
        header.as_mut_bytes()[..100].fill(0);
        header.as_mut_bytes()[..path.len()].copy_from_slice(path.as_bytes());
        header.set_mode(0o755);
        header.set_entry_type(tar::EntryType::new(*kind));
        let body = if *kind == b'0' { data.as_bytes() } else { &[] };
        if *kind == b'2' || *kind == b'1' {
            header.set_link_name(data).unwrap();
        }
        header.set_size(body.len() as u64);
        header.set_cksum();
        archive.append(&header, body).unwrap();
    }
    archive.into_inner().unwrap().finish().unwrap()
}

#[test]
fn archive_rejects_unsafe_entries_before_any_extraction() {
    for entry in [
        ("/Evil.app/Contents/file", b'0', "bad"),
        ("Good.app/Contents/../../outside", b'0', "bad"),
        ("Good.app/stray", b'0', "bad"),
        ("Other.app/Contents/file", b'0', "bad"),
        ("Good.app/Contents/escape", b'2', "../../outside"),
        ("Good.app/Contents/absolute", b'2', "/tmp/outside"),
        ("Good.app/Contents/hard", b'1', "Good.app/Contents/file"),
        ("Good.app/Contents/pipe", b'6', ""),
    ] {
        let destination = tempfile::tempdir().unwrap();
        let bytes = tar(&[("Good.app/Contents/file", b'0', "good"), entry]);
        assert!(
            archive::extract(&bytes, destination.path()).is_err(),
            "accepted {entry:?}"
        );
        assert_eq!(fs::read_dir(destination.path()).unwrap().count(), 0);
    }
    let destination = tempfile::tempdir().unwrap();
    assert!(archive::extract(
        &tar(&[
            ("Good.app/Contents/dir", b'2', "."),
            ("Good.app/Contents/dir/file", b'0', "bad")
        ]),
        destination.path()
    )
    .is_err());
    assert_eq!(fs::read_dir(destination.path()).unwrap().count(), 0);
}

#[test]
fn archive_supports_internal_framework_symlinks() {
    let destination = tempfile::tempdir().unwrap();
    let bytes = tar(&[
        (
            "Good.app/Contents/Frameworks/F.framework/Versions/A/F",
            b'0',
            "framework",
        ),
        (
            "Good.app/Contents/Frameworks/F.framework/Versions/Current",
            b'2',
            "A",
        ),
        (
            "Good.app/Contents/Frameworks/F.framework/F",
            b'2',
            "Versions/Current/F",
        ),
    ]);
    let app = archive::extract(&bytes, destination.path()).unwrap();
    assert_eq!(
        fs::read_to_string(app.join("Contents/Frameworks/F.framework/F")).unwrap(),
        "framework"
    );
}

#[test]
fn invalid_archive_and_identity_fail_before_ineligible_fallback() {
    let f = Fixture::new();
    fs::write(f.app.join("stray"), "detritus").unwrap();
    assert!(install_at(b"not an archive", &f.app, "test.app", "2.0.0").is_err());
    let info = br#"<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>test.app</string><key>CFBundleShortVersionString</key><string>2.0.0</string></dict></plist>"#;
    fs::write(f.bundle.join("Contents/Info.plist"), info).unwrap();
    assert!(validate_identity(&f.bundle, "wrong.app", "2.0.0").is_err());
    assert!(validate_identity(&f.bundle, "test.app", "3.0.0").is_err());
    validate_identity(&f.bundle, "test.app", "2.0.0").unwrap();
    assert!(verify(&f.bundle, "anchor apple generic").is_err());
    assert_eq!(
        fs::read_to_string(f.app.join("Contents/MacOS/rzm")).unwrap(),
        "old"
    );
}

#[test]
#[ignore = "requires RZM_SIGNED_UPDATE_ARCHIVE pointing to a public notarized updater archive and host trust-service access"]
fn signed_archive_uses_real_adapter_under_protected_parent() {
    let archive =
        std::env::var_os("RZM_SIGNED_UPDATE_ARCHIVE").expect("set the signed fixture path");
    let bytes = fs::read(archive).unwrap();
    let root = tempfile::tempdir().unwrap();
    let applications = root.path().join("Applications");
    fs::create_dir(&applications).unwrap();
    let app = archive::extract(&bytes, &applications).unwrap();
    let info = plist::Value::from_file(app.join("Contents/Info.plist")).unwrap();
    let info = info.as_dictionary().unwrap();
    let id = info["CFBundleIdentifier"].as_string().unwrap();
    let version = info["CFBundleShortVersionString"].as_string().unwrap();
    let before = fs::metadata(&app).unwrap();
    let contents = fs::metadata(app.join("Contents")).unwrap().ino();
    let link = root.path().join("rzm");
    symlink(app.join("Contents/MacOS/rzm"), &link).unwrap();
    assert!(Command::new("/usr/bin/xattr")
        .args(["-w", "com.apple.quarantine", "0081;67400000;RhizomeTest;"])
        .arg(&app)
        .status()
        .unwrap()
        .success());
    let xattrs = Command::new("/usr/bin/xattr")
        .arg("-lx")
        .arg(&app)
        .output()
        .unwrap()
        .stdout;
    fs::set_permissions(&applications, fs::Permissions::from_mode(0o555)).unwrap();
    let result = install_at(&bytes, &app, id, version);
    fs::set_permissions(&applications, fs::Permissions::from_mode(0o755)).unwrap();
    assert_eq!(result.unwrap(), Outcome::Installed);
    assert_eq!(fs::metadata(&app).unwrap().ino(), before.ino());
    assert_ne!(fs::metadata(app.join("Contents")).unwrap().ino(), contents);
    assert_eq!(
        Command::new("/usr/bin/xattr")
            .arg("-lx")
            .arg(&app)
            .output()
            .unwrap()
            .stdout,
        xattrs
    );
    verify(&app, &requirement(&app, id).unwrap()).unwrap();
    assert!(Command::new("/usr/bin/xcrun")
        .args(["stapler", "validate"])
        .arg(&app)
        .output()
        .unwrap()
        .status
        .success());
    assert!(Command::new("/usr/sbin/spctl")
        .args(["--assess", "--type", "execute"])
        .arg(&app)
        .output()
        .unwrap()
        .status
        .success());
    let output = Command::new(&link).arg("--version").output().unwrap();
    assert!(output.status.success());
    assert!(String::from_utf8_lossy(&output.stdout).contains(version));
    assert_eq!(
        fs::read_link(&link).unwrap(),
        app.join("Contents/MacOS/rzm")
    );
    // Root detritus makes a valid signed replacement eligible only for full-bundle fallback.
    fs::write(app.join("stray"), "detritus").unwrap();
    assert_eq!(
        install_at(&bytes, &app, id, version).unwrap(),
        Outcome::Fallback
    );
    assert!(install_at(&bytes, &app, id, "999.0.0").is_err());
    assert!(install_at(&bytes, &app, "wrong.identifier", version).is_err());
    let mut xml = Vec::new();
    plist::Value::Dictionary(info.clone())
        .to_writer_xml(&mut xml)
        .unwrap();
    let xml = String::from_utf8(xml).unwrap();
    let unsigned = tar(&[("Rhizome.app/Contents/Info.plist", b'0', &xml)]);
    assert!(install_at(&unsigned, &app, id, version)
        .unwrap_err()
        .contains("signature verification failed"));
    fs::remove_file(app.join("stray")).unwrap();
    fs::write(app.join("Contents/MacOS/rzm"), "damaged signature").unwrap();
    assert_eq!(
        install_at(&bytes, &app, id, version).unwrap(),
        Outcome::Fallback
    );
    assert_eq!(
        fs::read_to_string(app.join("Contents/MacOS/rzm")).unwrap(),
        "damaged signature"
    );
    assert_eq!(fs::metadata(&app).unwrap().ino(), before.ino());
}
