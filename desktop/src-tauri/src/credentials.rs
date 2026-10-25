use crate::bridge::{self, Failure, Request};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::{
    path::{Path, PathBuf},
    sync::{
        atomic::{AtomicU64, Ordering},
        Mutex,
    },
};

#[derive(Deserialize, Serialize)]
#[serde(deny_unknown_fields)]
pub struct Source {
    account: String,
    environment: String,
    keys: Vec<String>,
}

#[derive(Default)]
pub struct Imports(Mutex<Option<PathBuf>>);
static NEXT: AtomicU64 = AtomicU64::new(0);

struct Marker<'a> {
    path: PathBuf,
    imports: &'a Imports,
}
impl Drop for Marker<'_> {
    fn drop(&mut self) {
        let _ = std::fs::remove_file(&self.path);
        let mut active = self.imports.0.lock().unwrap();
        if active.as_ref() == Some(&self.path) {
            *active = None;
        }
    }
}

fn private_directory(directory: &Path) -> Result<(), std::io::Error> {
    std::fs::create_dir_all(directory)?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        std::fs::set_permissions(directory, std::fs::Permissions::from_mode(0o700))?;
    }
    Ok(())
}

impl Imports {
    pub fn cancel(&self) {
        if let Some(path) = self.0.lock().unwrap().as_ref() {
            let _ = std::fs::remove_file(path);
        }
    }

    fn begin(&self, directory: &Path) -> Result<(String, Marker<'_>), Failure> {
        let id = format!(
            "{}-{}",
            std::process::id(),
            NEXT.fetch_add(1, Ordering::Relaxed)
        );
        let path = directory.join(format!("credential-import-{id}"));
        {
            let mut active = self.0.lock().unwrap();
            if active.is_some() {
                return Err(
                    "A 1Password import is already running. Cancel it or wait for it to finish."
                        .to_string()
                        .into(),
                );
            }
            private_directory(directory)
                .map_err(|_| "Cannot make the desktop data directory private.".to_string())?;
            let mut options = std::fs::OpenOptions::new();
            options.write(true).create_new(true);
            #[cfg(unix)]
            {
                use std::os::unix::fs::OpenOptionsExt;
                options.mode(0o600);
            }
            options.open(&path).map_err(|_| {
                "Cannot start the 1Password import. Check desktop data directory permissions."
                    .to_string()
            })?;
            *active = Some(path.clone());
        }
        Ok((
            id,
            Marker {
                path,
                imports: self,
            },
        ))
    }

    pub async fn import(&self, directory: &Path, source: &Source) -> Result<Value, Failure> {
        let (id, _marker) = self.begin(directory)?;
        bridge::call(
            directory,
            Request {
                operation: "credential-import",
                source: Some(source),
                import_id: Some(&id),
                ..Request::default()
            },
            bridge::TIMEOUT,
        )
        .await
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn marker_drop_cancels_and_releases_import_for_retry() {
        let directory = tempfile::tempdir().unwrap();
        private_directory(directory.path()).unwrap();
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            assert_eq!(
                std::fs::metadata(directory.path())
                    .unwrap()
                    .permissions()
                    .mode()
                    & 0o777,
                0o700
            );
        }
        let imports = Imports::default();
        let (_, marker) = imports.begin(directory.path()).unwrap();
        let path = marker.path.clone();
        assert!(path.exists());
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            assert_eq!(
                std::fs::metadata(&path).unwrap().permissions().mode() & 0o777,
                0o600
            );
        }
        imports.cancel();
        assert!(!path.exists());
        drop(marker);
        assert!(imports.0.lock().unwrap().is_none());
    }
    #[test]
    fn source_rejects_secret_payload_fields() {
        assert!(serde_json::from_value::<Source>(serde_json::json!({"account":"example.1password.com","environment":"aaaaaaaaaaaaaaaaaaaaaaaaaa","keys":["VOYAGE_API_KEY"],"value":"synthetic-secret"})).is_err());
    }
}
