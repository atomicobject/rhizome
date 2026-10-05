use url::Url;

/// Only the bundled shell webview of each window may call native commands.
pub fn shell_authorized(label: &str, url: &Url, development: bool) -> bool {
    if !label.starts_with("shell-") {
        return false;
    }
    let packaged = (url.scheme() == "tauri" && url.host_str() == Some("localhost"))
        || (url.scheme() == "http"
            && url.host_str() == Some("tauri.localhost")
            && url.port().is_none());
    packaged
        || (development && url.origin() == Url::parse("http://127.0.0.1:1420").unwrap().origin())
}

pub fn runtime_url(raw: &str) -> Result<Url, String> {
    let url = Url::parse(raw).map_err(|_| "Rhizome returned an invalid address.")?;
    if url.scheme() != "http"
        || !matches!(url.host_str(), Some("127.0.0.1" | "[::1]"))
        || url.port().is_none()
        || !url.username().is_empty()
        || url.password().is_some()
        || url.path() != "/"
        || url.query().is_some()
        || url.fragment().is_some()
    {
        return Err(
            "Rhizome must provide an HTTP address on the loopback interface with an explicit port."
                .into(),
        );
    }
    Ok(url)
}

pub fn same_origin(expected: &Url, candidate: &Url) -> bool {
    candidate.username().is_empty()
        && candidate.password().is_none()
        && expected.origin() == candidate.origin()
}

pub fn runtime_content_origin(expected: &Url, candidate: &Url) -> bool {
    if !candidate.username().is_empty()
        || candidate.password().is_some()
        || candidate.scheme() != expected.scheme()
        || candidate.port_or_known_default() != expected.port_or_known_default()
    {
        return false;
    }
    let Some(token) = candidate
        .host_str()
        .and_then(|host| host.strip_suffix(".localhost"))
    else {
        return false;
    };
    token.len() == 32
        && token
            .bytes()
            .all(|c| c.is_ascii_digit() || (b'a'..=b'f').contains(&c))
}

pub fn empty_frame_document(candidate: &Url) -> bool {
    matches!(candidate.as_str(), "about:blank" | "about:srcdoc")
}

pub fn external_url(url: &Url) -> bool {
    matches!(url.scheme(), "http" | "https")
        && url.username().is_empty()
        && url.password().is_none()
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn rejects_runtime_addresses_outside_loopback() {
        for raw in [
            "https://127.0.0.1:8080",
            "http://localhost:8080",
            "http://127.0.0.1.evil.test:8080",
            "file:///tmp/x",
            "http://user@127.0.0.1:8080",
            "http://127.0.0.1",
            "http://127.0.0.1:8710/other",
            "http://127.0.0.1:8710/?token=abc",
            "http://127.0.0.1:8710/#fragment",
        ] {
            assert!(runtime_url(raw).is_err(), "{raw}");
        }
        assert!(runtime_url("http://127.0.0.1:8710/").is_ok());
        assert!(runtime_url("http://[::1]:8710/").is_ok());
    }
    #[test]
    fn repository_navigation_is_confined_to_one_origin() {
        let origin = runtime_url("http://127.0.0.1:8710/").unwrap();
        assert!(same_origin(
            &origin,
            &Url::parse("http://127.0.0.1:8710/note?x=1").unwrap()
        ));
        for raw in [
            "http://127.0.0.1:8711/",
            "http://localhost:8710/",
            "https://example.com/",
            "tauri://localhost/",
        ] {
            assert!(!same_origin(&origin, &Url::parse(raw).unwrap()));
        }
    }
    #[test]
    fn isolated_html_origins_are_narrowly_scoped_to_runtime() {
        let origin = runtime_url("http://127.0.0.1:8710/").unwrap();
        assert!(runtime_content_origin(
            &origin,
            &Url::parse("http://0123456789abcdef0123456789abcdef.localhost:8710/view").unwrap()
        ));
        for raw in [
            "http://other.localhost:8710/",
            "http://a.0123456789abcdef0123456789abcdef.localhost:8710/",
            "http://0123456789abcdef0123456789abcdef.localhost:8711/",
            "https://0123456789abcdef0123456789abcdef.localhost:8710/",
            "http://user@0123456789abcdef0123456789abcdef.localhost:8710/",
        ] {
            assert!(
                !runtime_content_origin(&origin, &Url::parse(raw).unwrap()),
                "{raw}"
            );
        }
    }
    #[test]
    fn allows_only_empty_browser_documents_without_native_authority() {
        for raw in ["about:blank", "about:srcdoc"] {
            let url = Url::parse(raw).unwrap();
            assert!(empty_frame_document(&url));
            assert!(!shell_authorized("content-1", &url, false));
        }
        for raw in [
            "about:config",
            "about:blank?x=1",
            "about:srcdoc#fragment",
            "data:text/html,hello",
            "javascript:alert(1)",
            "file:///tmp/note.html",
        ] {
            assert!(!empty_frame_document(&Url::parse(raw).unwrap()), "{raw}");
        }
    }
    #[test]
    fn only_local_shell_is_authorized() {
        let local = Url::parse("tauri://localhost/index.html").unwrap();
        assert!(shell_authorized("shell-1", &local, false));
        assert!(!shell_authorized("content-1", &local, false));
        assert!(!shell_authorized("window-1", &local, false));
        assert!(!shell_authorized(
            "shell-1",
            &runtime_url("http://127.0.0.1:8710").unwrap(),
            true
        ));
        let dev = Url::parse("http://127.0.0.1:1420/").unwrap();
        assert!(shell_authorized("shell-1", &dev, true));
        assert!(!shell_authorized("shell-1", &dev, false));
    }

    #[test]
    fn capabilities_reach_only_shell_webviews() {
        let directory = concat!(env!("CARGO_MANIFEST_DIR"), "/capabilities");
        for entry in std::fs::read_dir(directory).unwrap() {
            let path = entry.unwrap().path();
            let capability: serde_json::Value =
                serde_json::from_slice(&std::fs::read(&path).unwrap()).unwrap();
            assert!(
                capability.get("windows").is_none(),
                "{path:?} must scope by webview, since a window grants all of its webviews"
            );
            assert!(capability.get("remote").is_none(), "{path:?}");
            let webviews = capability["webviews"].as_array().unwrap();
            assert!(!webviews.is_empty());
            for pattern in webviews {
                assert_eq!(pattern, "shell-*", "{path:?}");
            }
        }
    }
}
