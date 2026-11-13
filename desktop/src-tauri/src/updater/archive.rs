use flate2::read::GzDecoder;
use std::{
    collections::HashSet,
    io,
    path::{Component, Path, PathBuf},
};

fn invalid(message: &str) -> io::Error {
    io::Error::new(io::ErrorKind::InvalidData, message)
}

fn relative(path: &Path) -> io::Result<()> {
    if path.as_os_str().is_empty()
        || path
            .components()
            .any(|c| !matches!(c, Component::Normal(_)))
    {
        return Err(invalid("Update archive contains an unsafe path"));
    }
    Ok(())
}

fn link_target(path: &Path, target: &Path, contents: &Path) -> io::Result<()> {
    let mut resolved = path.parent().unwrap().to_path_buf();
    for part in target.components() {
        match part {
            Component::Normal(name) => resolved.push(name),
            Component::CurDir => {}
            Component::ParentDir if resolved != contents => {
                resolved.pop();
            }
            _ => return Err(invalid("Update archive link escapes Contents")),
        }
    }
    if !resolved.starts_with(contents) {
        return Err(invalid("Update archive link escapes Contents"));
    }
    Ok(())
}

// Scan the entire archive before extraction; never write through an archive symlink.
pub(super) fn extract(bytes: &[u8], destination: &Path) -> io::Result<PathBuf> {
    let mut root: Option<PathBuf> = None;
    let mut paths = HashSet::new();
    let mut links = HashSet::new();
    let mut archive = tar::Archive::new(GzDecoder::new(bytes));
    for entry in archive.entries()? {
        let entry = entry?;
        let path = entry.path()?.into_owned();
        relative(&path)?;
        let first = PathBuf::from(path.components().next().unwrap().as_os_str());
        if first.extension().map_or(true, |ext| ext != "app")
            || root.as_ref().is_some_and(|r| r != &first)
        {
            return Err(invalid("Update archive must contain one app bundle"));
        }
        root = Some(first.clone());
        let contents = first.join("Contents");
        let kind = entry.header().entry_type();
        if path != first && !path.starts_with(&contents) {
            return Err(invalid("Update bundle contains an unsupported root entry"));
        }
        if (path == first || path == contents) && !kind.is_dir() {
            return Err(invalid(
                "Update bundle root and Contents must be directories",
            ));
        }
        if !paths.insert(path.clone()) {
            return Err(invalid("Update archive contains a duplicate path"));
        }
        if kind.is_symlink() {
            let target = entry
                .link_name()?
                .ok_or_else(|| invalid("Missing symlink target"))?;
            link_target(&path, &target, &contents)?;
            links.insert(path);
        } else if !kind.is_file() && !kind.is_dir() {
            return Err(invalid(
                "Update archive contains a hardlink or special file",
            ));
        }
    }
    let root = root.ok_or_else(|| invalid("Empty update archive"))?;
    for path in &paths {
        if path
            .ancestors()
            .skip(1)
            .any(|parent| links.contains(parent))
        {
            return Err(invalid("Update archive writes through a symlink"));
        }
    }
    // Materialize ordinary entries first so even forward symlinks cannot redirect writes.
    for symlinks in [false, true] {
        let mut archive = tar::Archive::new(GzDecoder::new(bytes));
        for entry in archive.entries()? {
            let mut entry = entry?;
            if entry.header().entry_type().is_symlink() == symlinks
                && !entry.unpack_in(destination)?
            {
                return Err(invalid("Cannot extract update entry"));
            }
        }
    }
    let bundle = destination.join(root);
    let contents = bundle.join("Contents").canonicalize()?;
    for link in links {
        if !destination
            .join(link)
            .canonicalize()?
            .starts_with(&contents)
        {
            return Err(invalid(
                "Update archive contains an escaping or cyclic symlink",
            ));
        }
    }
    Ok(bundle)
}
