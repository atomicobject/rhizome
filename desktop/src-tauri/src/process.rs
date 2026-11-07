use std::ffi::OsStr;
use tokio::process::Command;

/// Hidden companions must not create a console when launched by the GUI.
pub fn command(program: impl AsRef<OsStr>) -> Command {
    let command = Command::new(program);
    #[cfg(windows)]
    let command = {
        let mut command = command;
        command.creation_flags(0x08000000); // CREATE_NO_WINDOW
        command
    };
    command
}

#[cfg(all(test, windows))]
mod tests {
    use super::*;

    #[test]
    fn hidden_child_has_no_console() {
        tauri::async_runtime::block_on(async {
            let output = command(std::env::current_exe().unwrap())
                .args(["--exact", "process::tests::report_console", "--nocapture"])
                .env("RHIZOME_TEST_HIDDEN_PROCESS", "1")
                .output()
                .await
                .unwrap();
            assert!(output.status.success(), "{output:?}");
            assert!(String::from_utf8_lossy(&output.stdout).contains("no-console"));
        });
    }

    #[test]
    fn report_console() {
        if std::env::var_os("RHIZOME_TEST_HIDDEN_PROCESS").is_none() {
            return;
        }
        #[link(name = "kernel32")]
        extern "system" {
            #[link_name = "GetConsoleWindow"]
            fn get_console_window() -> *mut std::ffi::c_void;
        }
        assert!(unsafe { get_console_window() }.is_null());
        println!("no-console");
    }
}
