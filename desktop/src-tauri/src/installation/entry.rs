use super::{lock, message};
use std::{fs, path::Path};

pub(super) fn refresh(root: &Path, image: &Path, entry: &Path) -> Result<(), String> {
    // Both channels share the terminal entry; Go install/remove use this lock too.
    let _lock = lock(root.parent().ok_or("Invalid desktop installation root.")?)?;
    let target = match fs::read_link(entry) {
        Ok(target) => target,
        Err(error)
            if matches!(
                error.kind(),
                std::io::ErrorKind::NotFound | std::io::ErrorKind::InvalidInput
            ) =>
        {
            return Ok(())
        }
        Err(error) => return Err(message(error)),
    };
    let target = if target.is_absolute() {
        target
    } else {
        entry
            .parent()
            .ok_or("Invalid command-line entry.")?
            .join(target)
    };
    let hash = target
        .parent()
        .and_then(Path::file_name)
        .and_then(|name| name.to_str());
    if target.parent().and_then(Path::parent) != Some(root.join("images").as_path())
        || target.file_name() != Some(std::ffi::OsStr::new("rzm"))
        || !hash.is_some_and(|value| {
            value.len() == 64
                && value
                    .bytes()
                    .all(|byte| byte.is_ascii_digit() || (b'a'..=b'f').contains(&byte))
        })
        || target == image
    {
        return Ok(());
    }
    let staged =
        tempfile::NamedTempFile::new_in(entry.parent().ok_or("Invalid command-line entry.")?)
            .map_err(message)?;
    let temporary = staged.into_temp_path();
    fs::remove_file(&temporary).map_err(message)?;
    std::os::unix::fs::symlink(image, &temporary).map_err(message)?;
    fs::rename(&temporary, entry).map_err(message)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn update_refreshes_only_an_existing_owned_entry() {
        let temp = tempfile::tempdir().unwrap();
        let root = temp.path().join("com.atomicobject.rhizome");
        fs::create_dir_all(&root).unwrap();
        let old = root.join("images").join("a".repeat(64)).join("rzm");
        let new = root.join("images").join("b".repeat(64)).join("rzm");
        let entry = temp.path().join("rzm");
        refresh(&root, &new, &entry).unwrap();
        assert!(!entry.exists());
        std::os::unix::fs::symlink(&old, &entry).unwrap();
        refresh(&root, &new, &entry).unwrap();
        assert_eq!(fs::read_link(&entry).unwrap(), new);
        fs::remove_file(&entry).unwrap();
        let foreign = temp.path().join("foreign/rzm");
        std::os::unix::fs::symlink(&foreign, &entry).unwrap();
        refresh(&root, &new, &entry).unwrap();
        assert_eq!(fs::read_link(&entry).unwrap(), foreign);
        fs::remove_file(&entry).unwrap();
        fs::write(&entry, "foreign command").unwrap();
        refresh(&root, &new, &entry).unwrap();
        assert_eq!(fs::read(&entry).unwrap(), b"foreign command");
    }
}
