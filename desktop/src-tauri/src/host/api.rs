//! The runtime's host API (SPEC-0104 US7, effort contract 4): the
//! `GET /api/tasks/host` event stream and the drain, stop, pause, and Run now
//! requests. Every request goes to a verified loopback runtime origin.
use serde::Deserialize;
use serde_json::{json, Value};
use std::time::Duration;
use url::Url;

/// The runtime sends status at least every 30 seconds; a stream this quiet
/// is gone.
const QUIET: Duration = Duration::from_secs(75);
const CONNECT: Duration = Duration::from_secs(2);

/// One repository's scheduled-task host status, as the runtime reports it.
#[derive(Clone, Debug, Default, Deserialize, PartialEq)]
#[serde(rename_all = "camelCase", default)]
pub struct HostStatus {
    pub repository: String,
    pub common_dir: String,
    pub run_here: bool,
    pub paused: bool,
    pub paused_until: Option<String>,
    pub draining: bool,
    pub running: Option<Running>,
    /// A pass the runtime started that has not recorded a run yet.
    pub dispatching: Option<Dispatching>,
    pub next: Option<Next>,
    pub attention: u32,
    pub last_failure: Option<LastFailure>,
    pub notices: Vec<Notice>,
}

#[derive(Clone, Debug, Default, Deserialize, PartialEq)]
#[serde(rename_all = "camelCase", default)]
pub struct Dispatching {
    pub pid: u32,
    pub started_at: String,
}

impl HostStatus {
    /// The run going on now, counting a pass that has started but not yet
    /// recorded its first task, so quitting never leaves it behind.
    pub fn active(&self) -> Option<Running> {
        self.running.clone().or_else(|| {
            self.dispatching.as_ref().map(|d| Running {
                name: Some("Scheduled tasks".into()),
                started_at: d.started_at.clone(),
                ..Running::default()
            })
        })
    }
}

#[derive(Clone, Debug, Default, Deserialize, PartialEq)]
#[serde(rename_all = "camelCase", default)]
pub struct Running {
    pub task: String,
    pub name: Option<String>,
    pub run_id: String,
    pub started_at: String,
    pub stage: Option<String>,
}

#[derive(Clone, Debug, Default, Deserialize, PartialEq)]
#[serde(default)]
pub struct Next {
    pub task: String,
    pub name: Option<String>,
    pub at: String,
}

#[derive(Clone, Debug, Default, Deserialize, PartialEq)]
#[serde(default)]
pub struct LastFailure {
    pub task: String,
    pub name: Option<String>,
    pub at: String,
    pub message: String,
}

/// Something the person should hear about once. The id is stable.
#[derive(Clone, Debug, Default, Deserialize, PartialEq)]
#[serde(default)]
pub struct Notice {
    pub id: String,
    pub kind: String,
    pub task: String,
    pub title: String,
    pub body: String,
}

/// What the host stream reports.
#[derive(Debug, PartialEq)]
pub enum Update {
    Status(Box<HostStatus>),
    /// The runtime could not read the status; the message is for the menu.
    Error(String),
}

fn cannot_read(message: &str) -> String {
    format!("Can't read task status: {message}")
}

/// A task's display name, else its id.
pub fn label<'a>(name: &'a Option<String>, task: &'a str) -> &'a str {
    name.as_deref().filter(|n| !n.is_empty()).unwrap_or(task)
}

/// Splits a `text/event-stream` body into events, across chunk boundaries.
#[derive(Default)]
pub struct Events {
    pending: Vec<u8>,
    event: String,
    data: Vec<String>,
}

impl Events {
    /// Takes the next chunk and returns each completed `(event, data)`.
    pub fn push(&mut self, chunk: &[u8]) -> Vec<(String, String)> {
        self.pending.extend_from_slice(chunk);
        let mut done = vec![];
        while let Some(end) = self.pending.iter().position(|b| *b == b'\n') {
            let line: Vec<u8> = self.pending.drain(..=end).collect();
            let line = String::from_utf8_lossy(&line);
            let line = line.trim_end_matches(['\n', '\r']);
            if line.is_empty() {
                if !self.data.is_empty() {
                    let event = match std::mem::take(&mut self.event) {
                        event if event.is_empty() => "message".to_string(),
                        event => event,
                    };
                    done.push((event, std::mem::take(&mut self.data).join("\n")));
                }
                self.event.clear();
                continue;
            }
            let (field, value) = line.split_once(':').unwrap_or((line, ""));
            let value = value.strip_prefix(' ').unwrap_or(value);
            match field {
                "event" => self.event = value.to_string(),
                "data" => self.data.push(value.to_string()),
                _ => {}
            }
        }
        done
    }
}

/// A loopback client for one runtime origin.
#[derive(Clone)]
pub struct Api {
    client: reqwest::Client,
    origin: Url,
}

impl Api {
    pub fn new(origin: Url) -> Result<Self, String> {
        // A client needs a TLS provider even for loopback HTTP.
        if rustls::crypto::CryptoProvider::get_default().is_none() {
            let _ = rustls::crypto::ring::default_provider().install_default();
        }
        let client = reqwest::Client::builder()
            .no_proxy()
            .connect_timeout(CONNECT)
            .build()
            .map_err(|e| e.to_string())?;
        Ok(Self { client, origin })
    }

    fn url(&self, segments: &[&str]) -> Url {
        let mut url = self.origin.clone();
        url.path_segments_mut()
            .expect("runtime origins are http URLs")
            .clear()
            .extend(segments);
        url
    }

    /// Holds the host stream, handing each status or error event to `on`,
    /// until the runtime closes it, fails, or sends no status for
    /// [`QUIET`]. The runtime resends an unchanged error every 25 seconds, so
    /// a status that keeps failing ends the stream too. The result says why.
    pub async fn stream(&self, on: impl FnMut(Update)) -> String {
        self.stream_within(QUIET, on).await
    }

    async fn stream_within(&self, quiet: Duration, mut on: impl FnMut(Update)) -> String {
        let request = self
            .client
            .get(self.url(&["api", "tasks", "host"]))
            .header("Accept", "text/event-stream");
        let mut response = match request.send().await {
            Ok(response) if response.status() == reqwest::StatusCode::NOT_FOUND => {
                return "This Rhizome version cannot host scheduled tasks. Update Rhizome.".into()
            }
            Ok(response) if !response.status().is_success() => {
                return format!("Rhizome refused the task host ({}).", response.status())
            }
            Ok(response) => response,
            Err(error) => return format!("Cannot reach Rhizome: {error}"),
        };
        let mut events = Events::default();
        let mut status_by = tokio::time::Instant::now() + quiet;
        let mut failing: Option<String> = None;
        loop {
            let chunk = match tokio::time::timeout_at(status_by, response.chunk()).await {
                Err(_) => {
                    return match failing {
                        Some(message) => cannot_read(&message),
                        None => "Rhizome stopped reporting task status.".into(),
                    }
                }
                Ok(Err(error)) => return format!("Lost the connection to Rhizome: {error}"),
                Ok(Ok(None)) => return "Rhizome closed the task host.".into(),
                Ok(Ok(Some(chunk))) => chunk,
            };
            for (event, data) in events.push(&chunk) {
                match event.as_str() {
                    "status" => {
                        if let Ok(status) = serde_json::from_str::<HostStatus>(&data) {
                            status_by = tokio::time::Instant::now() + quiet;
                            failing = None;
                            on(Update::Status(Box::new(status)));
                        }
                    }
                    "error" => {
                        let value: Value = serde_json::from_str(&data).unwrap_or(Value::Null);
                        let message = value["error"].as_str().unwrap_or(data.trim()).to_string();
                        on(Update::Error(cannot_read(&message)));
                        failing = Some(message);
                    }
                    _ => {}
                }
            }
        }
    }

    async fn post(&self, segments: &[&str], body: Value, wait: Duration) -> Result<Value, String> {
        let response = self
            .client
            .post(self.url(segments))
            .header("Content-Type", "application/json")
            .body(body.to_string())
            .timeout(wait)
            .send()
            .await
            .map_err(|e| format!("Cannot reach Rhizome: {e}"))?;
        let status = response.status();
        let text = response.text().await.map_err(|e| e.to_string())?;
        let value: Value = serde_json::from_str(&text).unwrap_or(Value::Null);
        if !status.is_success() {
            let message = value["error"].as_str().unwrap_or(text.trim());
            return Err(format!("Rhizome refused ({status}): {message}"));
        }
        Ok(value)
    }

    /// Stops new dispatch until the runtime restarts; returns the run still
    /// going, if any.
    pub async fn drain(&self, wait: Duration) -> Result<Option<Running>, String> {
        let value = self
            .post(&["api", "tasks", "host", "drain"], json!({}), wait)
            .await?;
        let status = HostStatus {
            running: serde_json::from_value(value["running"].clone()).unwrap_or(None),
            dispatching: serde_json::from_value(value["dispatching"].clone()).unwrap_or(None),
            ..HostStatus::default()
        };
        Ok(status.active())
    }

    /// Stops the active run, recorded as stopped.
    pub async fn stop_run(&self, wait: Duration) -> Result<bool, String> {
        let value = self
            .post(&["api", "tasks", "stop"], json!({}), wait)
            .await?;
        Ok(value["stopped"].as_bool().unwrap_or(false))
    }

    pub async fn pause(&self, until: &str, wait: Duration) -> Result<(), String> {
        self.post(&["api", "tasks", "pause"], json!({ "until": until }), wait)
            .await
            .map(|_| ())
    }

    pub async fn resume(&self, wait: Duration) -> Result<(), String> {
        self.post(&["api", "tasks", "resume"], json!({}), wait)
            .await
            .map(|_| ())
    }

    pub async fn run_now(&self, task: &str, wait: Duration) -> Result<(), String> {
        self.post(&["api", "tasks", "tasks", task, "run"], json!({}), wait)
            .await
            .map(|_| ())
    }
}

#[cfg(test)]
#[path = "api_tests.rs"]
mod tests;
