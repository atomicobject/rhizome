use crate::setup::{ScopeEdits, Secret, SetupChoices};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::{path::Path, process::Stdio, time::Duration};
use tokio::{io::AsyncWriteExt, process::Command};

#[derive(Clone, Debug, Deserialize, Serialize)]
pub struct Failure {
    pub code: String,
    pub message: String,
}
impl From<String> for Failure {
    fn from(message: String) -> Self {
        Self {
            code: "desktop_error".into(),
            message,
        }
    }
}
#[derive(Deserialize)]
struct Response {
    protocol: u8,
    result: Option<Value>,
    error: Option<Failure>,
}
#[derive(Clone, Copy, Default, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Request<'a> {
    pub operation: &'a str,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub folder: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub executable: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub global_executable: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub primary: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub setup: Option<&'a SetupChoices>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub key: Option<&'a Secret>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub edits: Option<&'a ScopeEdits>,
    /// Replaces a live headless runtime with a fresh one during open.
    #[serde(skip_serializing_if = "std::ops::Not::not")]
    pub restart: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub folders: Option<&'a [String]>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub repositories: Option<&'a [RepositoryRef]>,
}

#[derive(Clone, Debug, Serialize)]
pub struct RepositoryRef {
    pub folder: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub primary: Option<String>,
}

/// Seeding copies and catches up a whole index, so it may take far longer than
/// any other request.
pub const SEED_TIMEOUT: Duration = Duration::from_secs(25 * 60);
pub const TIMEOUT: Duration = Duration::from_secs(300);

pub async fn call(
    directory: &Path,
    request: Request<'_>,
    timeout: Duration,
) -> Result<Value, Failure> {
    let current = std::env::current_exe().map_err(|e| e.to_string())?;
    let sidecar = current
        .parent()
        .ok_or("Cannot locate the desktop companion.".to_string())?
        .join(if cfg!(windows) {
            "rhizome-desktop-bridge.exe"
        } else {
            "rhizome-desktop-bridge"
        });
    let mut child = Command::new(sidecar)
        .arg("--state-dir")
        .arg(directory)
        .current_dir(directory)
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .kill_on_drop(true)
        .spawn()
        .map_err(|e| format!("Cannot start the Rhizome companion: {e}"))?;
    let mut bytes = serde_json::to_value(request).map_err(|e| e.to_string())?;
    bytes["protocol"] = 1.into();
    let bytes = serde_json::to_vec(&bytes).map_err(|e| e.to_string())?;
    let mut stdin = child
        .stdin
        .take()
        .ok_or("Cannot contact the Rhizome companion.".to_string())?;
    stdin.write_all(&bytes).await.map_err(|e| e.to_string())?;
    stdin.shutdown().await.map_err(|e| e.to_string())?;
    drop(stdin);
    let output = tokio::time::timeout(timeout, child.wait_with_output())
        .await
        .map_err(|_| "Rhizome took too long to respond. Try again.".to_string())?
        .map_err(|e| e.to_string())?;
    let response: Response = serde_json::from_slice(&output.stdout)
        .map_err(|_| "The Rhizome companion returned an invalid response.".to_string())?;
    if response.protocol != 1 {
        return Err("The Rhizome companion uses an unsupported protocol."
            .to_string()
            .into());
    }
    if let Some(error) = response.error {
        return Err(error);
    }
    if !output.status.success() {
        return Err(
            "The Rhizome companion exited before completing the request."
                .to_string()
                .into(),
        );
    }
    response.result.ok_or_else(|| {
        "The Rhizome companion returned no result."
            .to_string()
            .into()
    })
}
