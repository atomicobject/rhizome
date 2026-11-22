//! Launch at login (SPEC-0104 US8). A per-user LaunchAgent starts the app
//! with `--login`, so it opens no window and keeps the saved session for the
//! next Open. A LaunchAgent works the same for the signed release and for
//! development builds, and it can pass that argument; `SMAppService.mainApp`
//! cannot, so a login launch would look like the person's own.
use std::path::{Path, PathBuf};

pub const ARGUMENT: &str = "--login";

pub fn requested(args: &[String]) -> bool {
    args.iter().any(|arg| arg == ARGUMENT)
}

/// Registers or removes the login item for this app's executable.
pub fn set(identifier: &str, enabled: bool) -> Result<(), String> {
    let home = std::env::var_os("HOME").ok_or("Cannot find your home folder.")?;
    let agents = PathBuf::from(home).join("Library/LaunchAgents");
    let executable = std::env::current_exe().map_err(|e| e.to_string())?;
    set_in(&agents, identifier, enabled, &executable)
}

fn set_in(agents: &Path, identifier: &str, enabled: bool, executable: &Path) -> Result<(), String> {
    let label = format!("{identifier}.login");
    let path = agents.join(format!("{label}.plist"));
    if !enabled {
        return match std::fs::remove_file(&path) {
            Err(error) if error.kind() != std::io::ErrorKind::NotFound => {
                Err(format!("Cannot remove the login item: {error}"))
            }
            _ => Ok(()),
        };
    }
    let text = |s: &str| plist::Value::String(s.into());
    let mut agent = plist::Dictionary::new();
    agent.insert("Label".into(), text(&label));
    agent.insert(
        "ProgramArguments".into(),
        plist::Value::Array(vec![text(&executable.to_string_lossy()), text(ARGUMENT)]),
    );
    agent.insert("RunAtLoad".into(), plist::Value::Boolean(true));
    agent.insert("LimitLoadToSessionType".into(), text("Aqua"));
    agent.insert("ProcessType".into(), text("Interactive"));
    agent.insert(
        "AssociatedBundleIdentifiers".into(),
        plist::Value::Array(vec![text(identifier)]),
    );
    std::fs::create_dir_all(agents).map_err(|e| format!("Cannot add the login item: {e}"))?;
    plist::Value::Dictionary(agent)
        .to_file_xml(&path)
        .map_err(|e| format!("Cannot add the login item: {e}"))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_login_item_starts_this_executable_menu_bar_only_and_can_be_removed() {
        let agents = tempfile::tempdir().unwrap();
        let executable = Path::new("/Applications/Rhizome.app/Contents/MacOS/rhizome-desktop");
        set_in(agents.path(), "com.example.app", true, executable).unwrap();
        let path = agents.path().join("com.example.app.login.plist");
        let agent = plist::Value::from_file(&path).unwrap();
        let agent = agent.as_dictionary().unwrap();
        assert_eq!(
            agent["ProgramArguments"],
            plist::Value::Array(vec![
                plist::Value::String(executable.to_string_lossy().into()),
                plist::Value::String("--login".into()),
            ])
        );
        assert_eq!(agent["RunAtLoad"], plist::Value::Boolean(true));
        assert!(requested(&["app".into(), ARGUMENT.into()]));
        assert!(!requested(&["app".into(), "--open".into()]));
        set_in(agents.path(), "com.example.app", false, executable).unwrap();
        assert!(!path.exists());
        set_in(agents.path(), "com.example.app", false, executable).unwrap();
    }
}
