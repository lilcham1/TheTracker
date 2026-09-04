//! Local persistence: `history.json` (every finished match, append-only) and
//! `profile.json` (local username/rank/role). Same on-disk shape as the
//! original Node tracker, so an existing `logs/history.json` can be copied
//! straight into the new log directory and will load without changes.

use std::fs;
use std::path::PathBuf;

use crate::model::{MatchSummary, Profile};

/// Log directory: `DOTA_TRACKER_LOG_DIR` env var if set (matches the old
/// app's override), otherwise `<platform data dir>/DotaTracker/logs`.
pub fn log_dir() -> PathBuf {
    if let Ok(dir) = std::env::var("DOTA_TRACKER_LOG_DIR") {
        return PathBuf::from(dir);
    }
    let base = dirs::data_dir().unwrap_or_else(std::env::temp_dir);
    base.join("DotaTracker").join("logs")
}

fn history_file() -> PathBuf {
    log_dir().join("history.json")
}

fn profile_file() -> PathBuf {
    log_dir().join("profile.json")
}

fn ensure_dir() {
    let _ = fs::create_dir_all(log_dir());
}

pub fn load_history() -> Vec<MatchSummary> {
    match fs::read_to_string(history_file()) {
        Ok(s) => serde_json::from_str(&s).unwrap_or_default(),
        Err(_) => Vec::new(),
    }
}

pub fn save_history(history: &[MatchSummary]) {
    ensure_dir();
    if let Ok(json) = serde_json::to_string_pretty(history) {
        let _ = fs::write(history_file(), json);
    }
}

pub fn load_profile() -> Profile {
    match fs::read_to_string(profile_file()) {
        Ok(s) => serde_json::from_str(&s).unwrap_or_default(),
        Err(_) => Profile::default(),
    }
}

pub fn save_profile(profile: &Profile) {
    ensure_dir();
    if let Ok(json) = serde_json::to_string_pretty(profile) {
        let _ = fs::write(profile_file(), json);
    }
}
