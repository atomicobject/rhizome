use super::{change_prepared_registration, prepare_metadata};
use std::ffi::OsString;

const ARGUMENT: &str = "--install-desktop-integration";

pub fn requested(args: &[OsString]) -> bool {
    args.iter().any(|arg| arg == ARGUMENT)
}

/// Only the installer selects an installation without opening a desktop window.
pub fn run(identifier: &str, version: &str, args: &[OsString]) -> Result<(), String> {
    if !cfg!(windows) {
        return Err("Use the Windows installer to register desktop integration.".into());
    }
    validate(args)?;
    let prepared = prepare_metadata(identifier, version)?;
    change_prepared_registration(&prepared, true)
}

fn validate(args: &[OsString]) -> Result<(), String> {
    if args.len() != 1 || args[0] != ARGUMENT {
        return Err("Use the Windows installer to register desktop integration.".into());
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn only_the_exact_install_action_is_accepted() {
        assert!(validate(&[ARGUMENT.into()]).is_ok());
        for arguments in [
            vec![],
            vec!["--open".into()],
            vec![ARGUMENT.into(), "--open".into(), "repository".into()],
            vec![ARGUMENT.into(), ARGUMENT.into()],
            vec![ARGUMENT.into(), "--remove-desktop-integration".into()],
        ] {
            assert!(validate(&arguments).is_err(), "{arguments:?}");
        }
    }

    #[cfg(windows)]
    mod registration {
        use super::*;
        use crate::installation::{prepare_image, read_registration, write_registration};
        use std::{fs, path::PathBuf};

        fn fixture() -> (tempfile::TempDir, PathBuf, PathBuf, PathBuf) {
            let directory = tempfile::tempdir().unwrap();
            let root = directory.path().join("com.atomicobject.rhizome");
            let app = directory.path().join("Rhizome.exe");
            let source = directory.path().join("rzm.exe");
            fs::write(&app, "GUI").unwrap();
            fs::write(&source, "version one").unwrap();
            (directory, root, app, source)
        }

        #[test]
        fn a_fresh_install_registers_without_installing_a_cli_entry() {
            let (_directory, root, app, source) = fixture();
            let prepared = prepare_image(
                root.clone(),
                app.clone(),
                "com.atomicobject.rhizome".into(),
                "1.0.0".into(),
                &source,
                None,
            )
            .unwrap();
            change_prepared_registration(&prepared, true).unwrap();
            let record = read_registration(&root).unwrap().unwrap();
            assert!(record.enabled);
            assert_eq!(record.app_path, app);
            assert_eq!(record.version, "1.0.0");
            assert!(!root.join("bin").exists());
        }

        #[test]
        fn reinstall_explicitly_reenables_a_disabled_older_registration() {
            let (_directory, root, app, source) = fixture();
            prepare_image(
                root.clone(),
                app.clone(),
                "com.atomicobject.rhizome".into(),
                "1.0.0".into(),
                &source,
                None,
            )
            .unwrap();
            let mut disabled = read_registration(&root).unwrap().unwrap();
            disabled.enabled = false;
            disabled.generation += 1;
            write_registration(&root, &disabled).unwrap();
            fs::write(&source, "version two").unwrap();
            let prepared = prepare_image(
                root.clone(),
                app,
                "com.atomicobject.rhizome".into(),
                "1.1.0".into(),
                &source,
                None,
            )
            .unwrap();
            assert!(!read_registration(&root).unwrap().unwrap().enabled);
            change_prepared_registration(&prepared, true).unwrap();
            let record = read_registration(&root).unwrap().unwrap();
            assert!(record.enabled);
            assert_eq!(record.version, "1.1.0");
            assert_eq!(record.generation, disabled.generation + 1);
            assert_eq!(fs::read(&prepared.image).unwrap(), b"version two");
        }

        #[test]
        fn explicit_installation_selection_preserves_a_later_registration() {
            let (directory, root, app, source) = fixture();
            prepare_image(
                root.clone(),
                app,
                "com.atomicobject.rhizome".into(),
                "1.0.0".into(),
                &source,
                None,
            )
            .unwrap();
            let replacement = directory.path().join("new-install/Rhizome.exe");
            fs::create_dir_all(replacement.parent().unwrap()).unwrap();
            fs::write(&replacement, "new GUI").unwrap();
            let prepared = prepare_image(
                root.clone(),
                replacement.clone(),
                "com.atomicobject.rhizome".into(),
                "1.0.0".into(),
                &source,
                None,
            )
            .unwrap();
            assert_ne!(
                read_registration(&root).unwrap().unwrap().app_path,
                replacement
            );
            change_prepared_registration(&prepared, true).unwrap();
            assert_eq!(
                read_registration(&root).unwrap().unwrap().app_path,
                replacement
            );
            let mut later = read_registration(&root).unwrap().unwrap();
            later.generation += 1;
            write_registration(&root, &later).unwrap();
            assert!(change_prepared_registration(&prepared, true)
                .unwrap_err()
                .contains("changed"));
            assert_eq!(
                read_registration(&root).unwrap().unwrap().generation,
                later.generation
            );
        }
    }
}
