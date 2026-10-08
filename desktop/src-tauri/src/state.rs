use serde::{de::DeserializeOwned, Deserialize, Serialize};
use std::{
    collections::{BTreeMap, BTreeSet},
    fs,
    io::Write,
    path::{Path, PathBuf},
};

const LIBRARY: &str = "library.json";
const LEGACY: &str = "folders.json";
const SESSION: &str = "session.json";

#[derive(Clone, Debug, Deserialize, Serialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Folder {
    pub id: String,
    pub path: String,
    pub name: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub executable: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub last_opened: Option<u64>,
}

/// The version 1 folder library, read only for migration.
#[derive(Clone, Debug, Default, Deserialize, Serialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Registry {
    pub version: u8,
    pub folders: Vec<Folder>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub global_executable: Option<String>,
}

/// A repository is identified by its canonical common Git directory, or by its
/// folder outside Git. Worktree paths key per-worktree settings.
#[derive(Clone, Debug, Default, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Repository {
    pub id: String,
    pub root: String,
    pub name: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub primary: Option<String>,
    #[serde(default, skip_serializing_if = "BTreeMap::is_empty")]
    pub executables: BTreeMap<String, String>,
    #[serde(default)]
    pub acknowledged: BTreeSet<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub last_opened: Option<u64>,
}

#[derive(Clone, Debug, Deserialize, Serialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct Library {
    pub version: u8,
    pub repositories: Vec<Repository>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub global_executable: Option<String>,
}

impl Default for Library {
    fn default() -> Self {
        Self {
            version: 2,
            repositories: vec![],
            global_executable: None,
        }
    }
}

/// What the bridge reports about a repository: its identity and worktrees.
#[derive(Clone, Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Discovery {
    pub id: String,
    pub root: String,
    pub name: String,
    pub primary: String,
    /// The vault's folder below each Git worktree root, for a subfolder vault.
    #[serde(default)]
    pub subpath: String,
    pub worktrees: Vec<DiscoveredWorktree>,
}

#[derive(Clone, Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct DiscoveredWorktree {
    pub path: String,
    #[serde(default)]
    pub configured: bool,
    #[serde(default)]
    pub trust_required: bool,
    #[serde(default)]
    pub has_database: bool,
    #[serde(default)]
    pub error: Option<String>,
}

pub enum Stored {
    Current(Library),
    Legacy(Registry),
}

fn read<T: DeserializeOwned>(file: &Path, label: &str) -> Result<Option<T>, String> {
    let data = match fs::read(file) {
        Ok(data) => data,
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => return Ok(None),
        Err(error) => return Err(format!("Cannot read {label}: {error}")),
    };
    serde_json::from_slice(&data)
        .map(Some)
        .map_err(|error| format!("{label} is invalid. Your file has been preserved: {error}"))
}

fn write<T: Serialize>(directory: &Path, name: &str, value: &T) -> Result<(), String> {
    fs::create_dir_all(directory).map_err(|e| format!("Cannot create app data: {e}"))?;
    let mut pending = tempfile::NamedTempFile::new_in(directory).map_err(|e| e.to_string())?;
    serde_json::to_writer_pretty(&mut pending, value).map_err(|e| e.to_string())?;
    pending.write_all(b"\n").map_err(|e| e.to_string())?;
    pending.as_file().sync_all().map_err(|e| e.to_string())?;
    pending
        .persist(directory.join(name))
        .map_err(|e| format!("Cannot save {name}: {e}"))?;
    Ok(())
}

fn absolute(path: &str) -> bool {
    Path::new(path).is_absolute()
}

impl Registry {
    fn validate(self) -> Result<Self, String> {
        if self.version != 1 {
            return Err(
                "Saved folders use an unsupported format. Your file has been preserved.".into(),
            );
        }
        if self
            .folders
            .iter()
            .any(|f| !absolute(&f.path) || !valid_id(&f.id))
        {
            return Err(
                "Saved folder has an invalid path or identifier. Your file has been preserved."
                    .into(),
            );
        }
        Ok(self)
    }
}

impl Library {
    /// Reads `library.json`, falling back to a version 1 `folders.json` that the
    /// caller migrates. The legacy file is never modified.
    pub fn load(directory: &Path) -> Result<Stored, String> {
        if let Some(library) = read::<Self>(&directory.join(LIBRARY), "Saved repositories")? {
            return library.validate().map(Stored::Current);
        }
        Ok(Stored::Legacy(
            read::<Registry>(&directory.join(LEGACY), "Saved folders")?
                .map(Registry::validate)
                .transpose()?
                .unwrap_or(Registry {
                    version: 1,
                    ..Registry::default()
                }),
        ))
    }

    fn validate(self) -> Result<Self, String> {
        if self.version != 2 {
            return Err(
                "Saved repositories use an unsupported format. Your file has been preserved."
                    .into(),
            );
        }
        let paths_valid = |r: &Repository| {
            absolute(&r.root)
                && r.primary.as_deref().map_or(true, absolute)
                && r.executables
                    .iter()
                    .all(|(k, v)| absolute(k) && absolute(v))
                && r.acknowledged.iter().all(|p| absolute(p))
        };
        if self
            .repositories
            .iter()
            .any(|r| !valid_id(&r.id) || !paths_valid(r))
        {
            return Err(
                "Saved repository has an invalid path or identifier. Your file has been preserved."
                    .into(),
            );
        }
        Ok(self)
    }

    pub fn save(&self, directory: &Path) -> Result<(), String> {
        write(directory, LIBRARY, self)
    }

    pub fn repository(&self, id: &str) -> Result<&Repository, String> {
        self.repositories
            .iter()
            .find(|r| r.id == id)
            .ok_or_else(|| "This repository is no longer in your library.".into())
    }

    pub fn repository_mut(&mut self, id: &str) -> Result<&mut Repository, String> {
        self.repositories
            .iter_mut()
            .find(|r| r.id == id)
            .ok_or_else(|| "This repository is no longer in your library.".into())
    }

    /// Marks every worktree `found` lists as seen in its repository, so the
    /// new marker shows only worktrees that appear after the repository was
    /// last opened. Returns whether anything changed.
    pub fn acknowledge(&mut self, found: &Discovery) -> Result<bool, String> {
        let repository = self.repository_mut(&found.id)?;
        let before = repository.acknowledged.len();
        repository
            .acknowledged
            .extend(found.worktrees.iter().map(|w| w.path.clone()));
        Ok(repository.acknowledged.len() != before)
    }

    /// Orders repositories as `ids` lists them. Repositories it omits, such as
    /// one another window added meanwhile, keep their order after the rest.
    pub fn reorder(&mut self, ids: &[String]) {
        self.repositories
            .sort_by_key(|r| ids.iter().position(|id| *id == r.id).unwrap_or(usize::MAX));
    }

    /// Adds a newly discovered repository, acknowledging the worktrees present
    /// now. An existing entry with the same identity keeps its settings. Returns
    /// whether the library changed.
    pub fn add(&mut self, found: &Discovery) -> bool {
        if self.adopt(found) {
            return true;
        }
        if self.repositories.iter().any(|r| r.id == found.id) {
            return false;
        }
        self.repositories.push(Repository::discovered(found));
        true
    }

    /// Merges entries saved under another identity for one of the discovered
    /// repository's worktrees, such as a version 1 folder that was unavailable
    /// during migration, into that repository's entry, keeping their
    /// executables and recency. The merged entry takes the first one's place.
    /// Returns false when there is nothing to merge.
    pub fn adopt(&mut self, found: &Discovery) -> bool {
        let worktrees: BTreeSet<&str> = found.worktrees.iter().map(|w| w.path.as_str()).collect();
        let stale = |r: &Repository| r.id != found.id && worktrees.contains(r.root.as_str());
        if !self.repositories.iter().any(stale) {
            return false;
        }
        let at = self
            .repositories
            .iter()
            .position(|r| r.id == found.id || stale(r))
            .unwrap_or_default();
        let mut merged = self
            .repositories
            .iter()
            .find(|r| r.id == found.id)
            .cloned()
            .unwrap_or_else(|| Repository::discovered(found));
        for legacy in self.repositories.iter().filter(|r| stale(r)) {
            for (path, executable) in &legacy.executables {
                merged
                    .executables
                    .entry(path.clone())
                    .or_insert_with(|| executable.clone());
            }
            merged
                .acknowledged
                .extend(legacy.acknowledged.iter().cloned());
            merged.last_opened = merged.last_opened.max(legacy.last_opened);
            merged.primary = merged.primary.take().or_else(|| legacy.primary.clone());
        }
        self.repositories.retain(|r| r.id != found.id && !stale(r));
        self.repositories.insert(at, merged);
        true
    }
}

impl Repository {
    fn discovered(found: &Discovery) -> Self {
        Self {
            id: found.id.clone(),
            root: found.root.clone(),
            name: found.name.clone(),
            acknowledged: found.worktrees.iter().map(|w| w.path.clone()).collect(),
            ..Self::default()
        }
    }
}

/// Converts version 1 folders into repositories. Folders that resolve to one
/// repository merge; folders that cannot be resolved stay as their own entry.
pub fn migrate(legacy: Registry, found: &[Option<Discovery>]) -> Library {
    let mut library = Library {
        global_executable: legacy.global_executable,
        ..Library::default()
    };
    for (folder, found) in legacy.folders.into_iter().zip(found) {
        let identity = match found {
            Some(found) => found.clone(),
            None => Discovery {
                id: folder.id.clone(),
                root: folder.path.clone(),
                name: folder.name.clone(),
                primary: folder.path.clone(),
                subpath: String::new(),
                worktrees: vec![],
            },
        };
        library.add(&identity);
        let repository = library.repository_mut(&identity.id).unwrap();
        repository.acknowledged.insert(folder.path.clone());
        if let Some(executable) = folder.executable {
            repository.executables.insert(folder.path, executable);
        }
        repository.last_opened = repository.last_opened.max(folder.last_opened);
    }
    library
}

#[derive(Clone, Debug, Default, Deserialize, Serialize, PartialEq)]
#[serde(rename_all = "camelCase")]
pub struct WindowSession {
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub x: Option<f64>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub y: Option<f64>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub width: Option<f64>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub height: Option<f64>,
    #[serde(default)]
    pub sidebar_collapsed: bool,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub repository: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub worktree: Option<String>,
}

#[derive(Debug, Default, Deserialize, Serialize)]
pub struct Session {
    pub windows: Vec<WindowSession>,
    /// The last window closed while no other was open, restored when the app
    /// next has no windows to restore.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub closed: Option<WindowSession>,
}

impl Session {
    /// An unreadable session restores one default window; it holds no user data.
    pub fn load(directory: &Path) -> Self {
        read(&directory.join(SESSION), "Session")
            .ok()
            .flatten()
            .unwrap_or_default()
    }

    pub fn save(&self, directory: &Path) -> Result<(), String> {
        write(directory, SESSION, self)
    }
}

pub fn valid_id(id: &str) -> bool {
    id.len() == 64 && id.bytes().all(|c| c.is_ascii_hexdigit())
}

pub fn data_directory(default: PathBuf) -> Result<PathBuf, String> {
    match std::env::var_os("RHIZOME_DESKTOP_DATA_DIR") {
        Some(value) => {
            let path = PathBuf::from(value);
            if !path.is_absolute() {
                return Err("RHIZOME_DESKTOP_DATA_DIR must be absolute.".into());
            }
            Ok(path)
        }
        None => Ok(default),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn opening_a_repository_acknowledges_every_worktree_it_has() {
        let mut library = Library {
            repositories: vec![Repository::discovered(&discovery(
                'a',
                "/work/.git",
                &["/work/project"],
            ))],
            ..Library::default()
        };
        let now = discovery(
            'a',
            "/work/.git",
            &["/work/project", "/work/agent-1", "/work/agent-2"],
        );
        assert_eq!(library.acknowledge(&now), Ok(true));
        let acknowledged = &library.repositories[0].acknowledged;
        assert!(["/work/project", "/work/agent-1", "/work/agent-2"]
            .iter()
            .all(|p| acknowledged.contains(*p)));
        assert_eq!(library.acknowledge(&now), Ok(false));
        assert!(library
            .acknowledge(&discovery('b', "/other/.git", &["/other"]))
            .is_err());
    }

    fn folder(id: char, path: &str, executable: Option<&str>, last_opened: Option<u64>) -> Folder {
        Folder {
            id: id.to_string().repeat(64),
            path: path.into(),
            name: Path::new(path)
                .file_name()
                .unwrap()
                .to_str()
                .unwrap()
                .into(),
            executable: executable.map(Into::into),
            last_opened,
        }
    }

    fn discovery(id: char, root: &str, worktrees: &[&str]) -> Discovery {
        Discovery {
            id: id.to_string().repeat(64),
            root: root.into(),
            name: "project".into(),
            primary: worktrees[0].into(),
            subpath: String::new(),
            worktrees: worktrees
                .iter()
                .map(|p| DiscoveredWorktree {
                    path: (*p).into(),
                    configured: true,
                    trust_required: false,
                    has_database: false,
                    error: None,
                })
                .collect(),
        }
    }

    #[test]
    fn reordering_keeps_unlisted_repositories_after_the_listed_ones() {
        let mut library = Library::default();
        for (id, root) in [('a', "/a"), ('b', "/b"), ('c', "/c"), ('d', "/d")] {
            library.add(&discovery(id, root, &[root]));
        }
        let id = |c: char| c.to_string().repeat(64);
        library.reorder(&[id('c'), id('a'), "gone".into()]);
        let roots: Vec<_> = library
            .repositories
            .iter()
            .map(|r| r.root.as_str())
            .collect();
        assert_eq!(roots, ["/c", "/a", "/b", "/d"]);
    }

    #[test]
    fn worktree_folders_merge_into_one_repository_when_migrating() {
        let legacy = Registry {
            version: 1,
            folders: vec![
                folder('a', "/work/project", None, Some(10)),
                folder('b', "/work/project-feature", Some("/tools/rzm"), Some(30)),
                folder('c', "/gone/notes", Some("/tools/old"), Some(20)),
            ],
            global_executable: Some("/usr/local/bin/rzm".into()),
        };
        let repo = discovery(
            'd',
            "/work/project/.git",
            &[
                "/work/project",
                "/work/project-feature",
                "/work/project-new",
            ],
        );
        let library = migrate(legacy, &[Some(repo.clone()), Some(repo), None]);
        assert_eq!(
            library.global_executable.as_deref(),
            Some("/usr/local/bin/rzm")
        );
        assert_eq!(library.repositories.len(), 2);
        let merged = &library.repositories[0];
        assert_eq!(merged.id, "d".repeat(64));
        assert_eq!(merged.root, "/work/project/.git");
        assert_eq!(merged.last_opened, Some(30));
        assert_eq!(
            merged.executables,
            BTreeMap::from([("/work/project-feature".into(), "/tools/rzm".into())])
        );
        assert!(merged.acknowledged.contains("/work/project-new"));
        let missing = &library.repositories[1];
        assert_eq!(missing.id, "c".repeat(64));
        assert_eq!(missing.root, "/gone/notes");
        assert_eq!(missing.executables["/gone/notes"], "/tools/old");
        assert_eq!(missing.last_opened, Some(20));
    }

    #[test]
    fn an_unavailable_legacy_folder_merges_once_it_resolves() {
        let legacy = Registry {
            version: 1,
            folders: vec![
                folder('a', "/work/project", None, Some(10)),
                folder('n', "/work/notes", None, Some(5)),
                folder('b', "/work/project-feature", Some("/tools/rzm"), Some(30)),
            ],
            global_executable: None,
        };
        let repo = discovery(
            'd',
            "/work/project/.git",
            &["/work/project", "/work/project-feature"],
        );
        let mut library = migrate(legacy, &[Some(repo.clone()), None, None]);
        assert_eq!(library.repositories.len(), 3);
        assert!(library.adopt(&repo));
        assert!(!library.adopt(&repo), "nothing is left to merge");
        let ids: Vec<_> = library.repositories.iter().map(|r| &r.id[..1]).collect();
        assert_eq!(ids, ["d", "n"]);
        let merged = &library.repositories[0];
        assert_eq!(merged.last_opened, Some(30));
        assert_eq!(merged.executables["/work/project-feature"], "/tools/rzm");
        assert!(merged.acknowledged.contains("/work/project-feature"));
    }

    #[test]
    fn adding_a_worktree_merges_its_unresolved_legacy_entry() {
        let mut library = migrate(
            Registry {
                version: 1,
                folders: vec![
                    folder('n', "/work/notes", None, None),
                    folder('b', "/work/project-feature", Some("/tools/rzm"), Some(30)),
                ],
                global_executable: None,
            },
            &[None, None],
        );
        let repo = discovery(
            'd',
            "/work/project/.git",
            &[
                "/work/project",
                "/work/project-feature",
                "/work/project-new",
            ],
        );
        assert!(library.add(&repo));
        assert_eq!(library.repositories.len(), 2);
        let merged = &library.repositories[1];
        assert_eq!(merged.id, "d".repeat(64));
        assert_eq!(merged.root, "/work/project/.git");
        assert_eq!(merged.last_opened, Some(30));
        assert_eq!(merged.executables["/work/project-feature"], "/tools/rzm");
        assert!(merged.acknowledged.contains("/work/project-new"));
        assert!(!library.add(&repo));
    }

    #[test]
    fn legacy_file_is_read_but_never_rewritten() {
        let directory = tempfile::tempdir().unwrap();
        let legacy = r#"{"version":1,"folders":[{"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","path":"/work/project","name":"project","lastOpened":4}]}"#;
        fs::write(directory.path().join(LEGACY), legacy).unwrap();
        let Stored::Legacy(registry) = Library::load(directory.path()).unwrap() else {
            panic!("expected the legacy library");
        };
        migrate(registry, &[None]).save(directory.path()).unwrap();
        assert_eq!(
            fs::read_to_string(directory.path().join(LEGACY)).unwrap(),
            legacy
        );
        let Stored::Current(library) = Library::load(directory.path()).unwrap() else {
            panic!("expected the migrated library");
        };
        assert_eq!(library.repositories[0].last_opened, Some(4));
    }

    #[test]
    fn adding_a_known_repository_keeps_its_settings() {
        let mut library = Library::default();
        let found = discovery('a', "/work/project/.git", &["/work/project"]);
        library.add(&found);
        library.repositories[0].primary = Some("/work/project".into());
        library.add(&discovery(
            'a',
            "/work/project/.git",
            &["/work/project", "/work/other"],
        ));
        assert_eq!(library.repositories.len(), 1);
        assert_eq!(
            library.repositories[0].primary.as_deref(),
            Some("/work/project")
        );
        assert!(!library.repositories[0].acknowledged.contains("/work/other"));
    }

    #[test]
    fn missing_repository_survives_save_and_reload() {
        let directory = tempfile::tempdir().unwrap();
        let mut library = Library::default();
        library.add(&discovery(
            'a',
            "/no-longer-mounted/project",
            &["/no-longer-mounted/project"],
        ));
        library.save(directory.path()).unwrap();
        let Stored::Current(loaded) = Library::load(directory.path()).unwrap() else {
            panic!("expected the saved library");
        };
        assert_eq!(loaded.repositories, library.repositories);
    }

    #[test]
    fn corruption_and_unknown_versions_are_not_overwritten() {
        for (name, contents) in [
            (LIBRARY, "broken"),
            (LIBRARY, "{\"version\":3,\"repositories\":[]}"),
            (LEGACY, "broken"),
            (LEGACY, "{\"version\":2,\"folders\":[]}"),
        ] {
            let directory = tempfile::tempdir().unwrap();
            let file = directory.path().join(name);
            fs::write(&file, contents).unwrap();
            assert!(
                Library::load(directory.path()).is_err(),
                "{name} {contents}"
            );
            assert_eq!(fs::read_to_string(file).unwrap(), contents);
        }
    }
}
