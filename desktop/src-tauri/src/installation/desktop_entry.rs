//! A per-user launcher for the durable AppImage, never its temporary mount.
use sha2::{Digest, Sha256};
use std::{
    fs::{self, File},
    io::{Read, Write},
    path::{Path, PathBuf},
};

const RELEASE: &str = "com.atomicobject.rhizome";
const DEV: &str = "com.atomicobject.rhizome.dev";
const ICON_HASH_KEY: &str = "X-Rhizome-IconHash=";
const RELEASE_ICON: &[u8] = include_bytes!("../../icons/128x128.png");
const DEV_ICON: &[u8] = include_bytes!("../../icons-dev/128x128.png");
const ENTRY_LIMIT: u64 = 16 * 1024;

pub struct Location {
    pub entry: PathBuf,
    pub icon: PathBuf,
}

/// The caller supplies the native OS-account home, never repository environment.
pub fn location(home: &Path, identifier: &str) -> Result<Location, String> {
    path_text(home)?;
    let icon = icon_bytes(identifier)?;
    Ok(Location {
        entry: home
            .join(".local/share/applications")
            .join(format!("{identifier}.desktop")),
        icon: icon_path(home, identifier, &digest(icon)),
    })
}

/// Call under the installation generation lock. A prior path is accepted only
/// for a selection/relocation that the caller has already authorized.
pub fn register(
    home: &Path,
    identifier: &str,
    app_path: &Path,
    previous_app_path: Option<&Path>,
) -> Result<(), String> {
    let paths = location(home, identifier)?;
    executable_value(app_path)?;
    if !app_path.is_file() {
        return Err(
            "The installed AppImage is missing. Move it to ~/Applications before opening Rhizome."
                .into(),
        );
    }
    let before = snapshot(&paths.entry, ENTRY_LIMIT)?;
    if let Some(bytes) = before.as_deref() {
        if !owned_entry(bytes, home, identifier, app_path)
            && !previous_app_path
                .is_some_and(|previous| owned_entry(bytes, home, identifier, previous))
        {
            return Err(foreign_entry(&paths.entry));
        }
    }
    ensure_icon(&paths.icon, icon_bytes(identifier)?)?;
    let next = entry_text(home, identifier, app_path, &digest(icon_bytes(identifier)?))?;
    if before.as_deref() == Some(next.as_bytes()) {
        return Ok(());
    }
    let directory = paths.entry.parent().ok_or("Invalid desktop entry path.")?;
    fs::create_dir_all(directory).map_err(message)?;
    let mut staged = tempfile::NamedTempFile::new_in(directory).map_err(message)?;
    staged.write_all(next.as_bytes()).map_err(message)?;
    staged.as_file().sync_all().map_err(message)?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        staged
            .as_file()
            .set_permissions(fs::Permissions::from_mode(0o644))
            .map_err(message)?;
    }
    if snapshot(&paths.entry, ENTRY_LIMIT)? != before {
        return Err(
            "The desktop launcher changed during installation. It was left in place; try again."
                .into(),
        );
    }
    if before.is_some() {
        staged.persist(&paths.entry).map_err(message)?;
    } else {
        staged.persist_noclobber(&paths.entry).map_err(message)?;
    }
    Ok(())
}

/// Remove only this app's exact generated launcher. Cached icons stay with app
/// data, so a partially completed removal never deletes a foreign icon.
pub fn remove(home: &Path, identifier: &str, app_path: &Path) -> Result<(), String> {
    let paths = location(home, identifier)?;
    let Some(before) = snapshot(&paths.entry, ENTRY_LIMIT)? else {
        return Ok(());
    };
    if !owned_entry(&before, home, identifier, app_path) {
        return Err(foreign_entry(&paths.entry));
    }
    if snapshot(&paths.entry, ENTRY_LIMIT)?.as_ref() != Some(&before) {
        return Err(
            "The desktop launcher changed during removal. It was left in place; try again.".into(),
        );
    }
    match fs::remove_file(&paths.entry) {
        Ok(()) => Ok(()),
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(()),
        Err(error) => Err(message(error)),
    }
}

fn owned_entry(bytes: &[u8], home: &Path, identifier: &str, app_path: &Path) -> bool {
    let Ok(text) = std::str::from_utf8(bytes) else {
        return false;
    };
    let Some(hash) = text
        .lines()
        .find_map(|line| line.strip_prefix(ICON_HASH_KEY))
    else {
        return false;
    };
    if hash.len() != 64
        || !hash
            .bytes()
            .all(|value| value.is_ascii_digit() || (b'a'..=b'f').contains(&value))
    {
        return false;
    }
    entry_text(home, identifier, app_path, hash).is_ok_and(|expected| expected.as_bytes() == bytes)
}

fn entry_text(
    home: &Path,
    identifier: &str,
    app_path: &Path,
    icon_hash: &str,
) -> Result<String, String> {
    let name = match identifier {
        RELEASE => "Rhizome",
        DEV => "Rhizome Dev",
        _ => return Err("Unknown Rhizome desktop application identifier.".into()),
    };
    let app = path_text(app_path)?;
    let icon = icon_path(home, identifier, icon_hash);
    let icon = string_value(path_text(&icon)?);
    Ok(format!(
        "[Desktop Entry]\nType=Application\nName={name}\nComment=Open your repositories in Rhizome\nExec={}\nTryExec={}\nIcon={icon}\nTerminal=false\nCategories=Development;\nX-Rhizome-Identifier={identifier}\n{ICON_HASH_KEY}{icon_hash}\n",
        executable_value(app_path)?, string_value(app),
    ))
}

fn icon_path(home: &Path, identifier: &str, hash: &str) -> PathBuf {
    home.join(".config/rhizome/desktop")
        .join(identifier)
        .join("icons")
        .join(format!("{hash}.png"))
}

fn icon_bytes(identifier: &str) -> Result<&'static [u8], String> {
    match identifier {
        RELEASE => Ok(RELEASE_ICON),
        DEV => Ok(DEV_ICON),
        _ => Err("Unknown Rhizome desktop application identifier.".into()),
    }
}

fn path_text(path: &Path) -> Result<&str, String> {
    let text = path
        .to_str()
        .ok_or("Desktop integration requires a UTF-8 path.")?;
    if !path.is_absolute() || text.chars().any(char::is_control) {
        return Err(
            "Desktop integration requires an absolute path without control characters.".into(),
        );
    }
    Ok(text)
}

// Desktop Entry Exec quoting is not shell quoting. String-value backslashes
// are decoded first, then argument quoting and literal-percent expansion.
fn executable_value(path: &Path) -> Result<String, String> {
    let text = path_text(path)?;
    if text.contains('=') {
        return Err("A desktop launcher's executable path cannot contain '='. Move the AppImage to a path without that character.".into());
    }
    let mut quoted = String::from("\"");
    for value in text.chars() {
        if value == '%' {
            quoted.push_str("%%");
        } else {
            if matches!(value, '"' | '`' | '$' | '\\') {
                quoted.push('\\');
            }
            quoted.push(value);
        }
    }
    quoted.push('"');
    Ok(string_value(&quoted))
}

fn string_value(text: &str) -> String {
    text.replace('\\', "\\\\")
}

fn snapshot(path: &Path, limit: u64) -> Result<Option<Vec<u8>>, String> {
    let metadata = match fs::symlink_metadata(path) {
        Ok(metadata) => metadata,
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => return Ok(None),
        Err(error) => return Err(message(error)),
    };
    if !metadata.file_type().is_file() || metadata.len() > limit {
        return Err(foreign_entry(path));
    }
    let mut bytes = Vec::new();
    File::open(path)
        .map_err(message)?
        .take(limit + 1)
        .read_to_end(&mut bytes)
        .map_err(message)?;
    if bytes.len() as u64 > limit {
        return Err(foreign_entry(path));
    }
    Ok(Some(bytes))
}

fn ensure_icon(path: &Path, bytes: &[u8]) -> Result<(), String> {
    if let Some(existing) = snapshot(path, bytes.len() as u64)? {
        if existing == bytes {
            return Ok(());
        }
        return Err(
            "The cached desktop icon belongs to another file. It was left in place.".into(),
        );
    }
    let directory = path.parent().ok_or("Invalid desktop icon path.")?;
    fs::create_dir_all(directory).map_err(message)?;
    let mut staged = tempfile::NamedTempFile::new_in(directory).map_err(message)?;
    staged.write_all(bytes).map_err(message)?;
    staged.as_file().sync_all().map_err(message)?;
    staged.persist_noclobber(path).map_err(message)?;
    Ok(())
}

fn digest(bytes: &[u8]) -> String {
    format!("{:x}", Sha256::digest(bytes))
}

fn foreign_entry(path: &Path) -> String {
    format!(
        "{} belongs to another installation or was edited. It was left in place.",
        path.display()
    )
}

fn message(error: impl std::fmt::Display) -> String {
    error.to_string()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn app(home: &Path, name: &str) -> PathBuf {
        let directory = home.join("Applications");
        fs::create_dir_all(&directory).unwrap();
        let app = directory.join(name);
        fs::write(&app, b"synthetic app").unwrap();
        app
    }

    #[test]
    fn exec_uses_desktop_entry_escaping_without_shell_parsing() {
        let path =
            Path::new("/home/user/Applications/Rhi \"quoted\" ; $cash `tick` %f \\雪.AppImage");
        assert_eq!(
            executable_value(path).unwrap(),
            r#""/home/user/Applications/Rhi \\"quoted\\" ; \\$cash \\`tick\\` %%f \\\\雪.AppImage""#,
        );
        assert!(executable_value(Path::new("/home/user/a=b.AppImage"))
            .unwrap_err()
            .contains("cannot contain '='"));
        assert!(executable_value(Path::new("/home/user/a\nInjected=true.AppImage")).is_err());
    }

    #[test]
    fn registration_is_idempotent_and_removal_preserves_user_data() {
        let home = tempfile::tempdir().unwrap();
        let app = app(home.path(), "Rhizome.AppImage");
        register(home.path(), RELEASE, &app, None).unwrap();
        let paths = location(home.path(), RELEASE).unwrap();
        let first = fs::read(&paths.entry).unwrap();
        register(home.path(), RELEASE, &app, None).unwrap();
        assert_eq!(fs::read(&paths.entry).unwrap(), first);
        assert_eq!(fs::read(&paths.icon).unwrap(), RELEASE_ICON);
        fs::remove_file(&app).unwrap();
        remove(home.path(), RELEASE, &app).unwrap();
        remove(home.path(), RELEASE, &app).unwrap();
        assert!(!paths.entry.exists());
        assert!(paths.icon.exists());
    }

    #[test]
    fn foreign_or_different_installations_are_preserved() {
        let home = tempfile::tempdir().unwrap();
        let original = app(home.path(), "original.AppImage");
        let selected = app(home.path(), "selected.AppImage");
        register(home.path(), RELEASE, &original, None).unwrap();
        let paths = location(home.path(), RELEASE).unwrap();
        let before = fs::read(&paths.entry).unwrap();
        assert!(register(home.path(), RELEASE, &selected, None).is_err());
        assert!(remove(home.path(), RELEASE, &selected).is_err());
        assert_eq!(fs::read(&paths.entry).unwrap(), before);
        register(home.path(), RELEASE, &selected, Some(&original)).unwrap();
        fs::write(&paths.entry, b"[Desktop Entry]\nName=User entry\n").unwrap();
        assert!(register(home.path(), RELEASE, &selected, None).is_err());
        assert!(remove(home.path(), RELEASE, &selected).is_err());
        assert_eq!(
            fs::read(&paths.entry).unwrap(),
            b"[Desktop Entry]\nName=User entry\n"
        );
    }

    #[cfg(unix)]
    #[test]
    fn symbolic_link_entry_is_foreign_and_untouched() {
        use std::os::unix::fs::symlink;
        let home = tempfile::tempdir().unwrap();
        let app = app(home.path(), "Rhizome.AppImage");
        let paths = location(home.path(), RELEASE).unwrap();
        fs::create_dir_all(paths.entry.parent().unwrap()).unwrap();
        let other = home.path().join("foreign.desktop");
        fs::write(&other, b"foreign").unwrap();
        symlink(&other, &paths.entry).unwrap();
        assert!(register(home.path(), RELEASE, &app, None).is_err());
        assert!(remove(home.path(), RELEASE, &app).is_err());
        assert_eq!(fs::read_link(paths.entry).unwrap(), other);
        assert_eq!(fs::read(other).unwrap(), b"foreign");
        assert!(!paths.icon.exists());
    }

    #[cfg(target_os = "linux")]
    #[test]
    fn desktop_launcher_preserves_the_executable_path_in_gio() {
        use std::{
            os::unix::fs::PermissionsExt,
            process::Command,
            time::{Duration, Instant},
        };
        let home = tempfile::tempdir().unwrap();
        let app = app(
            home.path(),
            "Rhi \"quoted\" ; $cash `tick` %f \\雪.AppImage",
        );
        fs::write(&app, b"#!/bin/sh\nprintf '%s' \"$0\" > \"${0}.launched\"\n").unwrap();
        fs::set_permissions(&app, fs::Permissions::from_mode(0o700)).unwrap();
        register(home.path(), RELEASE, &app, None).unwrap();
        let paths = location(home.path(), RELEASE).unwrap();
        let launched = Command::new("gio")
            .arg("launch")
            .arg(&paths.entry)
            .output()
            .expect("gio is required for native launcher tests (libglib2.0-bin)");
        assert!(
            launched.status.success(),
            "{}",
            String::from_utf8_lossy(&launched.stderr)
        );
        let marker = PathBuf::from(format!("{}.launched", app.display()));
        let deadline = Instant::now() + Duration::from_secs(5);
        while !marker.exists() && Instant::now() < deadline {
            std::thread::sleep(Duration::from_millis(10));
        }
        assert_eq!(fs::read_to_string(marker).unwrap(), app.to_str().unwrap());
    }
}
