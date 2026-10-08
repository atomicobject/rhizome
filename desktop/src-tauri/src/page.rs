//! The workspace page's controls in the shell toolbar (SPEC-0119). The content
//! view has no native permissions, so the page reports its state through its
//! document title, and the app sends it commands as `rhizome:desktop` events.
use crate::{pane::Pane, security};
use serde::{Deserialize, Serialize};
use serde_json::json;
use url::Url;

/// Marks the content view so the web UI hides its header and reports state.
pub const MARKER: &str = "window.__RHIZOME_DESKTOP__ = true;";

const PREFIX: &str = "rhizome-desktop:";
const SEARCH_LIMIT: usize = 500;

#[derive(Clone, Copy, Debug, PartialEq, Deserialize, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum Section {
    Notes,
    Ontology,
    Explorer,
    Agent,
    Graphql,
}

#[derive(Clone, Copy, Debug, PartialEq, Deserialize, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum Health {
    NeverChecked,
    Running,
    CurrentClean,
    CurrentIssues,
    Incomplete,
    Failed,
    Stale,
}

/// The page state the toolbar shows. `issues` and `health` are present on Notes.
#[derive(Debug, PartialEq, Deserialize, Serialize)]
pub struct Report {
    #[serde(skip_serializing)]
    v: u8,
    section: Section,
    search: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    issues: Option<u32>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    health: Option<Health>,
}

/// Reads a page title as a report, only from a page on the verified runtime
/// origin. Any other title, such as an HTML viewer page's, yields `None`.
pub fn decode(title: &str, page: &Url, runtime: Option<&Url>) -> Option<Report> {
    if !security::same_origin(runtime?, page) {
        return None;
    }
    let report: Report = serde_json::from_str(title.strip_prefix(PREFIX)?).ok()?;
    (report.v == 1 && report.search.chars().count() <= SEARCH_LIMIT).then_some(report)
}

/// A new document is loading in the content view; it has reported nothing yet.
/// Tracking the document here keeps the title handler from querying the
/// webview from inside its own callback.
pub fn loading(pane: &mut Pane, url: &Url) {
    pane.document = Some(url.clone());
    pane.reported = false;
    pane.send(json!({"type": "page", "state": null}));
}

/// Relays the current document's title to the shell as its page state.
pub fn titled(pane: &mut Pane, title: &str) {
    let runtime = pane.expected.read().unwrap().clone();
    let report = pane
        .document
        .as_ref()
        .and_then(|document| decode(title, document, runtime.as_ref()));
    pane.reported = report.is_some();
    pane.send(json!({"type": "page", "state": report}));
}

/// Whether a visible content view shows a document on the verified runtime origin.
pub fn on_runtime(pane: &Pane) -> bool {
    let runtime = pane.expected.read().unwrap();
    pane.visible()
        && matches!((runtime.as_ref(), pane.document.as_ref()),
            (Some(runtime), Some(document)) if security::same_origin(runtime, document))
}

/// Hands ⌘K to a page that shows its own header, as a runtime older than this
/// contract does; the menu shortcut would otherwise swallow it.
pub const LEGACY_SEARCH: &str =
    "window.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', metaKey: true }))";

/// A toolbar control's command to the page.
#[derive(Debug, PartialEq, Deserialize, Serialize)]
#[serde(tag = "command", rename_all = "lowercase", deny_unknown_fields)]
pub enum Command {
    Section { section: Section },
    Search { query: String },
    Issues,
    Shortcuts,
}

impl Command {
    /// The script that hands the command to the page, or `None` for a search
    /// longer than a report may carry.
    pub fn script(&self) -> Option<String> {
        if let Command::Search { query } = self {
            if query.chars().count() > SEARCH_LIMIT {
                return None;
            }
        }
        let detail = serde_json::to_string(self).ok()?;
        Some(format!(
            "window.dispatchEvent(new CustomEvent('rhizome:desktop', {{ detail: {detail} }}))"
        ))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn runtime() -> Url {
        "http://127.0.0.1:4100/".parse().unwrap()
    }

    fn page(path: &str) -> Url {
        runtime().join(path).unwrap()
    }

    #[test]
    fn decodes_a_report_from_the_runtime_page() {
        let title = r#"rhizome-desktop:{"v":1,"section":"notes","search":"meetings","issues":37,"health":"current_issues"}"#;
        let report = decode(title, &page("/notes"), Some(&runtime())).unwrap();
        assert_eq!(
            serde_json::to_value(&report).unwrap(),
            json!({"section": "notes", "search": "meetings", "issues": 37, "health": "current_issues"})
        );
        let title =
            r#"rhizome-desktop:{"v":1,"section":"graphql","search":"","issues":null,"later":true}"#;
        let report = decode(title, &page("/graphql"), Some(&runtime())).unwrap();
        assert_eq!(
            serde_json::to_value(&report).unwrap(),
            json!({"section": "graphql", "search": ""})
        );
    }

    #[test]
    fn rejects_reports_it_cannot_trust() {
        let valid = r#"rhizome-desktop:{"v":1,"section":"notes","search":""}"#;
        let viewer: Url = "http://0123456789abcdef0123456789abcdef.localhost:4100/"
            .parse()
            .unwrap();
        assert!(decode(valid, &viewer, Some(&runtime())).is_none());
        assert!(decode(valid, &page("/notes"), None).is_none());
        let long = format!(
            r#"rhizome-desktop:{{"v":1,"section":"notes","search":"{}"}}"#,
            "a".repeat(501)
        );
        for title in [
            "Rhizome · vault",
            "",
            r#"rhizome-desktop:{"v":2,"section":"notes","search":""}"#,
            r#"rhizome-desktop:{"v":1,"section":"settings","search":""}"#,
            r#"rhizome-desktop:{"v":1,"section":"notes","search":"","issues":-1}"#,
            r#"rhizome-desktop:{"v":1,"section":"notes","search":"","health":"great"}"#,
            &long,
        ] {
            assert!(
                decode(title, &page("/notes"), Some(&runtime())).is_none(),
                "{title}"
            );
        }
    }

    /// A pane showing `runtime`'s page whose shell messages land in the receiver.
    fn shown_pane() -> (Pane, std::sync::mpsc::Receiver<serde_json::Value>) {
        let (sent, received) = std::sync::mpsc::channel();
        let mut pane = Pane::default();
        pane.attach(tauri::ipc::Channel::new(move |body| {
            if let tauri::ipc::InvokeResponseBody::Json(text) = body {
                sent.send(serde_json::from_str(&text).unwrap()).unwrap();
            }
            Ok(())
        }));
        let generation = pane.select(1, 1, "r", "/w").unwrap().0;
        pane.expect(generation, &runtime(), 1, true);
        pane.navigation_started(&page("/notes"));
        pane.loaded(&page("/notes"));
        (pane, received)
    }

    #[test]
    fn relays_titles_and_commands_only_for_the_runtime_document() {
        let (mut pane, received) = shown_pane();
        let report = r#"rhizome-desktop:{"v":1,"section":"agent","search":""}"#;
        let viewer: Url = "http://0123456789abcdef0123456789abcdef.localhost:4100/"
            .parse()
            .unwrap();
        let page_states = || {
            received
                .try_iter()
                .filter(|m| m["type"] == "page")
                .map(|m| m["state"].clone())
                .collect::<Vec<_>>()
        };
        page_states();

        loading(&mut pane, &viewer);
        titled(&mut pane, report);
        assert_eq!(page_states(), vec![json!(null), json!(null)]);
        assert!(!on_runtime(&pane));
        assert!(!pane.reported);

        loading(&mut pane, &page("/agent"));
        titled(&mut pane, "Rhizome · vault");
        assert!(!pane.reported, "an older runtime's title is not a report");
        titled(&mut pane, report);
        assert!(pane.reported);
        assert_eq!(
            page_states(),
            vec![
                json!(null),
                json!(null),
                json!({"section": "agent", "search": ""})
            ]
        );
        assert!(on_runtime(&pane));
        pane.cover(true);
        assert!(!on_runtime(&pane));
    }

    #[test]
    fn serializes_commands_into_a_fixed_event() {
        let command: Command =
            serde_json::from_value(json!({"command": "search", "query": "a'b\"</script>"}))
                .unwrap();
        assert_eq!(
            command.script().unwrap(),
            r#"window.dispatchEvent(new CustomEvent('rhizome:desktop', { detail: {"command":"search","query":"a'b\"</script>"} }))"#
        );
        let command: Command =
            serde_json::from_value(json!({"command": "section", "section": "ontology"})).unwrap();
        assert_eq!(
            command,
            Command::Section {
                section: Section::Ontology
            }
        );
        let long = Command::Search {
            query: "a".repeat(501),
        };
        assert!(long.script().is_none());
        for invalid in [
            json!({"command": "section", "section": "settings"}),
            json!({"command": "navigate", "url": "https://example.com"}),
            json!({"command": "search"}),
        ] {
            assert!(serde_json::from_value::<Command>(invalid).is_err());
        }
    }
}
