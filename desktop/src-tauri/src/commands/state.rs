use super::discovery;
use crate::{
    bridge::{self, Failure, Request},
    state::{managed_executable, migrate, Library, Stored},
};
use serde_json::{json, Value};
use std::{
    collections::HashMap,
    path::PathBuf,
    sync::{
        atomic::{AtomicU64, Ordering},
        Arc,
    },
};
use tokio::sync::{Mutex, MutexGuard};

/// Serializes library file updates, command-line link changes, and each worktree's
/// open, trust, and setup work. Different worktrees never wait on each other.
pub struct Desktop {
    pub directory: PathBuf,
    library: Mutex<()>,
    /// Counts library saves since launch. Shells apply a library snapshot only
    /// when its revision is newer than the one they hold, so a snapshot that
    /// arrives late cannot restore an entry a later save removed.
    revision: AtomicU64,
    pub(super) global: Mutex<()>,
    // ponytail: one small lock per worktree ever touched, never pruned.
    worktrees: std::sync::Mutex<HashMap<String, Arc<Mutex<()>>>>,
}

impl Desktop {
    pub fn new(directory: PathBuf) -> Self {
        Self {
            directory,
            library: Mutex::default(),
            revision: AtomicU64::new(0),
            global: Mutex::default(),
            worktrees: std::sync::Mutex::default(),
        }
    }

    pub fn worktree(&self, path: &str) -> Arc<Mutex<()>> {
        self.worktrees
            .lock()
            .unwrap()
            .entry(path.into())
            .or_default()
            .clone()
    }

    /// Loads the library, migrating a version 1 folder library on first use.
    /// Hold the returned guard while changing and saving the library.
    pub async fn library(&self) -> Result<(MutexGuard<'_, ()>, Library), Failure> {
        let guard = self.library.lock().await;
        let library = match Library::load(&self.directory)? {
            Stored::Current(library) => library,
            Stored::Legacy(legacy) if legacy.folders.is_empty() => {
                let mut library = Library {
                    global_executable: legacy.global_executable,
                    ..Library::default()
                };
                library.forget_managed_executable(managed_executable().as_deref());
                library
            }
            Stored::Legacy(legacy) => {
                let mut found = vec![];
                for folder in &legacy.folders {
                    found.push(match self.discover(&folder.path, None).await {
                        Ok(value) => Some(discovery(&value)?),
                        Err(failure) if failure.code == "desktop_error" => return Err(failure),
                        Err(_) => None,
                    });
                }
                let mut library = migrate(legacy, &found);
                library.forget_managed_executable(managed_executable().as_deref());
                self.store(&library)?;
                library
            }
        };
        Ok((guard, library))
    }

    /// Writes the library under the next revision and returns the snapshot
    /// shells receive. Hold the library guard.
    pub(super) fn store(&self, library: &Library) -> Result<Value, Failure> {
        library.save(&self.directory)?;
        self.revision.fetch_add(1, Ordering::SeqCst);
        Ok(self.snapshot(library))
    }

    /// The library as shells receive it, with the revision of the last save.
    /// Hold the library guard, so the revision is the one that wrote it.
    pub fn snapshot(&self, library: &Library) -> Value {
        let mut snapshot = json!(library);
        snapshot["revision"] = self.revision.load(Ordering::SeqCst).into();
        snapshot
    }

    pub async fn discover(&self, path: &str, primary: Option<&str>) -> Result<Value, Failure> {
        bridge::call(
            &self.directory,
            Request {
                operation: "repository",
                folder: Some(path),
                primary,
                ..Request::default()
            },
            bridge::TIMEOUT,
        )
        .await
    }
}
