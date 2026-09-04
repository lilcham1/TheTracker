//! Dota Tracker — native desktop match tracker built on Valve's official
//! Game State Integration (GSI) feed. No memory reading, no third-party
//! game data, nothing that touches Dota 2's process — just the same
//! official local HTTP feed pro broadcast overlays use.
//!
//! This is the Tauri shell: a thin Rust backend (this crate) exposing
//! commands to an HTML/CSS/JS frontend in `../ui`. All the actual GSI
//! parsing and match-tracking logic lives in `state.rs`, unchanged from
//! the original native-egui version — only the UI layer changed.

mod gsi;
mod heroes;
mod model;
mod state;
mod storage;

use std::sync::{Arc, Mutex};

use serde::Serialize;

use model::{MatchState, MatchSummary, Profile};
use state::Tracker;

struct AppState {
    tracker: Arc<Mutex<Tracker>>,
    server_error: Arc<Mutex<Option<String>>>,
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
fn set_history_game_type(matchid: String, game_type: String) -> Result<Vec<MatchSummary>, String> {
    let mut history = storage::load_history();
    if !state::set_history_game_type(&mut history, &matchid, &game_type) {
        return Err("Match not found, or not a recognized game type".to_string());
    }
    storage::save_history(&history);
    Ok(history)
}

#[tauri::command]
fn get_profile() -> Profile {
    storage::load_profile()
}

#[tauri::command]
fn save_profile(profile: Profile) {
    storage::save_profile(&profile);
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

    tauri::Builder::default()
        .manage(AppState { tracker, server_error })
        .invoke_handler(tauri::generate_handler![
            get_live_state,
            set_tracking,
            mark_roshan_death,
            set_live_game_type,
            get_history,
            set_history_game_type,
            get_profile,
            save_profile,
        ])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
