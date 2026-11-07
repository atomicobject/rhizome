//! Portable app ownership and immutable companion images.
use fs2::FileExt;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::{
    fs::{self, File, OpenOptions},
    io::{Read, Write},
    path::{Path, PathBuf},
    sync::{
        atomic::{AtomicU64, Ordering},
        Mutex, OnceLock,
    },
};
use tauri::AppHandle;

#[cfg(all(unix, any(target_os = "linux", test)))]
mod desktop_entry;
#[cfg(all(unix, any(target_os = "linux", test)))]
mod entry;
pub mod install;
mod paths;
#[cfg(test)]
mod tests;
pub mod uninstall;

static PREPARED: OnceLock<Prepared> = OnceLock::new();
const RECORD: &str = "installation.json";

#[derive(Clone, Debug, Deserialize, Serialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct Registration {
    v: u32,
    identifier: String,
    generation: u64,
    app_path: PathBuf,
    version: String,
    image_hash: String,
    enabled: bool,
}

#[derive(Debug)]
struct Prepared {
    root: PathBuf,
    app_path: PathBuf,
    image: PathBuf,
    hash: String,
    identifier: String,
    version: String,
    generation: AtomicU64,
    integration_error: Mutex<Option<String>>,
}

/// Materialize before any hidden Go process starts, including auth and bridge calls.
pub fn prepare(app: &AppHandle) -> Result<(), String> {
    if cfg!(target_os = "macos") {
        return Ok(());
    }
    let prepared = prepare_metadata(
        &app.config().identifier,
        &app.package_info().version.to_string(),
    )?;
    PREPARED
        .set(prepared)
        .map_err(|_| "Desktop installation was prepared twice.".to_string())
}

fn prepare_metadata(identifier: &str, version: &str) -> Result<Prepared, String> {
    if !valid_identifier(identifier) {
        return Err("Unknown Rhizome desktop application identifier.".into());
    }
    let current = std::env::current_exe()
        .map_err(message)?
        .canonicalize()
        .map_err(message)?;
    let source = current
        .parent()
        .ok_or("Cannot locate the app installation.")?
        .join(if cfg!(windows) { "rzm.exe" } else { "rzm" });
    let app_path = paths::durable_app(&current)?;
    let root = paths::base()?.join(identifier);
    #[cfg(target_os = "linux")]
    let desktop_home = Some(paths::account_home()?);
    #[cfg(not(target_os = "linux"))]
    let desktop_home: Option<PathBuf> = None;
    prepare_image(
        root,
        app_path,
        identifier.into(),
        version.into(),
        &source,
        desktop_home.as_deref(),
    )
}

pub fn sidecar() -> Option<PathBuf> {
    PREPARED.get().map(|prepared| prepared.image.clone())
}

pub fn integration_error() -> Option<String> {
    PREPARED
        .get()
        .and_then(|prepared| prepared.integration_error.lock().unwrap().clone())
}

/// Only an explicit installation selection changes a different current app.
pub fn activate() -> Result<(), String> {
    change_registration(true)
}

/// Keep a disabled generation so an older app cannot recreate its registration.
pub fn disable() -> Result<(), String> {
    change_registration(false)
}

pub fn expected_generation() -> Result<u64, String> {
    let Some(prepared) = PREPARED.get() else {
        return Ok(0);
    };
    let _lock = lock(&prepared.root)?;
    let current = read_registration(&prepared.root)?.ok_or("Desktop registration is missing.")?;
    let generation = prepared.generation.load(Ordering::Acquire);
    if current.generation != generation || current.app_path != prepared.app_path {
        return Err(
            "The installed app changed. Reopen Rhizome before changing its integration.".into(),
        );
    }
    Ok(generation)
}

fn change_registration(enabled: bool) -> Result<(), String> {
    let Some(prepared) = PREPARED.get() else {
        return Ok(());
    };
    change_prepared_registration(prepared, enabled)
}

fn change_prepared_registration(prepared: &Prepared, enabled: bool) -> Result<(), String> {
    let _lock = lock(&prepared.root)?;
    let current = read_registration(&prepared.root)?.ok_or("Desktop registration is missing.")?;
    if current.generation != prepared.generation.load(Ordering::Acquire) {
        return Err(
            "The installed app changed. Reopen Rhizome before changing its integration.".into(),
        );
    }
    #[cfg(target_os = "linux")]
    {
        let result = if enabled {
            desktop_entry::register(
                &paths::account_home()?,
                &prepared.identifier,
                &prepared.app_path,
                Some(&current.app_path),
            )
        } else {
            desktop_entry::remove(
                &paths::account_home()?,
                &prepared.identifier,
                &prepared.app_path,
            )
        };
        *prepared.integration_error.lock().unwrap() = result.as_ref().err().cloned();
        result?;
    }
    let next = prepared.record(
        current
            .generation
            .checked_add(1)
            .ok_or("The desktop registration generation is exhausted.")?,
        enabled,
    );
    write_registration(&prepared.root, &next)?;
    prepared
        .generation
        .store(next.generation, Ordering::Release);
    Ok(())
}

/// Reject a delayed updater from an obsolete process before it replaces app files.
pub fn prepare_update() -> Result<(), String> {
    let Some(prepared) = PREPARED.get() else {
        return Ok(());
    };
    let _lock = lock(&prepared.root)?;
    let current = read_registration(&prepared.root)?.ok_or("Desktop registration is missing.")?;
    if current.generation != prepared.generation.load(Ordering::Acquire)
        || current.app_path != prepared.app_path
    {
        return Err("The installed app changed. Reopen Rhizome before updating it.".into());
    }
    Ok(())
}

impl Prepared {
    fn record(&self, generation: u64, enabled: bool) -> Registration {
        Registration {
            v: 1,
            identifier: self.identifier.clone(),
            generation,
            app_path: self.app_path.clone(),
            version: self.version.clone(),
            image_hash: self.hash.clone(),
            enabled,
        }
    }
}

fn prepare_image(
    root: PathBuf,
    app_path: PathBuf,
    identifier: String,
    version: String,
    source: &Path,
    _desktop_home: Option<&Path>,
) -> Result<Prepared, String> {
    if !app_path.is_absolute() || !app_path.is_file() {
        return Err("The installed Rhizome app is unavailable.".into());
    }
    fs::create_dir_all(&root).map_err(message)?;
    let root = root.canonicalize().map_err(message)?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        fs::set_permissions(&root, fs::Permissions::from_mode(0o700)).map_err(message)?;
    }
    let _lock = lock(&root)?;
    let hash = file_hash(source)?;
    let directory = root.join("images").join(&hash);
    fs::create_dir_all(&directory).map_err(message)?;
    let image = directory.join(if cfg!(windows) { "rzm.exe" } else { "rzm" });
    if image.exists() {
        if file_hash(&image)? != hash {
            return Err("An app-owned executable has changed. Reinstall Rhizome.".into());
        }
    } else {
        let mut staged = tempfile::NamedTempFile::new_in(&directory).map_err(message)?;
        let mut input = File::open(source).map_err(message)?;
        std::io::copy(&mut input, &mut staged).map_err(message)?;
        staged.as_file().sync_all().map_err(message)?;
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            staged
                .as_file()
                .set_permissions(fs::Permissions::from_mode(0o700))
                .map_err(message)?;
        }
        if file_hash(staged.path())? != hash {
            return Err("The app executable changed while it was being copied. Try again.".into());
        }
        staged.persist_noclobber(&image).map_err(message)?;
    }
    let current = read_registration(&root)?;
    let prepared = Prepared {
        root,
        app_path,
        image,
        hash,
        identifier,
        version,
        generation: AtomicU64::new(current.as_ref().map_or(0, |record| record.generation)),
        integration_error: Mutex::new(None),
    };
    let next = match current {
        None => Some(prepared.record(1, true)),
        Some(ref record) if record.enabled && can_finish_install(record, &prepared) => Some(
            prepared.record(
                record
                    .generation
                    .checked_add(1)
                    .ok_or("The desktop registration generation is exhausted.")?,
                true,
            ),
        ),
        _ => None,
    };
    #[cfg(all(unix, any(target_os = "linux", test)))]
    if let Some(home) = _desktop_home {
        if next.as_ref().or(current.as_ref()).is_some_and(|record| {
            record.enabled
                && record.app_path == prepared.app_path
                && record.image_hash == prepared.hash
        }) {
            let previous = current.as_ref().filter(|_| next.is_some());
            let result = desktop_entry::register(
                home,
                &prepared.identifier,
                &prepared.app_path,
                previous.map(|record| record.app_path.as_path()),
            );
            if let Err(error) = result {
                *prepared.integration_error.lock().unwrap() = Some(error);
                if previous.is_some_and(|record| record.app_path != prepared.app_path) {
                    // Preserve the old launcher's owner until relocation can be retried.
                    return Ok(prepared);
                }
            }
        }
    }
    if let Some(record) = next {
        write_registration(&prepared.root, &record)?;
        prepared
            .generation
            .store(record.generation, Ordering::Release);
    }
    #[cfg(target_os = "linux")]
    if let Some(record) = read_registration(&prepared.root)? {
        if record.enabled
            && record.app_path == prepared.app_path
            && record.image_hash == prepared.hash
        {
            entry::refresh(&prepared.root, &prepared.image, &paths::cli_entry()?)?;
        }
    }
    // shortcut: retain old images, add collection only with proven active-use tracking.
    Ok(prepared)
}

fn can_finish_install(current: &Registration, prepared: &Prepared) -> bool {
    if current.image_hash == prepared.hash && current.app_path == prepared.app_path {
        return false;
    }
    if current.app_path != prepared.app_path && current.app_path.is_file() {
        return false;
    }
    if prepared.identifier.ends_with(".dev") {
        return true;
    }
    semver::Version::parse(&prepared.version)
        .ok()
        .zip(semver::Version::parse(&current.version).ok())
        .is_some_and(|(new, old)| new > old || new == old && current.image_hash == prepared.hash)
}

fn valid_identifier(identifier: &str) -> bool {
    matches!(
        identifier,
        "com.atomicobject.rhizome" | "com.atomicobject.rhizome.dev"
    )
}

fn read_registration(root: &Path) -> Result<Option<Registration>, String> {
    let file = match File::open(root.join(RECORD)) {
        Ok(file) => file,
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => return Ok(None),
        Err(error) => return Err(message(error)),
    };
    let mut bytes = Vec::new();
    file.take(64 * 1024 + 1)
        .read_to_end(&mut bytes)
        .map_err(message)?;
    if bytes.len() > 64 * 1024 {
        return Err("The desktop registration is too large.".into());
    }
    let record: Registration = serde_json::from_slice(&bytes).map_err(message)?;
    if record.v != 1
        || record.generation == 0
        || !valid_identifier(&record.identifier)
        || root.file_name().and_then(|name| name.to_str()) != Some(record.identifier.as_str())
        || !record.app_path.is_absolute()
        || record.version.is_empty()
        || record.image_hash.len() != 64
        || !record
            .image_hash
            .bytes()
            .all(|value| value.is_ascii_digit() || (b'a'..=b'f').contains(&value))
    {
        return Err("The desktop registration is invalid. Reinstall Rhizome.".into());
    }
    Ok(Some(record))
}

fn write_registration(root: &Path, record: &Registration) -> Result<(), String> {
    let mut staged = tempfile::NamedTempFile::new_in(root).map_err(message)?;
    serde_json::to_writer(&mut staged, record).map_err(message)?;
    staged.write_all(b"\n").map_err(message)?;
    staged.as_file().sync_all().map_err(message)?;
    staged.persist(root.join(RECORD)).map_err(message)?;
    Ok(())
}

fn lock(root: &Path) -> Result<File, String> {
    let file = OpenOptions::new()
        .create(true)
        .truncate(false)
        .read(true)
        .write(true)
        .open(root.join("installation.lock"))
        .map_err(message)?;
    let deadline = std::time::Instant::now() + std::time::Duration::from_secs(10);
    loop {
        match file.try_lock_exclusive() {
            Ok(()) => break,
            Err(error) if error.kind() == std::io::ErrorKind::WouldBlock => {
                if std::time::Instant::now() >= deadline {
                    return Err(
                        "Another process is changing the Rhizome installation. Try again.".into(),
                    );
                }
                std::thread::sleep(std::time::Duration::from_millis(25));
            }
            Err(error) => return Err(message(error)),
        }
    }
    Ok(file)
}

fn file_hash(path: &Path) -> Result<String, String> {
    let mut file = File::open(path).map_err(message)?;
    let mut hash = Sha256::new();
    let mut buffer = [0u8; 64 * 1024];
    loop {
        let read = file.read(&mut buffer).map_err(message)?;
        if read == 0 {
            break;
        }
        hash.update(&buffer[..read]);
    }
    Ok(format!("{:x}", hash.finalize()))
}

fn message(error: impl std::fmt::Display) -> String {
    error.to_string()
}
