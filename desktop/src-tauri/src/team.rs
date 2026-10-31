//! Team account sign-in through the bundled `rzm auth` (SPEC-0129 US5).
use crate::bridge::Failure;
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use std::{
    future::Future, path::Path, pin::Pin, process::Stdio, sync::Mutex, task::Poll, time::Duration,
};
use tokio::{
    io::{AsyncBufReadExt, BufReader},
    process::{Child, Command},
    sync::oneshot,
};

/// How long sign-in waits for the browser before giving up.
pub const SIGN_IN_TIMEOUT: Duration = Duration::from_secs(300);
/// Status and sign-out read or clear the local record and revoke one session.
pub const QUICK_TIMEOUT: Duration = Duration::from_secs(30);
/// The team session replaces these, so `rzm auth` never receives them.
const PROVIDER_KEYS: [&str; 6] = [
    "VOYAGE_API_KEY",
    "TYPESAFE_API_KEY",
    "OPENAI_API_KEY",
    "CEREBRAS_API_KEY",
    "ANTHROPIC_API_KEY",
    "ATOMIC_RHIZOME_KEY",
];

#[derive(Deserialize, Serialize)]
struct Account {
    email: String,
    origin: String,
    session_expires_at: String,
}

#[derive(Deserialize)]
#[serde(tag = "event", rename_all = "snake_case")]
enum Event {
    AuthorizeUrl { url: String },
    SignedIn(Account),
    Error { code: String, message: String },
}

#[derive(Deserialize, Serialize)]
struct Status {
    signed_in: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    email: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    origin: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    session_expires_at: Option<String>,
}

#[derive(Deserialize, Serialize)]
struct SignedOut {
    signed_out: bool,
    remote_revoked: bool,
}

fn failure(code: &str, message: &str) -> Failure {
    Failure {
        code: code.into(),
        message: message.into(),
    }
}

fn auth(sidecar: &Path, directory: &Path, subcommand: &str) -> Command {
    let mut command = Command::new(sidecar);
    command
        .args(["auth", subcommand, "--json"])
        .current_dir(directory)
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .kill_on_drop(true);
    for key in PROVIDER_KEYS {
        command.env_remove(key);
    }
    command
}

/// The error event on any line, else the last line that parses as `T`.
fn parse<T: for<'a> Deserialize<'a>>(stdout: &[u8]) -> Result<Option<T>, Failure> {
    let mut found = None;
    for line in String::from_utf8_lossy(stdout).lines() {
        if let Ok(Event::Error { code, message }) = serde_json::from_str(line) {
            return Err(Failure { code, message });
        }
        if let Ok(value) = serde_json::from_str(line) {
            found = Some(value);
        }
    }
    Ok(found)
}

async fn once<T: for<'a> Deserialize<'a> + Serialize>(
    sidecar: &Path,
    directory: &Path,
    subcommand: &str,
    timeout: Duration,
) -> Result<Value, Failure> {
    let output = tokio::time::timeout(timeout, auth(sidecar, directory, subcommand).output())
        .await
        .map_err(|_| {
            failure(
                "team_timeout",
                "Rhizome took too long to respond. Try again.",
            )
        })?
        .map_err(|e| format!("Cannot start the Rhizome companion: {e}"))?;
    match parse::<T>(&output.stdout)? {
        Some(value) if output.status.success() => Ok(json!(value)),
        _ => Err(failure(
            "team_unavailable",
            "This Rhizome cannot report the team account. Update Rhizome and try again.",
        )),
    }
}

/// Reads login events until a final one, forwarding the browser address.
async fn read_login(
    child: &mut Child,
    progress: &(dyn Fn(Value) + Sync),
) -> Result<Value, Failure> {
    let stdout = child
        .stdout
        .take()
        .ok_or("Cannot contact the Rhizome companion.".to_string())?;
    let mut lines = BufReader::new(stdout).lines();
    let mut outcome = None;
    // Lines that are not known events are skipped, so later fields or events are additive.
    while let Ok(Some(line)) = lines.next_line().await {
        match serde_json::from_str(&line) {
            Ok(Event::AuthorizeUrl { url })
                if url.starts_with("https://") || url.starts_with("http://") =>
            {
                progress(json!({"event": "authorize_url", "url": url}))
            }
            Ok(Event::SignedIn(account)) => {
                outcome = Some(Ok(account));
                break;
            }
            Ok(Event::Error { code, message }) => {
                outcome = Some(Err(Failure { code, message }));
                break;
            }
            _ => {}
        }
    }
    let status = child.wait().await.map_err(|e| e.to_string())?;
    match outcome {
        Some(Err(failure)) => Err(failure),
        Some(Ok(account)) if status.success() => {
            let mut value = json!(account);
            value["signed_in"] = true.into();
            Ok(value)
        }
        _ => Err(failure(
            "team_sign_in_failed",
            "Sign-in ended without confirming your account. Try again.",
        )),
    }
}

/// Runs `rzm auth login --json` until it reports, `cancel` resolves, or `timeout`
/// passes. Cancelling and timing out kill the child.
async fn login(
    sidecar: &Path,
    directory: &Path,
    timeout: Duration,
    mut cancel: oneshot::Receiver<()>,
    progress: &(dyn Fn(Value) + Sync),
) -> Result<Value, Failure> {
    let mut child = auth(sidecar, directory, "login")
        .spawn()
        .map_err(|e| format!("Cannot start the Rhizome companion: {e}"))?;
    let finished = {
        let work = read_login(&mut child, progress);
        tokio::pin!(work);
        // ponytail: hand-rolled two-way race; tokio's select! needs the macros feature.
        let race = std::future::poll_fn(|cx| {
            if Pin::new(&mut cancel).poll(cx).is_ready() {
                return Poll::Ready(None);
            }
            work.as_mut().poll(cx).map(Some)
        });
        tokio::time::timeout(timeout, race).await
    };
    let stopped = match finished {
        Ok(Some(result)) => return result,
        Ok(None) => failure("canceled", "Sign-in canceled."),
        Err(_) => failure(
            "team_timeout",
            "Sign-in timed out waiting for the browser. Try again.",
        ),
    };
    let _ = child.kill().await;
    Err(stopped)
}

struct Active {
    window: String,
    cancel: Option<oneshot::Sender<()>>,
}

/// The one sign-in that may run at a time.
#[derive(Default)]
pub struct Team(Mutex<Option<Active>>);

struct Running<'a>(&'a Team);
impl Drop for Running<'_> {
    fn drop(&mut self) {
        *self.0 .0.lock().unwrap() = None;
    }
}

impl Team {
    /// Cancels the running sign-in when `window` started it, or any when None.
    pub fn cancel(&self, window: Option<&str>) {
        if let Some(active) = self.0.lock().unwrap().as_mut() {
            if window.map_or(true, |window| window == active.window) {
                active.cancel.take();
            }
        }
    }

    pub async fn sign_in(
        &self,
        directory: &Path,
        window: &str,
        progress: &(dyn Fn(Value) + Sync),
    ) -> Result<Value, Failure> {
        let sidecar = crate::bridge::sidecar()?;
        self.sign_in_with(&sidecar, directory, window, SIGN_IN_TIMEOUT, progress)
            .await
    }

    async fn sign_in_with(
        &self,
        sidecar: &Path,
        directory: &Path,
        window: &str,
        timeout: Duration,
        progress: &(dyn Fn(Value) + Sync),
    ) -> Result<Value, Failure> {
        let (sender, receiver) = oneshot::channel();
        {
            let mut active = self.0.lock().unwrap();
            if active.is_some() {
                return Err(failure(
                    "team_busy",
                    "Sign-in is already running. Finish it in the browser or cancel it.",
                ));
            }
            *active = Some(Active {
                window: window.into(),
                cancel: Some(sender),
            });
        }
        let _running = Running(self);
        login(sidecar, directory, timeout, receiver, progress).await
    }
}

pub async fn status(directory: &Path) -> Result<Value, Failure> {
    once::<Status>(
        &crate::bridge::sidecar()?,
        directory,
        "status",
        QUICK_TIMEOUT,
    )
    .await
}

pub async fn sign_out(directory: &Path) -> Result<Value, Failure> {
    once::<SignedOut>(
        &crate::bridge::sidecar()?,
        directory,
        "logout",
        QUICK_TIMEOUT,
    )
    .await
}

#[cfg(all(test, unix))]
mod tests {
    use super::*;
    use std::{os::unix::fs::PermissionsExt, path::PathBuf, sync::Arc};

    fn sidecar(directory: &Path, body: &str) -> PathBuf {
        let path = directory.join("rzm");
        std::fs::write(&path, format!("#!/bin/sh\n{body}\n")).unwrap();
        std::fs::set_permissions(&path, std::fs::Permissions::from_mode(0o700)).unwrap();
        path
    }

    fn run<T>(future: impl Future<Output = T>) -> T {
        tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()
            .unwrap()
            .block_on(future)
    }

    fn sign_in(body: &str, timeout: Duration) -> (Result<Value, Failure>, Vec<Value>) {
        let directory = tempfile::tempdir().unwrap();
        let rzm = sidecar(directory.path(), body);
        let seen = Mutex::new(vec![]);
        let result = run(Team::default().sign_in_with(
            &rzm,
            directory.path(),
            "shell-1",
            timeout,
            &|value| seen.lock().unwrap().push(value),
        ));
        (result, seen.into_inner().unwrap())
    }

    #[test]
    fn sign_in_streams_the_browser_address_and_returns_the_account() {
        let (result, seen) = sign_in(
            r#"[ "$*" = "auth login --json" ] || exit 9
[ -z "$VOYAGE_API_KEY" ] || exit 8
echo 'not json'
echo '{"event":"authorize_url","url":"https://accounts.example.test/auth"}'
echo '{"event":"later_event"}'
echo '{"event":"signed_in","email":"ada@example.test","origin":"https://team.example.test","session_expires_at":"2026-11-07T12:00:00Z"}'"#,
            Duration::from_secs(10),
        );
        assert_eq!(
            seen,
            vec![json!({"event": "authorize_url", "url": "https://accounts.example.test/auth"})]
        );
        assert_eq!(
            result.unwrap(),
            json!({"signed_in": true, "email": "ada@example.test", "origin": "https://team.example.test", "session_expires_at": "2026-11-07T12:00:00Z"})
        );
    }

    #[test]
    fn sign_in_surfaces_error_events_and_rejects_unconfirmed_exits() {
        let (result, _) = sign_in(
            r#"echo '{"event":"error","code":"not_eligible","message":"Your account is not eligible."}'; exit 1"#,
            Duration::from_secs(10),
        );
        let failure = result.unwrap_err();
        assert_eq!(failure.code, "not_eligible");
        assert_eq!(failure.message, "Your account is not eligible.");
        for body in [
            "echo '{\"event\":\"signed_in\",\"email\":1}'",
            "echo '{\"event\":\"signed_in\",\"email\":\"a@example.test\",\"origin\":\"o\",\"session_expires_at\":\"t\"}'; exit 3",
            "echo garbage; exit 0",
        ] {
            assert_eq!(
                sign_in(body, Duration::from_secs(10)).0.unwrap_err().code,
                "team_sign_in_failed",
                "{body}"
            );
        }
    }

    /// The fake sidecar records its PID and then waits, as login does for the browser.
    const WAITING: &str = "echo $$ > pid\nexec /bin/sleep 30";

    fn alive(directory: &Path) -> bool {
        let pid = std::fs::read_to_string(directory.join("pid")).unwrap();
        std::process::Command::new("/bin/kill")
            .args(["-0", pid.trim()])
            .stderr(Stdio::null())
            .status()
            .unwrap()
            .success()
    }

    #[test]
    fn timeout_kills_the_child() {
        let directory = tempfile::tempdir().unwrap();
        let rzm = sidecar(directory.path(), WAITING);
        let result = run(Team::default().sign_in_with(
            &rzm,
            directory.path(),
            "shell-1",
            Duration::from_millis(300),
            &|_| {},
        ));
        assert_eq!(result.unwrap_err().code, "team_timeout");
        assert!(!alive(directory.path()));
    }

    #[test]
    fn cancel_kills_the_child_and_only_the_owner_window_cancels() {
        let directory = tempfile::tempdir().unwrap();
        let rzm = sidecar(directory.path(), WAITING);
        let team = Arc::new(Team::default());
        let result = run(async {
            let canceller = team.clone();
            let pid = directory.path().join("pid");
            let cancel = async move {
                while !pid.exists() {
                    tokio::time::sleep(Duration::from_millis(10)).await;
                }
                canceller.cancel(Some("shell-2"));
                tokio::time::sleep(Duration::from_millis(100)).await;
                assert!(alive(pid.parent().unwrap()), "another window cannot cancel");
                canceller.cancel(Some("shell-1"));
            };
            let sign_in = team.sign_in_with(
                &rzm,
                directory.path(),
                "shell-1",
                Duration::from_secs(10),
                &|_| {},
            );
            tokio::pin!(sign_in);
            tokio::pin!(cancel);
            // Drive both until sign-in finishes.
            let mut cancelled = false;
            std::future::poll_fn(|cx| {
                if !cancelled {
                    cancelled = cancel.as_mut().poll(cx).is_ready();
                }
                sign_in.as_mut().poll(cx)
            })
            .await
        });
        assert_eq!(result.unwrap_err().code, "canceled");
        assert!(!alive(directory.path()));
        assert!(team.0.lock().unwrap().is_none(), "a new sign-in can start");
    }

    #[test]
    fn a_second_sign_in_is_rejected_while_one_runs() {
        let team = Team::default();
        *team.0.lock().unwrap() = Some(Active {
            window: "shell-1".into(),
            cancel: None,
        });
        let directory = tempfile::tempdir().unwrap();
        let result = run(team.sign_in_with(
            Path::new("/nonexistent"),
            directory.path(),
            "shell-2",
            Duration::from_secs(1),
            &|_| {},
        ));
        assert_eq!(result.unwrap_err().code, "team_busy");
        assert!(team.0.lock().unwrap().is_some(), "the owner keeps its slot");
    }

    #[test]
    fn status_and_sign_out_parse_their_documents() {
        let directory = tempfile::tempdir().unwrap();
        let path = directory.path();
        let check = |body: &str, subcommand: &str| {
            let rzm = sidecar(path, body);
            run(async {
                match subcommand {
                    "status" => once::<Status>(&rzm, path, "status", QUICK_TIMEOUT).await,
                    _ => once::<SignedOut>(&rzm, path, "logout", QUICK_TIMEOUT).await,
                }
            })
        };
        assert_eq!(
            check(r#"[ "$*" = "auth status --json" ] && echo '{"signed_in":false,"origin":"https://team.example.test"}'"#, "status").unwrap(),
            json!({"signed_in": false, "origin": "https://team.example.test"})
        );
        assert_eq!(
            check(r#"[ "$*" = "auth logout --json" ] && echo '{"signed_out":true,"remote_revoked":false}'"#, "logout").unwrap(),
            json!({"signed_out": true, "remote_revoked": false})
        );
        assert_eq!(
            check("echo 'unknown command \"auth\"'; exit 1", "status")
                .unwrap_err()
                .code,
            "team_unavailable"
        );
        assert_eq!(
            check(r#"echo '{"event":"error","code":"keychain_locked","message":"Unlock the keychain."}'; exit 1"#, "logout")
                .unwrap_err()
                .code,
            "keychain_locked"
        );
    }
}
