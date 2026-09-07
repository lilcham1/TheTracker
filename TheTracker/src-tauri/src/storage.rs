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

// ---------- Warm-start cache ----------
//
// The in-memory caches use `Instant`, which is a reading of a monotonic
// clock held in RAM. Their TTLs — a day for hero and item constants, half an
// hour for meta — therefore never survived a restart: every launch began
// with every cache empty and refetched the lot from OpenDota, which is slow
// and rate-limited. That is why the app opened, failed, and only filled in
// once something retried.
//
// These write the same payloads to disk with a wall-clock stamp, so a
// relaunch starts warm and works offline until the data is genuinely stale.

fn cache_dir() -> PathBuf {
    log_dir().join("cache")
}

fn now_secs() -> u64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_secs())
        .unwrap_or(0)
}

/// Reads a cached payload if it exists and is younger than `ttl_secs`.
///
/// A clock that has moved backwards — a timezone change, an NTP correction —
/// would make `now - stamp` underflow and look infinitely fresh, so that
/// case is treated as stale rather than trusted.
pub fn read_cache(name: &str, ttl_secs: u64) -> Option<serde_json::Value> {
    let raw = fs::read_to_string(cache_dir().join(format!("{name}.json"))).ok()?;
    let v: serde_json::Value = serde_json::from_str(&raw).ok()?;
    let stamp = v.get("fetchedAt")?.as_u64()?;
    let now = now_secs();
    if now < stamp || now - stamp >= ttl_secs {
        return None;
    }
    v.get("payload").cloned()
}

pub fn write_cache(name: &str, payload: &serde_json::Value) {
    let dir = cache_dir();
    if fs::create_dir_all(&dir).is_err() {
        return;
    }
    let wrapped = serde_json::json!({ "fetchedAt": now_secs(), "payload": payload });
    // Written via a temporary file and renamed, so a crash mid-write cannot
    // leave a half-written cache that then fails to parse on every launch.
    let tmp = dir.join(format!("{name}.json.tmp"));
    if serde_json::to_string(&wrapped).map(|s| fs::write(&tmp, s)).is_ok() {
        let _ = fs::rename(&tmp, dir.join(format!("{name}.json")));
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_cache_written_now_reads_back_and_one_past_its_ttl_does_not() {
        let dir = std::env::temp_dir().join(format!("tt-cache-test-{}", now_secs()));
        std::env::set_var("THETRACKER_LOG_DIR", &dir);

        let payload = serde_json::json!({ "heroes": [1, 2, 3] });
        write_cache("probe", &payload);

        assert_eq!(read_cache("probe", 3600), Some(payload), "fresh cache should read back");
        assert_eq!(read_cache("probe", 0), None, "a zero TTL makes everything stale");
        assert_eq!(read_cache("absent", 3600), None, "a missing cache is not an error");

        std::env::remove_var("THETRACKER_LOG_DIR");
        let _ = fs::remove_dir_all(&dir);
    }
}
