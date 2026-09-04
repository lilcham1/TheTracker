//! Dota Tracker — native desktop match tracker built on Valve's official
//! Game State Integration (GSI) feed. No memory reading, no third-party
//! game data, nothing that touches Dota 2's process — just the same
//! official local HTTP feed pro broadcast overlays use.
//!
//! This is the Tauri shell: a thin Rust backend (this crate) exposing
//! commands to an HTML/CSS/JS frontend in `../ui`. All the actual GSI
//! parsing and match-tracking logic lives in `state.rs`, unchanged from
//! the original native-egui version — only the UI layer changed.

mod convex_sync;
mod device_id;
mod gsi;
mod heroes;
mod model;
mod state;
mod storage;

use std::sync::{Arc, Mutex};

use serde::Serialize;
use tauri::Manager;

use convex_sync::{SyncJob, SyncStatus, Syncer};
use model::{MatchState, MatchSummary, Profile};
use state::Tracker;

struct AppState {
    tracker: Arc<Mutex<Tracker>>,
    server_error: Arc<Mutex<Option<String>>>,
    syncer: Syncer,
}

#[derive(Serialize)]
struct LiveStatus {
    current: Option<MatchState>,
    #[serde(rename = "trackingEnabled")]
    tracking_enabled: bool,
    #[serde(rename = "serverError")]
    server_error: Option<String>,
}

#[tauri::command]
fn get_live_state(app_state: tauri::State<AppState>) -> LiveStatus {
    let tracker = app_state.tracker.lock().unwrap();
    let server_error = app_state.server_error.lock().unwrap().clone();
    LiveStatus { current: tracker.current.clone(), tracking_enabled: tracker.tracking_enabled, server_error }
}

#[tauri::command]
fn set_tracking(enabled: bool, app_state: tauri::State<AppState>) {
    app_state.tracker.lock().unwrap().tracking_enabled = enabled;
}

#[tauri::command]
fn mark_roshan_death(app_state: tauri::State<AppState>) {
    app_state.tracker.lock().unwrap().mark_roshan_death("manual");
}

#[tauri::command]
fn set_live_game_type(game_type: String, app_state: tauri::State<AppState>) {
    app_state.tracker.lock().unwrap().set_game_type(&game_type);
}

#[tauri::command]
fn get_history() -> Vec<MatchSummary> {
    storage::load_history()
}

/// Re-tags a finished match's game type (Ranked/All Pick/Turbo/Other) from
/// the History tab and recomputes every match's historical comparison so
/// stats stay consistent with the (possibly changed) peer groups.
#[tauri::command]
fn set_history_game_type(
    matchid: String,
    game_type: String,
    app_state: tauri::State<AppState>,
) -> Result<Vec<MatchSummary>, String> {
    let mut history = storage::load_history();
    if !state::set_history_game_type(&mut history, &matchid, &game_type) {
        return Err("Match not found, or not a recognized game type".to_string());
    }
    storage::save_history(&history);

    // Push the re-tagged match back up, or the cloud row (and so the shared
    // leaderboard's type filter) would keep the old game type.
    if let Some(updated) = history.iter().find(|m| m.matchid == matchid) {
        app_state.syncer.send(SyncJob::Match(Box::new(updated.clone())));
    }
    Ok(history)
}

#[tauri::command]
fn get_profile() -> Profile {
    storage::load_profile()
}

#[tauri::command]
fn save_profile(profile: Profile, app_state: tauri::State<AppState>) {
    storage::save_profile(&profile);
    // Keep the name shown on the shared leaderboard current.
    app_state.syncer.send(SyncJob::Profile(Box::new(profile)));
}

// ---------- Convex ----------

#[tauri::command]
fn sync_status(app_state: tauri::State<AppState>) -> SyncStatus {
    app_state.syncer.status.lock().unwrap().clone()
}

/// Pushes the entire local history up. Safe to run repeatedly — the Convex
/// mutation upserts on (deviceId, matchid), so nothing duplicates.
#[tauri::command]
fn sync_all(app_state: tauri::State<AppState>) -> usize {
    let history = storage::load_history();
    let n = history.len();
    app_state.syncer.send(SyncJob::Matches(history));
    app_state.syncer.send(SyncJob::Profile(Box::new(storage::load_profile())));
    n
}

/// Cross-player leaderboard, pulled live from Convex.
#[tauri::command]
async fn global_leaderboard(
    metric: String,
    game_type: String,
    limit: Option<f64>,
) -> Result<serde_json::Value, String> {
    convex_sync::global_leaderboard(&metric, &game_type, limit.unwrap_or(10.0)).await
}

/// Everything this install has synced, for restoring onto a new machine.
#[tauri::command]
async fn cloud_history(device: String) -> Result<serde_json::Value, String> {
    convex_sync::cloud_history(&device).await
}

#[tauri::command]
fn device_identity(app_state: tauri::State<AppState>) -> String {
    app_state.syncer.device_id.clone()
}

fn main() {
    let tracker = Arc::new(Mutex::new(Tracker::new()));
    let server_error: Arc<Mutex<Option<String>>> = Arc::new(Mutex::new(None));

    {
        let tracker_for_server = tracker.clone();
        let server_error_for_server = server_error.clone();
        gsi::spawn_server(tracker_for_server, move |status| {
            if let gsi::ServerStatus::Failed(msg) = status {
                *server_error_for_server.lock().unwrap() = Some(msg);
            }
        });
    }

    let tracker_for_setup = tracker.clone();
    tauri::Builder::default()
        .setup(move |app| {
            // The sync worker runs on Tauri's async runtime, so it can only
            // start once the app is being set up — not before the builder.
            let syncer = convex_sync::spawn(device_id::device_id());
            tracker_for_setup.lock().unwrap().syncer = Some(syncer.clone());
            app.manage(AppState {
                tracker: tracker_for_setup.clone(),
                server_error: server_error.clone(),
                syncer,
            });
            Ok(())
        })
        .invoke_handler(tauri::generate_handler![
            get_live_state,
            set_tracking,
            mark_roshan_death,
            set_live_game_type,
            get_history,
            set_history_game_type,
            get_profile,
            save_profile,
            sync_status,
            sync_all,
            global_leaderboard,
            cloud_history,
            device_identity,
        ])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
