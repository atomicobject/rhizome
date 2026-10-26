//! The workspace page's controls in the shell toolbar (SPEC-0119). The content
//! view has no native permissions, so the page reports its state through its
//! document title, and the app sends it commands as `rhizome:desktop` events.
use crate::{
    appearance::Theme, commands::Desktop, pane::Pane, security, state::Preferences, updater,
};
use serde::{Deserialize, Serialize};
use serde_json::json;
use tauri::{AppHandle, Manager, Window};
use url::Url;

/// Marks the content view so the web UI hides its header and reports state.
pub const MARKER: &str = "window.__RHIZOME_DESKTOP__ = true;";

/// Turns on the web UI's React render regions (SPEC-0116). They add noticeable
/// render time, so a release build adds this only when the user asks.
pub const RENDER_PROFILING: &str = "window.__RHIZOME_RENDER_PROFILING__ = true;";

/// Whether new content views record render timings. `always` marks Rhizome Dev
/// and local builds, the ones that never update themselves.
#[derive(Debug, PartialEq, Serialize)]
pub struct Profiling {
    pub enabled: bool,
    pub always: bool,
}

pub fn profiling(identifier: &str, version: &str, chosen: bool) -> Profiling {
    let always = !updater::enabled(identifier, version);
    Profiling {
        enabled: always || chosen,
        always,
    }
}

pub fn profiling_for(app: &AppHandle) -> Profiling {
    let chosen = Preferences::load(&app.state::<Desktop>().directory).render_profiling;
    profiling(
        &app.config().identifier,
        &app.package_info().version.to_string(),
        chosen,
    )
}

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
    /// The workbench sidebar's state, once the app has sent the page its choice.
    #[serde(
        default,
        rename = "sidebarCollapsed",
        skip_serializing_if = "Option::is_none"
    )]
    sidebar_collapsed: Option<bool>,
    /// How many times the page itself toggled the sidebar (⌘\). Replies to
    /// the app's commands leave it unchanged.
    #[serde(
        default,
        rename = "sidebarToggles",
        skip_serializing_if = "Option::is_none"
    )]
    sidebar_toggles: Option<u32>,
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
    pane.sidebar = None;
    pane.sidebar_toggles = None;
    pane.graphql = false;
    pane.send(json!({"type": "page", "state": null}));
}

/// Relays the current document's title to the shell as its page state.
/// Returns whether this is the document's first valid report, and the sidebar
/// state when the page itself toggled it since its last report (⌘\ in the
/// page). Only the page's own toggle count marks a toggle: a late reply to an
/// earlier app command must not undo a newer choice.
pub fn titled(pane: &mut Pane, title: &str) -> (bool, Option<bool>) {
    let runtime = pane.expected.read().unwrap().clone();
    let report = pane
        .document
        .as_ref()
        .and_then(|document| decode(title, document, runtime.as_ref()));
    let first = !pane.reported && report.is_some();
    pane.reported = report.is_some();
    let previous_toggles = pane.sidebar_toggles;
    pane.sidebar = report.as_ref().and_then(|r| r.sidebar_collapsed);
    pane.sidebar_toggles = report.as_ref().and_then(|r| r.sidebar_toggles);
    pane.graphql = report
        .as_ref()
        .is_some_and(|r| r.section == Section::Graphql);
    let toggled = match (previous_toggles, pane.sidebar_toggles) {
        (Some(before), Some(now)) if now > before => pane.sidebar,
        _ => None,
    };
    pane.send(json!({"type": "page", "state": report}));
    (first, toggled)
}

/// Whether the shown page can toggle its sidebar: a runtime page that took
/// the app's sidebar command, in a section other than GraphQL.
pub fn toggles_sidebar(pane: &mut Pane) -> bool {
    on_runtime(pane) && pane.sidebar.is_some() && !pane.graphql
}

/// Hands a runtime document an app setting (theme, sidebar). A page that is
/// loading or covered still takes it.
pub fn send_setting(window: &Window, command: &Command) {
    let shown = window
        .app_handle()
        .state::<crate::pane::Panes>()
        .with(window.label(), |pane| on_runtime_document(pane))
        .unwrap_or(false);
    let content = window.get_webview(&crate::windows::content_label(window.label()));
    if let (true, Some(content), Some(script)) = (shown, content, command.script()) {
        let _ = content.eval(script);
    }
}

/// Saves the app-wide sidebar state and hands it to every window's page; the
/// pages' reports then update each toolbar.
pub fn set_sidebar(app: &AppHandle, collapsed: bool) -> Result<(), String> {
    let desktop = app.state::<Desktop>();
    {
        let _saving = desktop.preferences.lock().unwrap();
        let mut saved = Preferences::load(&desktop.directory);
        saved.sidebar_collapsed = collapsed;
        saved.save(&desktop.directory)?;
    }
    for window in app.windows().values() {
        send_setting(window, &Command::Sidebar { collapsed });
    }
    crate::menu::refresh_sidebar(app);
    Ok(())
}

/// Applies a page's report that the user toggled its sidebar, unless the app
/// already holds that state.
pub fn sidebar_reported(app: &AppHandle, collapsed: bool) {
    let saved = Preferences::load(&app.state::<Desktop>().directory).sidebar_collapsed;
    if saved != collapsed {
        let _ = set_sidebar(app, collapsed);
    }
}

/// Whether a visible content view shows a document on the verified runtime origin.
pub fn on_runtime(pane: &Pane) -> bool {
    pane.visible() && on_runtime_document(pane)
}

/// Whether the content view's document is on the verified runtime origin,
/// even while it loads or the shell covers it. Only the theme command, which
/// carries nothing but a preset name, is sent on this looser gate.
pub fn on_runtime_document(pane: &Pane) -> bool {
    let runtime = pane.expected.read().unwrap();
    matches!((runtime.as_ref(), pane.document.as_ref()),
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
    Section {
        section: Section,
    },
    Search {
        query: String,
    },
    Issues,
    Shortcuts,
    /// Opens the page's note and file finder (⌘P).
    Find,
    /// The chrome theme, sent by the app rather than a toolbar control.
    #[serde(skip_deserializing)]
    Theme {
        theme: Theme,
    },
    /// Shows or hides the workbench sidebar. From the shell it asks the app to
    /// store the state and send it to every window's page.
    Sidebar {
        collapsed: bool,
    },
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

    #[test]
    fn render_profiling_is_always_on_in_dev_and_local_builds_and_chosen_in_releases() {
        let release = "com.atomicobject.rhizome";
        let on = |always| Profiling {
            enabled: true,
            always,
        };
        assert_eq!(
            profiling("com.atomicobject.rhizome.dev", "0.52.0", false),
            on(true)
        );
        assert_eq!(profiling(release, "0.0.0", false), on(true));
        assert_eq!(profiling(release, "0.52.0", true), on(false));
        assert_eq!(
            profiling(release, "0.52.0", false),
            Profiling {
                enabled: false,
                always: false
            }
        );
    }

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
        assert!(!titled(&mut pane, "Rhizome · vault").0);
        assert!(!pane.reported, "an older runtime's title is not a report");
        assert!(
            titled(&mut pane, report).0,
            "the first report takes the theme"
        );
        assert!(pane.reported);
        assert!(
            !titled(&mut pane, report).0,
            "later reports do not resend it"
        );
        assert_eq!(
            page_states(),
            vec![
                json!(null),
                json!(null),
                json!({"section": "agent", "search": ""}),
                json!({"section": "agent", "search": ""})
            ]
        );
        assert!(on_runtime(&pane));
        pane.cover(true);
        assert!(!on_runtime(&pane));
        assert!(
            on_runtime_document(&pane),
            "a covered page still takes the theme"
        );
        loading(&mut pane, &viewer);
        assert!(!on_runtime_document(&pane));
    }

    #[test]
    fn reports_carry_the_sidebar_state_when_the_page_has_one() {
        let title =
            r#"rhizome-desktop:{"v":1,"section":"explorer","search":"","sidebarCollapsed":true}"#;
        let report = decode(title, &page("/explorer"), Some(&runtime())).unwrap();
        assert_eq!(report.sidebar_collapsed, Some(true));
        assert_eq!(
            serde_json::to_value(&report).unwrap(),
            json!({"section": "explorer", "search": "", "sidebarCollapsed": true})
        );
        let title = r#"rhizome-desktop:{"v":1,"section":"explorer","search":""}"#;
        let report = decode(title, &page("/explorer"), Some(&runtime())).unwrap();
        assert_eq!(report.sidebar_collapsed, None);
        let title =
            r#"rhizome-desktop:{"v":1,"section":"explorer","search":"","sidebarCollapsed":"no"}"#;
        assert!(decode(title, &page("/explorer"), Some(&runtime())).is_none());
    }

    #[test]
    fn only_a_change_the_page_made_counts_as_a_sidebar_toggle() {
        let (mut pane, _received) = shown_pane();
        let report = |section: &str, sidebar: Option<(bool, u32)>| {
            let sidebar = sidebar
                .map(|(c, n)| format!(r#","sidebarCollapsed":{c},"sidebarToggles":{n}"#))
                .unwrap_or_default();
            format!(r#"rhizome-desktop:{{"v":1,"section":"{section}","search":""{sidebar}}}"#)
        };
        loading(&mut pane, &page("/notes"));
        assert_eq!(titled(&mut pane, &report("notes", None)), (true, None));
        assert!(!toggles_sidebar(&mut pane), "no sidebar command yet");
        // The page's echo of the app's command is not a toggle.
        assert_eq!(
            titled(&mut pane, &report("notes", Some((false, 0)))).1,
            None
        );
        assert!(toggles_sidebar(&mut pane));
        // Late replies to two quick app commands change the value but not the
        // page's own count, so neither undoes the newer choice.
        assert_eq!(titled(&mut pane, &report("notes", Some((true, 0)))).1, None);
        assert_eq!(
            titled(&mut pane, &report("notes", Some((false, 0)))).1,
            None
        );
        // ⌘\ in the page raises its count.
        assert_eq!(
            titled(&mut pane, &report("notes", Some((true, 1)))).1,
            Some(true)
        );
        assert_eq!(titled(&mut pane, &report("notes", Some((true, 1)))).1, None);
        assert_eq!(
            titled(&mut pane, &report("graphql", Some((true, 1)))).1,
            None
        );
        assert!(!toggles_sidebar(&mut pane), "GraphQL has no sidebar");
        assert!(titled(&mut pane, &report("agent", Some((true, 1))))
            .1
            .is_none());
        assert!(toggles_sidebar(&mut pane));
        pane.cover(true);
        assert!(!toggles_sidebar(&mut pane));
        pane.cover(false);
        // A new document starts its own count.
        loading(&mut pane, &page("/notes"));
        assert!(!toggles_sidebar(&mut pane));
        assert_eq!(
            titled(&mut pane, &report("notes", Some((false, 0)))),
            (true, None)
        );
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
        assert_eq!(
            Command::Theme {
                theme: Theme::Indigo
            }
            .script()
            .unwrap(),
            r#"window.dispatchEvent(new CustomEvent('rhizome:desktop', { detail: {"command":"theme","theme":"indigo"} }))"#
        );
        assert_eq!(
            serde_json::from_value::<Command>(json!({"command": "find"})).unwrap(),
            Command::Find
        );
        let sidebar: Command =
            serde_json::from_value(json!({"command": "sidebar", "collapsed": true})).unwrap();
        assert_eq!(sidebar, Command::Sidebar { collapsed: true });
        assert_eq!(
            sidebar.script().unwrap(),
            r#"window.dispatchEvent(new CustomEvent('rhizome:desktop', { detail: {"command":"sidebar","collapsed":true} }))"#
        );
        for invalid in [
            json!({"command": "sidebar"}),
            json!({"command": "sidebar", "collapsed": "yes"}),
            json!({"command": "sidebar", "collapsed": true, "width": 300}),
            // The shell cannot send the theme command; the app sends it from settings.
            json!({"command": "theme", "theme": "indigo"}),
            json!({"command": "section", "section": "settings"}),
            json!({"command": "navigate", "url": "https://example.com"}),
            json!({"command": "search"}),
        ] {
            assert!(serde_json::from_value::<Command>(invalid).is_err());
        }
    }
}
