// Build release binaries as a Windows GUI app rather than a console app, so
// launching the tracker doesn't pop an empty black console window alongside
// it. Debug builds keep the console — that's where panics and the GSI log
// lines show up while developing.
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

//! Dota Tracker — native desktop match tracker built on Valve's official
//! Game State Integration (GSI) feed. No memory reading, no third-party
//! game data, nothing that touches Dota 2's process — just the same
//! official local HTTP feed pro broadcast overlays use.
//!
//! This is the Tauri shell: a thin Rust backend (this crate) exposing
//! commands to an HTML/CSS/JS frontend in `../ui`. All the actual GSI
//! parsing and match-tracking logic lives in `state.rs`, unchanged from
//! the original native-egui version — only the UI layer changed.

mod auth;
mod convex_sync;
mod deadlock;
mod dota_api;
mod device_id;
mod gsi;
mod heroes;
mod model;
mod overlay;
mod popular;
mod prefs;
mod state;
mod steam;
mod storage;
mod updates;

use std::sync::{Arc, Mutex};

use serde::Serialize;
use tauri::Manager;

use auth::{AuthState, SharedAuth};
use convex_sync::{SyncJob, SyncStatus, Syncer};
use model::{MatchState, MatchSummary, Profile};
use state::Tracker;

struct AppState {
    tracker: Arc<Mutex<Tracker>>,
    server_error: Arc<Mutex<Option<String>>>,
    syncer: Syncer,
    auth: SharedAuth,
    heroes: deadlock::SharedHeroes,
    dota_heroes: dota_api::SharedDotaHeroes,
    items: popular::SharedItemNames,
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

/// Everything this account has published, for restoring onto a new machine.
#[tauri::command]
async fn cloud_history(app_state: tauri::State<'_, AppState>) -> Result<serde_json::Value, String> {
    let auth = app_state.auth.clone();
    convex_sync::cloud_history(&auth).await
}

#[tauri::command]
fn device_identity(app_state: tauri::State<AppState>) -> String {
    app_state.syncer.device_id.clone()
}

// ---------- Accounts ----------

#[tauri::command]
fn auth_status(app_state: tauri::State<AppState>) -> AuthState {
    app_state.auth.lock().unwrap().clone()
}

/// `flow` is "signUp" to create an account or "signIn" for an existing one.
/// On success, matches this install synced before it had an account are
/// claimed, and anything waiting locally is pushed up.
#[tauri::command]
async fn sign_in(
    email: String,
    password: String,
    flow: String,
    app_state: tauri::State<'_, AppState>,
) -> Result<AuthState, String> {
    let auth = app_state.auth.clone();
    let device = app_state.syncer.device_id.clone();

    auth::sign_in(auth.clone(), email, password, &flow).await?;
    // Best-effort: a failure here shouldn't undo an otherwise good sign-in.
    let _ = convex_sync::claim_device(&auth, &device).await;

    let status = auth.lock().unwrap().clone();
    Ok(status)
}

#[tauri::command]
async fn sign_out(app_state: tauri::State<'_, AppState>) -> Result<AuthState, String> {
    let auth = app_state.auth.clone();
    auth::sign_out(auth.clone()).await;
    let status = auth.lock().unwrap().clone();
    Ok(status)
}

// ---------- Deadlock ----------

#[tauri::command]
fn deadlock_link_status() -> deadlock::DeadlockLink {
    deadlock::load_link()
}

#[tauri::command]
async fn deadlock_search(query: String) -> Result<Vec<deadlock::SteamProfile>, String> {
    if query.trim().len() < 2 {
        return Err("Type at least two characters to search.".to_string());
    }
    deadlock::search_players(query.trim()).await
}

#[tauri::command]
fn deadlock_link(
    account_id: u64,
    personaname: String,
    avatar: Option<String>,
) -> deadlock::DeadlockLink {
    let link = deadlock::DeadlockLink {
        account_id: Some(account_id),
        personaname: Some(personaname),
        avatar,
    };
    deadlock::save_link(&link);
    link
}

#[tauri::command]
fn deadlock_unlink() -> deadlock::DeadlockLink {
    let empty = deadlock::DeadlockLink::default();
    deadlock::save_link(&empty);
    empty
}

#[derive(Serialize)]
struct DeadlockOverview {
    matches: Vec<deadlock::DeadlockMatch>,
    summary: deadlock::DeadlockSummary,
    rank: Option<deadlock::DeadlockRank>,
}

#[tauri::command]
async fn deadlock_overview(
    limit: Option<usize>,
    app_state: tauri::State<'_, AppState>,
) -> Result<DeadlockOverview, String> {
    let link = deadlock::load_link();
    let Some(account_id) = link.account_id else {
        return Err("No Deadlock account linked yet.".to_string());
    };
    let cache = app_state.heroes.clone();

    let matches = deadlock::match_history(account_id, &cache, limit.unwrap_or(50)).await?;
    let summary = deadlock::summarize(&matches);
    // A missing rank shouldn't sink the whole view — plenty of accounts
    // simply haven't been ranked yet.
    let rank = deadlock::rank(account_id).await.unwrap_or(None);

    Ok(DeadlockOverview { matches, summary, rank })
}

#[tauri::command]
async fn deadlock_live(
    app_state: tauri::State<'_, AppState>,
) -> Result<Option<deadlock::DeadlockLive>, String> {
    let Some(account_id) = deadlock::load_link().account_id else { return Ok(None) };
    let cache = app_state.heroes.clone();
    deadlock::live_match(account_id, &cache).await
}

// ---------- Dota match history (OpenDota) ----------

#[tauri::command]
fn dota_link_status() -> dota_api::DotaLink {
    dota_api::load_link()
}

#[tauri::command]
async fn dota_search(query: String) -> Result<Vec<dota_api::DotaProfile>, String> {
    if query.trim().len() < 2 {
        return Err("Type at least two characters to search.".to_string());
    }
    dota_api::search_players(query.trim()).await
}

#[tauri::command]
fn dota_link(account_id: u64, personaname: String, avatar: Option<String>) -> dota_api::DotaLink {
    let link = dota_api::DotaLink {
        account_id: Some(account_id),
        personaname: Some(personaname),
        avatar,
    };
    dota_api::save_link(&link);
    link
}

#[tauri::command]
fn dota_unlink() -> dota_api::DotaLink {
    let empty = dota_api::DotaLink::default();
    dota_api::save_link(&empty);
    empty
}

#[derive(Serialize)]
struct DotaApiOverview {
    matches: Vec<dota_api::DotaApiMatch>,
    summary: dota_api::DotaApiSummary,
}

#[tauri::command]
async fn dota_api_history(
    limit: Option<usize>,
    app_state: tauri::State<'_, AppState>,
) -> Result<DotaApiOverview, String> {
    let Some(account_id) = dota_api::load_link().account_id else {
        return Err("No Steam account linked yet.".to_string());
    };
    let cache = app_state.dota_heroes.clone();
    let matches = dota_api::match_history(account_id, &cache, limit.unwrap_or(50)).await?;
    let summary = dota_api::summarize(&matches);
    Ok(DotaApiOverview { matches, summary })
}

#[tauri::command]
async fn dota_match_detail(
    match_id: u64,
    app_state: tauri::State<'_, AppState>,
) -> Result<dota_api::DotaMatchDetail, String> {
    let me = dota_api::load_link().account_id;
    let cache = app_state.dota_heroes.clone();
    dota_api::match_detail(match_id, me, &cache).await
}

/// Steam accounts known to this PC, for one-click linking. Reads only the
/// account id and display name — never credentials or auth tokens. See
/// steam.rs for the specifics.
#[tauri::command]
fn steam_accounts() -> Vec<steam::SteamAccount> {
    steam::detect()
}

// ---------- Preferences: favourites, builds, overlay ----------

#[tauri::command]
fn get_prefs() -> prefs::Prefs {
    prefs::load()
}

#[tauri::command]
fn set_favorite_hero(game: String, hero: Option<String>) -> prefs::Prefs {
    prefs::set_favorite(&game, hero)
}

#[tauri::command]
fn save_build(build: prefs::Build) -> prefs::Prefs {
    prefs::upsert_build(build)
}

#[tauri::command]
fn delete_build(id: String) -> prefs::Prefs {
    prefs::delete_build(&id)
}

/// Persists overlay appearance and applies it to the live window, so the
/// change is visible immediately rather than after a restart.
#[tauri::command]
fn save_overlay_settings(settings: prefs::OverlaySettings, app: tauri::AppHandle) -> prefs::Prefs {
    let p = prefs::save_overlay(settings);
    overlay::apply_settings(&app, &p.overlay);
    p
}

#[tauri::command]
async fn deadlock_match_detail(
    match_id: u64,
    app_state: tauri::State<'_, AppState>,
) -> Result<deadlock::DeadlockMatchDetail, String> {
    let me = deadlock::load_link().account_id;
    let cache = app_state.heroes.clone();
    deadlock::match_detail(match_id, me, &cache).await
}

// ---------- Updates ----------

#[tauri::command]
async fn check_for_update(app: tauri::AppHandle) -> Result<updates::UpdateInfo, String> {
    updates::check(&app).await
}

#[tauri::command]
async fn install_update(app: tauri::AppHandle) -> Result<(), String> {
    updates::install(&app).await
}

// ---------- Popular builds ----------

#[tauri::command]
async fn dota_popular_builds(
    hero_id: u32,
    app_state: tauri::State<'_, AppState>,
) -> Result<Vec<popular::PopularBuild>, String> {
    let cache = app_state.items.clone();
    popular::dota_builds(hero_id, &cache, 6).await
}

#[tauri::command]
async fn deadlock_popular_items(hero_id: u32) -> Result<Vec<popular::DeadlockPopularItem>, String> {
    popular::deadlock_builds(hero_id, 12).await
}

// ---------- Overlay ----------

#[tauri::command]
fn overlay_show(app: tauri::AppHandle) -> Result<(), String> {
    overlay::show(&app)
}

#[tauri::command]
fn overlay_hide(app: tauri::AppHandle) -> Result<(), String> {
    overlay::hide(&app)
}

#[tauri::command]
fn overlay_visible(app: tauri::AppHandle) -> bool {
    overlay::is_visible(&app)
}

#[tauri::command]
fn overlay_click_through(app: tauri::AppHandle, click_through: bool) -> Result<(), String> {
    overlay::set_click_through(&app, click_through)
}

fn main() {
    // Must run before anything reads history/profile files.
    storage::migrate_legacy_dir();

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
        .plugin(tauri_plugin_updater::Builder::new().build())
        .setup(move |app| {
            // The sync worker runs on Tauri's async runtime, so it can only
            // start once the app is being set up — not before the builder.
            let auth: SharedAuth = Arc::new(Mutex::new(AuthState::default()));
            // Restores a previous session from the stored refresh token.
            auth::restore(auth.clone());

            let syncer = convex_sync::spawn(device_id::device_id(), auth.clone());
            tracker_for_setup.lock().unwrap().syncer = Some(syncer.clone());
            app.manage(AppState {
                tracker: tracker_for_setup.clone(),
                server_error: server_error.clone(),
                syncer,
                auth,
                heroes: Arc::new(Mutex::new(deadlock::HeroCache::default())),
                dota_heroes: Arc::new(Mutex::new(dota_api::DotaHeroCache::default())),
                items: Arc::new(Mutex::new(popular::ItemNameCache::default())),
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
            auth_status,
            sign_in,
            sign_out,
            deadlock_link_status,
            deadlock_search,
            deadlock_link,
            deadlock_unlink,
            deadlock_overview,
            deadlock_live,
            deadlock_match_detail,
            steam_accounts,
            get_prefs,
            set_favorite_hero,
            save_build,
            delete_build,
            save_overlay_settings,
            check_for_update,
            install_update,
            dota_popular_builds,
            deadlock_popular_items,
            overlay_show,
            overlay_hide,
            overlay_visible,
            overlay_click_through,
            dota_link_status,
            dota_search,
            dota_link,
            dota_unlink,
            dota_api_history,
            dota_match_detail,
        ])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
