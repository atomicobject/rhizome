//! Which host notices this app has shown, kept across launches so a relaunch
//! does not repeat them.
use super::api::Notice;
use serde::{Deserialize, Serialize};
use std::{collections::VecDeque, path::Path};

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

    /// The notices from `repository` not shown before, now recorded.
    pub fn fresh(&mut self, repository: &str, notices: &[Notice]) -> Vec<Notice> {
        let mut fresh = vec![];
        for notice in notices.iter().filter(|n| !n.id.is_empty()) {
            let key = format!("{repository}\n{}", notice.id);
            if self.shown.contains(&key) {
                continue;
            }
            self.shown.push_back(key);
            if self.shown.len() > KEEP {
                self.shown.pop_front();
            }
            fresh.push(notice.clone());
        }
        fresh
    }
}
