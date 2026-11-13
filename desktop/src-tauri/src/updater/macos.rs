use std::{
    ffi::CString,
    fs, io,
    os::unix::{ffi::OsStrExt, fs::MetadataExt},
    path::Path,
    process::Command,
};
use tempfile::TempDir;

#[path = "archive.rs"]
mod archive;

#[derive(Debug, PartialEq)]
pub(super) enum Outcome {
    Installed,
    Fallback,
}

type Result<T> = std::result::Result<T, String>;

pub(super) fn install(bytes: &[u8], identifier: &str, version: &str) -> Result<Outcome> {
    let executable = std::env::current_exe().map_err(|e| e.to_string())?;
    let app = executable
        .parent()
        .and_then(Path::parent)
        .and_then(Path::parent)
        .filter(|path| path.extension().is_some_and(|ext| ext == "app"))
        .ok_or("The running executable is not in an app bundle")?;
    install_at(bytes, app, identifier, version)
}

fn install_at(bytes: &[u8], app: &Path, identifier: &str, version: &str) -> Result<Outcome> {
    let temp = std::env::temp_dir()
        .canonicalize()
        .map_err(|e| e.to_string())?;
    let installed = app.canonicalize().map_err(|e| e.to_string())?;
    if temp.starts_with(&installed) {
        return Err("Update staging directory must be outside the installed app".into());
    }
    let stage = tempfile::Builder::new()
        .prefix("rhizome-update-")
        .tempdir_in(temp)
        .map_err(|e| e.to_string())?;
    let bundle = archive::extract(bytes, stage.path()).map_err(|e| e.to_string())?;
    let requirement = requirement(app, identifier)?;
    validate_identity(&bundle, identifier, version)?;
    verify(&bundle, &requirement)?;
    if !eligible(app, &bundle) || verify(app, &requirement).is_err() {
        return Ok(Outcome::Fallback);
    }
    transact(app, &bundle, stage, &mut Native { requirement })
}

fn validate_identity(bundle: &Path, identifier: &str, version: &str) -> Result<()> {
    let info =
        plist::Value::from_file(bundle.join("Contents/Info.plist")).map_err(|e| e.to_string())?;
    let info = info
        .as_dictionary()
        .ok_or("Update Info.plist is not a dictionary")?;
    for (key, expected) in [
        ("CFBundleIdentifier", identifier),
        ("CFBundleShortVersionString", version),
    ] {
        if info.get(key).and_then(plist::Value::as_string) != Some(expected) {
            return Err(format!("Update {key} does not match {expected}"));
        }
    }
    Ok(())
}

fn requirement(app: &Path, identifier: &str) -> Result<String> {
    let output = Command::new("/usr/bin/codesign")
        .args(["-d", "--verbose=4"])
        .arg(app)
        .output()
        .map_err(|e| e.to_string())?;
    let details = String::from_utf8_lossy(&output.stderr);
    let team = details
        .lines()
        .find_map(|line| line.strip_prefix("TeamIdentifier="))
        .filter(|team| {
            team.len() == 10
                && team
                    .bytes()
                    .all(|b| b.is_ascii_uppercase() || b.is_ascii_digit())
        })
        .ok_or("Running app has no Developer ID signing team")?;
    let identifier = serde_json::to_string(identifier).map_err(|e| e.to_string())?;
    Ok(format!("anchor apple generic and identifier {identifier} and certificate leaf[subject.OU] = \"{team}\""))
}

fn verify(app: &Path, requirement: &str) -> Result<()> {
    let output = Command::new("/usr/bin/codesign")
        .args(["--verify", "--deep", "--strict", "-R"])
        .arg(format!("={requirement}"))
        .arg(app)
        .output()
        .map_err(|e| e.to_string())?;
    if !output.status.success() {
        return Err(format!(
            "App signature verification failed: {}",
            String::from_utf8_lossy(&output.stderr).trim()
        ));
    }
    Ok(())
}

fn owned_writable_directory(path: &Path) -> bool {
    let Ok(metadata) = fs::symlink_metadata(path) else {
        return false;
    };
    let Ok(path) = CString::new(path.as_os_str().as_bytes()) else {
        return false;
    };
    // SAFETY: the path is NUL terminated; these calls do not retain its pointer.
    unsafe {
        metadata.is_dir()
            && metadata.uid() == libc::geteuid()
            && libc::faccessat(libc::AT_FDCWD, path.as_ptr(), libc::W_OK, libc::AT_EACCESS) == 0
    }
}

fn eligible(app: &Path, bundle: &Path) -> bool {
    let contents = app.join("Contents");
    owned_writable_directory(app)
        && owned_writable_directory(&contents)
        && fs::metadata(&contents)
            .and_then(|current| fs::metadata(bundle).map(|staged| current.dev() == staged.dev()))
            .unwrap_or(false)
        && fs::read_dir(app).is_ok_and(|entries| {
            entries
                .collect::<io::Result<Vec<_>>>()
                .is_ok_and(|entries| entries.len() == 1 && entries[0].file_name() == "Contents")
        })
}

fn swap(left: &Path, right: &Path) -> io::Result<()> {
    let left = CString::new(left.as_os_str().as_bytes())?;
    let right = CString::new(right.as_os_str().as_bytes())?;
    // SAFETY: both pointers are valid NUL-terminated paths for the duration of the call.
    if unsafe { libc::renamex_np(left.as_ptr(), right.as_ptr(), libc::RENAME_SWAP) } == 0 {
        Ok(())
    } else {
        Err(io::Error::last_os_error())
    }
}

fn can_fallback(error: &io::Error) -> bool {
    matches!(
        error.raw_os_error(),
        Some(
            libc::EACCES | libc::EPERM | libc::ENOTSUP | libc::ENOSYS | libc::EINVAL | libc::EXDEV
        )
    )
}

trait Operations {
    fn swap(&mut self, left: &Path, right: &Path) -> io::Result<()>;
    fn verify(&mut self, app: &Path) -> Result<()>;
    fn cleanup(&mut self, stage: TempDir) -> io::Result<()> {
        stage.close()
    }
}

struct Native {
    requirement: String,
}
impl Operations for Native {
    fn swap(&mut self, left: &Path, right: &Path) -> io::Result<()> {
        swap(left, right)
    }
    fn verify(&mut self, app: &Path) -> Result<()> {
        verify(app, &self.requirement)
    }
}

fn transact(
    app: &Path,
    bundle: &Path,
    stage: TempDir,
    ops: &mut impl Operations,
) -> Result<Outcome> {
    let current = app.join("Contents");
    let replacement = bundle.join("Contents");
    if let Err(error) = ops.swap(&current, &replacement) {
        if error.raw_os_error() == Some(libc::EPERM) {
            eprintln!("Rhizome Contents swap denied with EPERM; macOS App Management may require authorization: {error}");
        } else {
            eprintln!("Rhizome Contents swap failed before mutation: {error}");
        }
        return if can_fallback(&error) {
            Ok(Outcome::Fallback)
        } else {
            Err(error.to_string())
        };
    }
    if let Err(error) = ops.verify(app) {
        if let Err(rollback) = ops.swap(&current, &replacement) {
            let recovery = stage.keep();
            return Err(format!("{error}; rollback failed: {rollback}. Previous Contents preserved at {}. Installed app is {}. Do not retry before recovery.", recovery.join(bundle.file_name().unwrap()).join("Contents").display(), app.display()));
        }
        return Err(format!("{error}; previous Contents restored"));
    }
    // Preserve the bundle root and its xattrs while notifying LaunchServices of the update.
    let _ = Command::new("/usr/bin/touch").arg(app).status();
    if let Err(error) = ops.cleanup(stage) {
        eprintln!("Rhizome update installed; could not remove previous Contents: {error}");
    }
    Ok(Outcome::Installed)
}

#[cfg(test)]
#[path = "macos_tests.rs"]
mod tests;
