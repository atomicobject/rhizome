use super::{Desktop, Failure, Request};
use crate::{bridge, installation};
use serde::Deserialize;
use serde_json::Value;
use std::future::Future;

#[derive(Clone, Copy, PartialEq)]
pub(super) enum Action {
    Status,
    Install,
    RemoveCli,
    RemoveDesktop,
}

#[derive(Deserialize)]
#[serde(rename_all = "kebab-case")]
enum LinkState {
    Absent,
    Installed,
    OtherBundle,
    Dangling,
    Foreign,
    Unsupported,
}

#[derive(Deserialize)]
struct LinkStatus {
    state: LinkState,
}

pub(super) async fn run(desktop: &Desktop, action: Action) -> Result<Value, Failure> {
    if action == Action::RemoveDesktop && cfg!(target_os = "macos") {
        return Err(
            "Remove the command-line tool before deleting the macOS app."
                .to_string()
                .into(),
        );
    }
    let _linking = desktop.global.lock().await;
    let generation = if action == Action::RemoveDesktop {
        installation::expected_generation()?
    } else {
        0
    };
    let mut result = change(
        action,
        |operation| {
            bridge::call(
                &desktop.directory,
                Request {
                    operation,
                    expected_generation: if operation == "cli-link-remove" {
                        generation
                    } else {
                        0
                    },
                    ..Request::default()
                },
                bridge::TIMEOUT,
            )
        },
        installation::activate,
        installation::disable,
    )
    .await?;
    if let (Some(fields), Some(error)) = (result.as_object_mut(), installation::integration_error())
    {
        fields.insert("desktopIntegrationError".into(), Value::String(error));
    }
    Ok(result)
}

async fn change<F: Future<Output = Result<Value, Failure>>>(
    action: Action,
    mut call: impl FnMut(&'static str) -> F,
    activate: impl FnOnce() -> Result<(), String>,
    disable: impl FnOnce() -> Result<(), String>,
) -> Result<Value, Failure> {
    match action {
        Action::Status => call("cli-link-status").await,
        Action::Install => {
            activate()?;
            call("cli-link-install").await
        }
        Action::RemoveCli => call("cli-link-remove").await,
        Action::RemoveDesktop => {
            let current = call("cli-link-status").await?;
            let status: LinkStatus = serde_json::from_value(current.clone()).map_err(|_| {
                "Rhizome returned invalid command-line tool information.".to_string()
            })?;
            let (result, summary) = match status.state {
                LinkState::Foreign | LinkState::Unsupported => {
                    (current, "The command-line tool was not changed")
                }
                LinkState::Absent
                | LinkState::Installed
                | LinkState::OtherBundle
                | LinkState::Dangling => (
                    call("cli-link-remove").await?,
                    "The command-line tool is unlinked",
                ),
            };
            disable().map_err(|message| Failure {
                code: "desktop_integration_partial".into(),
                message: format!(
                    "{summary}, but desktop integration could not be disabled: {message}"
                ),
            })?;
            Ok(result)
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    use std::{cell::Cell, cell::RefCell, future::ready};

    #[test]
    fn ordinary_cli_removal_does_not_disable_the_installation() {
        tauri::async_runtime::block_on(async {
            let result = change(
                Action::RemoveCli,
                |operation| {
                    assert_eq!(operation, "cli-link-remove");
                    ready(Ok(json!({"state":"absent"})))
                },
                || panic!("unlink must not activate"),
                || panic!("unlink must not disable"),
            )
            .await
            .unwrap();
            assert_eq!(result["state"], "absent");
        });
    }

    #[test]
    fn installing_the_cli_activates_before_the_bridge_call() {
        tauri::async_runtime::block_on(async {
            let active = Cell::new(false);
            change(
                Action::Install,
                |operation| {
                    assert!(active.get());
                    assert_eq!(operation, "cli-link-install");
                    ready(Ok(json!({"state":"installed"})))
                },
                || {
                    active.set(true);
                    Ok(())
                },
                || panic!("install must not disable"),
            )
            .await
            .unwrap();
        });
    }

    #[test]
    fn full_removal_preserves_a_foreign_entry() {
        tauri::async_runtime::block_on(async {
            let disabled = Cell::new(false);
            let result = change(
                Action::RemoveDesktop,
                |operation| {
                    assert_eq!(
                        operation, "cli-link-status",
                        "foreign entries must not be unlinked"
                    );
                    ready(Ok(json!({"state":"foreign","target":"/foreign/rzm"})))
                },
                || panic!("remove must not activate"),
                || {
                    disabled.set(true);
                    Ok(())
                },
            )
            .await
            .unwrap();
            assert!(disabled.get());
            assert_eq!(result["target"], "/foreign/rzm");
        });
    }

    #[test]
    fn an_unlink_failure_keeps_the_registration_enabled() {
        tauri::async_runtime::block_on(async {
            let error = change(
                Action::RemoveDesktop,
                |operation| {
                    ready(match operation {
                        "cli-link-status" => Ok(json!({"state":"installed"})),
                        "cli-link-remove" => Err(Failure {
                            code: "link_failed".into(),
                            message: "synthetic unlink failure".into(),
                        }),
                        _ => panic!("unexpected bridge call"),
                    })
                },
                || panic!("remove must not activate"),
                || panic!("failed unlink must not disable"),
            )
            .await
            .unwrap_err();
            assert_eq!(error.code, "link_failed");
        });
    }

    #[test]
    fn a_disable_failure_reports_that_the_cli_is_already_unlinked() {
        tauri::async_runtime::block_on(async {
            let calls = RefCell::new(Vec::new());
            let error = change(
                Action::RemoveDesktop,
                |operation| {
                    calls.borrow_mut().push(operation);
                    ready(Ok(json!({"state":if operation == "cli-link-status" {"installed"} else {"absent"}})))
                },
                || panic!("remove must not activate"),
                || {
                    assert_eq!(*calls.borrow(), ["cli-link-status", "cli-link-remove"]);
                    Err("synthetic registration failure".into())
                },
            )
            .await
            .unwrap_err();
            assert_eq!(error.code, "desktop_integration_partial");
            assert!(error.message.contains("command-line tool is unlinked"));
            assert!(error.message.contains("synthetic registration failure"));
        });
    }
}
