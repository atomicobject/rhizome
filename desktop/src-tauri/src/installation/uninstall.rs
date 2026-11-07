use super::{
    file_hash, lock, paths, read_registration, valid_identifier, write_registration, Registration,
};
use crate::bridge::{self, Request};
use serde_json::Value;
use std::{ffi::OsString, future::Future, path::Path, time::Duration};

const ARGUMENT: &str = "--remove-desktop-integration";
const TIMEOUT: Duration = Duration::from_secs(15);

pub fn requested(args: &[OsString]) -> bool {
    args.iter().any(|arg| arg == ARGUMENT)
}

/// The NSIS hook runs before Tauri, so it never opens a window or creates an image.
pub fn run(identifier: &str, args: &[OsString]) -> Result<(), String> {
    if !cfg!(windows) || args.len() != 1 || !requested(args) {
        return Err("Use the Windows uninstaller to remove desktop integration.".into());
    }
    if !valid_identifier(identifier) {
        return Err("Unknown Rhizome desktop application identifier.".into());
    }
    let app = std::env::current_exe()
        .and_then(|path| path.canonicalize())
        .map_err(|error| error.to_string())?;
    let root = paths::base()?.join(identifier);
    let Some(record) = snapshot(&root, &app)? else {
        return Ok(());
    };
    let image = root.join("images").join(&record.image_hash).join("rzm.exe");
    tauri::async_runtime::block_on(remove(&root, &record, |operation, generation| {
        bridge::call_sidecar(
            &root,
            Request {
                operation,
                expected_generation: generation,
                ..Request::default()
            },
            TIMEOUT,
            &image,
        )
    }))
}

fn snapshot(root: &Path, app: &Path) -> Result<Option<Registration>, String> {
    if !root.exists() {
        return Ok(None);
    }
    let _lock = lock(root)?;
    let Some(record) = read_registration(root)? else {
        return Ok(None);
    };
    if record.app_path.canonicalize().ok().as_deref() != Some(app) {
        return Ok(None);
    }
    let image = root
        .join("images")
        .join(&record.image_hash)
        .join(if cfg!(windows) { "rzm.exe" } else { "rzm" });
    if file_hash(&image)? != record.image_hash {
        return Err(
            "The registered Rhizome executable changed. Reinstall before uninstalling.".into(),
        );
    }
    Ok(Some(record))
}

async fn remove<F: Future<Output = Result<Value, bridge::Failure>>>(
    root: &Path,
    expected: &Registration,
    mut call: impl FnMut(&'static str, u64) -> F,
) -> Result<(), String> {
    let status = call("cli-link-status", 0)
        .await
        .map_err(|error| error.message)?;
    let unlinked = match status.get("state").and_then(Value::as_str) {
        Some("foreign" | "unsupported" | "other-bundle") => false,
        Some("absent" | "installed" | "dangling") => {
            call("cli-link-remove", expected.generation)
                .await
                .map_err(|error| error.message)?;
            true
        }
        _ => return Err("Rhizome returned invalid command-line tool information.".into()),
    };
    disable(root, expected).map_err(|message| {
        let state = if unlinked {
            "The command-line tool is unlinked"
        } else {
            "The command-line tool was not changed"
        };
        format!("{state}, but desktop integration could not be disabled: {message}")
    })
}

fn disable(root: &Path, expected: &Registration) -> Result<(), String> {
    let _lock = lock(root)?;
    let mut current = read_registration(root)?.ok_or("Desktop registration is missing.")?;
    if current.generation != expected.generation || current.app_path != expected.app_path {
        return Err(
            "The installed app changed. Retry removal from its current installation.".into(),
        );
    }
    if current.enabled {
        current.enabled = false;
        current.generation = current
            .generation
            .checked_add(1)
            .ok_or("The desktop registration generation is exhausted.")?;
        write_registration(root, &current)?;
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    use std::{cell::RefCell, fs, future::ready, path::PathBuf};

    fn fixture() -> (tempfile::TempDir, PathBuf, PathBuf, Registration) {
        let directory = tempfile::tempdir().unwrap();
        let root = directory.path().join("com.atomicobject.rhizome");
        let app = directory.path().join("Rhizome");
        fs::create_dir(&root).unwrap();
        fs::write(&app, "GUI").unwrap();
        let source = directory.path().join("companion");
        fs::write(&source, "immutable executable").unwrap();
        let hash = file_hash(&source).unwrap();
        let image =
            root.join("images")
                .join(&hash)
                .join(if cfg!(windows) { "rzm.exe" } else { "rzm" });
        fs::create_dir_all(image.parent().unwrap()).unwrap();
        fs::copy(source, &image).unwrap();
        let record = Registration {
            v: 1,
            identifier: "com.atomicobject.rhizome".into(),
            generation: 4,
            app_path: app.canonicalize().unwrap(),
            version: "1.0.0".into(),
            image_hash: hash,
            enabled: true,
        };
        write_registration(&root, &record).unwrap();
        (directory, root, image, record)
    }

    #[test]
    fn missing_or_another_installation_does_not_create_or_change_registration() {
        let (directory, root, _image, record) = fixture();
        let absent = directory.path().join("absent");
        assert!(snapshot(&absent, &record.app_path).unwrap().is_none());
        assert!(!absent.exists());
        let other = directory.path().join("other");
        fs::write(&other, "another GUI").unwrap();
        assert!(snapshot(&root, &other.canonicalize().unwrap())
            .unwrap()
            .is_none());
        assert_eq!(read_registration(&root).unwrap().unwrap().generation, 4);
    }

    #[test]
    fn a_modified_companion_is_never_executed() {
        let (_directory, root, image, record) = fixture();
        fs::write(&image, "modified").unwrap();
        assert!(snapshot(&root, &record.app_path)
            .unwrap_err()
            .contains("changed"));
    }

    #[test]
    fn removal_passes_the_snapshot_generation_and_keeps_images() {
        tauri::async_runtime::block_on(async {
            let (_directory, root, image, record) = fixture();
            let calls = RefCell::new(Vec::new());
            remove(&root, &record, |operation, generation| {
                calls.borrow_mut().push((operation, generation));
                ready(Ok(json!({"state":if operation == "cli-link-status" {"installed"} else {"absent"}})))
            }).await.unwrap();
            assert_eq!(
                *calls.borrow(),
                [("cli-link-status", 0), ("cli-link-remove", 4)]
            );
            let current = read_registration(&root).unwrap().unwrap();
            assert!(!current.enabled);
            assert_eq!(current.generation, 5);
            assert!(image.is_file());
        });
    }

    #[test]
    fn a_foreign_cli_is_not_removed() {
        tauri::async_runtime::block_on(async {
            let (_directory, root, _image, record) = fixture();
            remove(&root, &record, |operation, generation| {
                assert_eq!((operation, generation), ("cli-link-status", 0));
                ready(Ok(json!({"state":"foreign"})))
            })
            .await
            .unwrap();
            assert!(!read_registration(&root).unwrap().unwrap().enabled);
        });
    }

    #[test]
    fn a_newer_registration_is_not_disabled_after_an_unlink() {
        tauri::async_runtime::block_on(async {
            let (_directory, root, _image, record) = fixture();
            let error = remove(&root, &record, |operation, _| {
                if operation == "cli-link-remove" {
                    let mut newer = record.clone();
                    newer.generation += 1;
                    write_registration(&root, &newer).unwrap();
                }
                ready(Ok(json!({"state":if operation == "cli-link-status" {"installed"} else {"absent"}})))
            }).await.unwrap_err();
            assert!(error.contains("command-line tool is unlinked"));
            let newer = read_registration(&root).unwrap().unwrap();
            assert!(newer.enabled);
            assert_eq!(newer.generation, 5);
        });
    }
}
