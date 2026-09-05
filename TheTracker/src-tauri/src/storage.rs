//! Local persistence: `history.json` (every finished match, append-only) and
//! `profile.json` (local username/rank/role). Same on-disk shape as the
//! original Node tracker, so an existing `logs/history.json` can be copied
//! straight into the new log directory and will load without changes.

use std::fs;
use std::path::PathBuf;

use crate::model::{MatchSummary, Profile};

/// Log directory: `THETRACKER_LOG_DIR` (or the legacy
/// `DOTA_TRACKER_LOG_DIR`) if set, otherwise
/// `<platform data dir>/TheTracker/logs`.
pub fn log_dir() -> PathBuf {
    for var in ["THETRACKER_LOG_DIR", "DOTA_TRACKER_LOG_DIR"] {
        if let Ok(dir) = std::env::var(var) {
            return PathBuf::from(dir);
        }
    }
    let base = dirs::data_dir().unwrap_or_else(std::env::temp_dir);
    base.join("TheTracker").join("logs")
}

/// The app used to store data under `DotaTracker/logs`. Renaming to
/// TheTracker would otherwise look like losing every past match, so on first
/// run the old directory is copied across (never moved — if anything goes
/// wrong the original is still sitting there untouched).
pub fn migrate_legacy_dir() {
    let new_dir = log_dir();
    if new_dir.join("history.json").exists() {
        return;
    }
    let Some(base) = dirs::data_dir() else { return };
    let old_dir = base.join("DotaTracker").join("logs");
    if !old_dir.is_dir() || old_dir == new_dir {
        return;
    }
    if fs::create_dir_all(&new_dir).is_err() {
        return;
    }
    let Ok(entries) = fs::read_dir(&old_dir) else { return };
    for entry in entries.flatten() {
        if !entry.path().is_file() {
            continue;
        }
        let target = new_dir.join(entry.file_name());
        if !target.exists() {
            let _ = fs::copy(entry.path(), target);
        }
    }
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
