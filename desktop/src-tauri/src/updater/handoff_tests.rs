use super::*;
use std::process::{Command, Output};

const PROBE: &str = "RHIZOME_UPDATER_HANDOFF_TEST";
const TEST: &str = "updater::handoff_tests::installer_handoff_probe";

fn probe(mode: &str) -> Command {
    let mut command = Command::new(std::env::current_exe().unwrap());
    command
        .args(["--exact", TEST, "--nocapture"])
        .env(PROBE, mode);
    command
}

fn assert_probe(output: Output, marker: &str) {
    assert!(
        output.status.success(),
        "{}{}",
        String::from_utf8_lossy(&output.stdout),
        String::from_utf8_lossy(&output.stderr)
    );
    assert!(String::from_utf8_lossy(&output.stdout).contains(marker));
}

#[test]
fn installer_handoff_marks_the_restart_and_restores_errors() {
    assert_probe(
        probe("handoff").env_remove(RESTARTED).output().unwrap(),
        "HANDOFF_COMPLETE",
    );
}

#[test]
fn installer_handoff_probe() {
    match std::env::var(PROBE).ok().as_deref() {
        Some("handoff") => {
            assert!(std::env::var_os(RESTARTED).is_none());
            let failure = install_handoff(|| {
                assert_eq!(std::env::var(RESTARTED).unwrap(), "1");
                Err("synthetic installer failure")
            });
            assert_eq!(failure, Err("synthetic installer failure"));
            assert!(std::env::var_os(RESTARTED).is_none());

            std::env::set_var(RESTARTED, "previous marker");
            assert_eq!(
                install_handoff(|| Err("synthetic installer failure")),
                Err("synthetic installer failure")
            );
            assert_eq!(std::env::var(RESTARTED).unwrap(), "previous marker");
            std::env::remove_var(RESTARTED);

            install_handoff(|| {
                assert_probe(probe("restarted").output().unwrap(), "RESTART_CONSUMED");
                Ok::<(), &str>(())
            })
            .unwrap();
            assert_eq!(std::env::var(RESTARTED).unwrap(), "1");
            assert!(take_restart_marker());
            assert!(!take_restart_marker());
            println!("HANDOFF_COMPLETE");
        }
        Some("restarted") => {
            let restarted = take_restart_marker();
            assert!(
                restarted,
                "the installer handoff must already carry the marker"
            );
            let arguments = vec!["Rhizome".into(), "--install-update".into()];
            assert!(!requested(&arguments, restarted));
            assert!(!take_restart_marker());
            assert!(requested(&arguments, false));
            println!("RESTART_CONSUMED");
        }
        _ => {}
    }
}
