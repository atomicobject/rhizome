use super::*;

#[test]
fn task_numbers_cannot_supply_terminal_commands() {
    for item in ["", "1;quit", "1 2", "-1", "1\n", "1234567890", "１"] {
        assert!(item_number(item).is_err(), "{item:?}");
    }
    assert!(item_number("12").is_ok());
}

#[cfg(target_os = "linux")]
#[test]
fn gnome_terminal_receives_paths_as_individual_arguments() {
    let main = Path::new("/work/Drew's; café");
    let rzm = Path::new("/apps/Rhizome's; 开发/rzm");
    let command = start_command(main, rzm, "12").unwrap();
    assert_eq!(command.as_std().get_program(), "gnome-terminal");
    assert_eq!(
        command.as_std().get_args().collect::<Vec<_>>(),
        [
            "--working-directory",
            "/work/Drew's; café",
            "--",
            "/apps/Rhizome's; 开发/rzm",
            "tasks",
            "start",
            "12"
        ]
    );
}

#[cfg(windows)]
#[test]
fn powershell_runs_the_exact_task_executable_and_directory() {
    tauri::async_runtime::block_on(async {
        let dir = tempfile::tempdir().unwrap();
        let main = dir.path().join("Drew's; café’s");
        std::fs::create_dir(&main).unwrap();
        let source = dir.path().join("recorder.rs");
        std::fs::write(
            &source,
            r#"fn main() {
                let mut recorded = format!("cwd={}\n", std::env::current_dir().unwrap().display());
                for arg in std::env::args().skip(1) { recorded.push_str(&format!("arg={arg}\n")); }
                std::fs::write(std::env::var_os("RHIZOME_TASK_TEST_OUTPUT").unwrap(), recorded).unwrap();
            }"#,
        )
        .unwrap();
        let rzm = main.join("Rhizome's; 开发.exe");
        let compiled = crate::process::command("rustc")
            .arg(&source)
            .arg("-o")
            .arg(&rzm)
            .output()
            .await
            .unwrap();
        assert!(compiled.status.success(), "{compiled:?}");
        let terminal = start_command(
            &main.canonicalize().unwrap(),
            &rzm.canonicalize().unwrap(),
            "12",
        )
        .unwrap();
        let recorded = dir.path().join("recorded.txt");
        let output = crate::process::command("powershell.exe")
            .args(["-NoLogo", "-NoProfile", "-Command", POWERSHELL_START])
            .envs(
                terminal
                    .as_std()
                    .get_envs()
                    .map(|(key, value)| (key, value.unwrap())),
            )
            .env("RHIZOME_TASK_TEST_OUTPUT", &recorded)
            .output()
            .await
            .unwrap();
        assert!(output.status.success(), "{output:?}");
        let recorded = std::fs::read_to_string(recorded).unwrap();
        let mut lines = recorded.lines();
        let cwd = lines.next().unwrap().strip_prefix("cwd=").unwrap();
        assert_eq!(
            std::fs::canonicalize(cwd).unwrap(),
            std::fs::canonicalize(&main).unwrap()
        );
        assert_eq!(
            lines.collect::<Vec<_>>(),
            ["arg=tasks", "arg=start", "arg=12"]
        );
    });
}

#[test]
fn selected_checkout_executable_matches_its_canonical_path() {
    let directory = tempfile::tempdir().unwrap();
    let checkout = directory.path().join("Drew's; 开发");
    std::fs::create_dir(&checkout).unwrap();
    let repository = crate::state::Repository {
        executables: [(checkout.to_str().unwrap().to_owned(), "selected-rzm".into())].into(),
        ..Default::default()
    };
    assert_eq!(
        selected_executable(&repository, &checkout.canonicalize().unwrap()),
        Some("selected-rzm")
    );
    assert_eq!(
        selected_executable(&repository, &directory.path().canonicalize().unwrap()),
        None
    );
}
