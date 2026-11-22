//! Which host notices this app has shown, kept across launches so a relaunch
//! does not repeat them.
use super::api::Notice;
use serde::{Deserialize, Serialize};
use std::{
    collections::{HashSet, VecDeque},
    path::Path,
};

const FILE: &str = "host-notices.json";
/// Notice ids are stable, and a runtime reports only recent ones.
const KEEP: usize = 500;

#[derive(Default, Deserialize, Serialize)]
pub struct Notified {
    shown: VecDeque<String>,
}

impl Notified {
    /// An unreadable file means nothing was shown.
    pub fn load(directory: &Path) -> Self {
        std::fs::read(directory.join(FILE))
            .ok()
            .and_then(|data| serde_json::from_slice(&data).ok())
            .unwrap_or_default()
    }

    pub fn save(&self, directory: &Path) {
        let Ok(data) = serde_json::to_vec(self) else {
            return;
        };
        let pending = directory.join(format!("{FILE}.tmp"));
        if std::fs::write(&pending, data).is_ok() {
            let _ = std::fs::rename(pending, directory.join(FILE));
        }
    }

    /// The notices from `repository` not shown before, now recorded. Past
    /// [`KEEP`] ids, the oldest of this repository's ids that `notices` no
    /// longer lists are forgotten, so a visible notice, here or in another
    /// repository, is never shown twice.
    pub fn fresh(&mut self, repository: &str, notices: &[Notice]) -> Vec<Notice> {
        let prefix = format!("{repository}\n");
        let key = |notice: &Notice| format!("{prefix}{}", notice.id);
        let visible: HashSet<String> = notices.iter().map(key).collect();
        let mut fresh = vec![];
        for notice in notices.iter().filter(|n| !n.id.is_empty()) {
            let key = key(notice);
            if self.shown.contains(&key) {
                continue;
            }
            self.shown.push_back(key);
            fresh.push(notice.clone());
        }
        let mut excess = self.shown.len().saturating_sub(KEEP);
        self.shown.retain(|id| {
            let forget = excess > 0 && id.starts_with(&prefix) && !visible.contains(id);
            excess -= usize::from(forget);
            !forget
        });
        fresh
    }
}
