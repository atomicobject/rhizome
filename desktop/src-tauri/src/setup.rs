use serde::{Deserialize, Serialize};
use std::fmt;

/// Serialized only into the bridge's stdin payload. Diagnostics never reveal it.
#[derive(Deserialize, Serialize)]
#[serde(transparent)]
pub struct Secret(String);

impl fmt::Debug for Secret {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("[redacted]")
    }
}

impl fmt::Display for Secret {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("[redacted]")
    }
}

#[derive(Deserialize, Serialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct SetupChoices {
    pub workflow: String,
    pub addons: Vec<String>,
    pub agents: Vec<String>,
    pub search: String,
    pub skip: Vec<String>,
    pub keep_indexed: Vec<String>,
    pub include_ignored: Vec<String>,
}

#[derive(Deserialize, Serialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct ScopeEdits {
    pub skip: Vec<String>,
    pub remove_rules: Vec<String>,
    pub include_ignored: Vec<String>,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn secret_diagnostics_are_redacted_but_stdin_payload_keeps_the_value() {
        let secret: Secret = serde_json::from_str("\"synthetic-search-secret\"").unwrap();
        assert_eq!(format!("{secret:?}"), "[redacted]");
        assert_eq!(format!("{secret}"), "[redacted]");
        assert_eq!(
            serde_json::to_string(&secret).unwrap(),
            "\"synthetic-search-secret\""
        );
    }
}
