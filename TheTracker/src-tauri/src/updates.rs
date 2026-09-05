//! In-app updates.
//!
//! Driven from Rust rather than the JavaScript updater plugin, because this
//! frontend has no bundler — the same reason the Convex auth flow lives in
//! Rust. The UI just calls two commands.
//!
//! Updates are served from the project's GitHub Releases: the endpoint
//! points at `releases/latest/download/latest.json`, which GitHub always
//! resolves to the newest published release, so shipping a new version
//! never means changing a URL.
//!
//! Every update is signature-checked against the public key baked into
//! `tauri.conf.json`. Tauri enforces this and it cannot be turned off, so a
//! tampered or unsigned download is rejected rather than installed.

use serde::Serialize;
use tauri_plugin_updater::UpdaterExt;

#[derive(Debug, Clone, Serialize)]
pub struct UpdateInfo {
    pub available: bool,
    /// The version on offer, when one is available.
    pub version: Option<String>,
    /// Release notes, straight from the manifest.
    pub notes: Option<String>,
    pub date: Option<String>,
    /// The version currently running, so the UI can show "x → y".
    pub current: String,
}

fn current_version() -> String {
    env!("CARGO_PKG_VERSION").to_string()
}

/// Asks the release feed whether anything newer exists.
///
/// A failure here is reported as an error string rather than swallowed:
/// "couldn't reach the update server" and "you're up to date" are very
/// different answers and the UI says which it got.
pub async fn check(app: &tauri::AppHandle) -> Result<UpdateInfo, String> {
    let updater = app.updater().map_err(|e| format!("Updater unavailable: {e}"))?;

    match updater.check().await {
        Ok(Some(update)) => Ok(UpdateInfo {
            available: true,
            version: Some(update.version.clone()),
            notes: update.body.clone(),
            date: update.date.map(|d| d.to_string()),
            current: current_version(),
        }),
        Ok(None) => Ok(UpdateInfo {
            available: false,
            version: None,
            notes: None,
            date: None,
            current: current_version(),
        }),
        Err(e) => Err(friendly(&e.to_string())),
    }
}

/// Downloads and installs the pending update, then restarts into it.
///
/// On Windows this hands off to the NSIS installer, so the app exits as
/// part of installing — `restart` never returns on the happy path.
pub async fn install(app: &tauri::AppHandle) -> Result<(), String> {
    let updater = app.updater().map_err(|e| format!("Updater unavailable: {e}"))?;

    let update = updater
        .check()
        .await
        .map_err(|e| friendly(&e.to_string()))?
        .ok_or_else(|| "No update available.".to_string())?;

    update
        .download_and_install(|_chunk, _total| {}, || {})
        .await
        .map_err(|e| format!("Update failed to install: {e}"))?;

    app.restart();
}

/// The updater's errors are accurate but not written for people.
fn friendly(raw: &str) -> String {
    let low = raw.to_lowercase();
    if low.contains("404") || low.contains("not found") {
        // Expected until the first release is published.
        "No releases published yet — nothing to update to.".to_string()
    } else if low.contains("dns") || low.contains("connect") || low.contains("network") {
        "Couldn't reach the update server — check your connection.".to_string()
    } else if low.contains("signature") {
        "That update failed its signature check and was not installed.".to_string()
    } else {
        format!("Update check failed: {raw}")
    }
}
