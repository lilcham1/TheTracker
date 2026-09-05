//! Stable per-install identity for the shared leaderboard.
//!
//! There's no sign-in: a random id is generated the first time the app runs
//! and kept next to the history/profile files. It's what tells one player's
//! synced rows apart from another's. The display name shown on the
//! leaderboard is the Profile username, which can change freely — this id
//! is what actually stays put.

use std::fs;

use crate::storage::log_dir;

fn device_id_file() -> std::path::PathBuf {
    log_dir().join("device_id.txt")
}

/// Reads the saved device id, generating and persisting one on first run.
pub fn device_id() -> String {
    let path = device_id_file();
    if let Ok(existing) = fs::read_to_string(&path) {
        let trimmed = existing.trim().to_string();
        if !trimmed.is_empty() {
            return trimmed;
        }
    }
    let fresh = uuid::Uuid::new_v4().to_string();
    let _ = fs::create_dir_all(log_dir());
    let _ = fs::write(&path, &fresh);
    fresh
}
