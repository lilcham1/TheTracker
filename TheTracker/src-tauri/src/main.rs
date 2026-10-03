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
mod gsi_setup;
mod heroes;
mod meta;
mod model;
mod overlay;
mod popular;
mod prefs;
mod state;
mod steam;
mod storage;
mod updates;

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use serde::Serialize;
use tauri::menu::{Menu, MenuItem};
use tauri::tray::{MouseButton, MouseButtonState, TrayIcon, TrayIconBuilder, TrayIconEvent};
use tauri::Manager;
use tauri_plugin_autostart::{MacosLauncher, ManagerExt};

use auth::{AuthState, SharedAuth};
use convex_sync::{SyncJob, SyncStatus, Syncer};
use model::{MatchState, MatchSummary, Profile};
use state::Tracker;

/// Passed by the "start with Windows" entry, so a launch at login goes
/// straight to the tray instead of putting a window in front of whatever
/// the player was about to do.
const MINIMIZED_ARG: &str = "--minimized";

/// Dota posts a heartbeat every 30 seconds while it runs (see the config in
/// gsi_setup.rs), so silence well past that means it has stopped talking.
const MATCH_FEED_TIMEOUT: Duration = Duration::from_secs(45);

/// Set once the tray icon exists. Hiding the window on close is only safe
/// with somewhere to bring it back from — without a tray, close has to mean
/// quit, or the app would vanish with no way back short of relaunching.
static TRAY_READY: AtomicBool = AtomicBool::new(false);

struct AppState {
    tracker: Arc<Mutex<Tracker>>,
    server_error: Arc<Mutex<Option<String>>>,
    syncer: Syncer,
    auth: SharedAuth,
    heroes: deadlock::SharedHeroes,
    dota_heroes: dota_api::SharedDotaHeroes,
    items: popular::SharedItemNames,
    meta: meta::SharedMeta,
}

/// Holds the tray icon for the life of the app. Tauri also registers it,
/// but the handle is reference-counted and the icon disappears when the
/// last one drops; keeping one here makes that impossible to get wrong.
struct TrayState(#[allow(dead_code)] TrayIcon);

#[derive(Serialize)]
struct LiveStatus {
    current: Option<MatchState>,
    #[serde(rename = "trackingEnabled")]
    tracking_enabled: bool,
    #[serde(rename = "serverError")]
    server_error: Option<String>,
    /// Seconds since Dota last posted anything, menus included. `None` if it
    /// has not since TheTracker started — which, with Dota open, means the
    /// feed is not reaching us.
    #[serde(rename = "gsiAgeSecs")]
    gsi_age_secs: Option<u64>,
}

#[derive(Serialize)]
struct ServiceCheck {
    name: &'static str,
    ok: bool,
    detail: String,
    #[serde(rename = "latencyMs")]
    latency_ms: Option<u128>,
}

#[derive(Serialize)]
struct Diagnostics {
    steam: ServiceCheck,
    gsi: ServiceCheck,
    convex: ServiceCheck,
    opendota: ServiceCheck,
    deadlock: ServiceCheck,
}

async fn probe_service(name: &'static str, url: &str) -> ServiceCheck {
    let started = Instant::now();
    let client = match reqwest::Client::builder()
        .timeout(Duration::from_secs(8))
        .user_agent("TheTracker diagnostics")
        .build()
    {
        Ok(client) => client,
        Err(error) => return ServiceCheck { name, ok: false, detail: error.to_string(), latency_ms: None },
    };
    match client.get(url).send().await {
        Ok(response) => ServiceCheck {
            name,
            ok: response.status().is_success() || response.status().is_client_error(),
            detail: format!("HTTP {}", response.status().as_u16()),
            latency_ms: Some(started.elapsed().as_millis()),
        },
        Err(error) => ServiceCheck { name, ok: false, detail: error.to_string(), latency_ms: None },
    }
}

/// "just now", "4 min ago", "3 h ago" — for status lines, not timestamps.
fn ago(secs: u64) -> String {
    match secs {
        0..=59 => "just now".to_string(),
        60..=3599 => format!("{} min ago", secs / 60),
        _ => format!("{} h ago", secs / 3600),
    }
}

/// Takes the app handle rather than borrowed state: an async command that
/// borrows managed state has to return a Result, and this one never fails.
#[tauri::command]
async fn run_diagnostics(app: tauri::AppHandle) -> Diagnostics {
    let accounts = steam::detect();
    let steam = ServiceCheck {
        name: "Steam",
        ok: !accounts.is_empty(),
        detail: if accounts.is_empty() { "No local Steam identity found".into() } else { format!("{} local account(s) found", accounts.len()) },
        latency_ms: None,
    };

    // Whether the config file exists is not the question. It existed the
    // whole time twelve matches went unrecorded, and this check said all
    // was well. What matters is whether Dota is actually posting to us.
    let status = gsi_setup::status();
    let heard = app
        .state::<AppState>()
        .tracker
        .lock()
        .ok()
        .and_then(|t| t.last_payload_at)
        .map(|at| at.elapsed().as_secs());
    let (ok, detail) = match (status.installed, heard) {
        (false, _) => (false, "Configuration not installed".to_string()),
        (true, Some(s)) if s < MATCH_FEED_TIMEOUT.as_secs() => {
            (true, format!("Receiving data from Dota on port {}", status.port))
        }
        (true, Some(s)) => (true, format!("Last heard from Dota {}", ago(s))),
        (true, None) => (
            status.launch_option != Some(false),
            if status.launch_option == Some(false) {
                "Configured, but Dota's launch option is missing, so it is not sending anything".to_string()
            } else {
                "Configured, nothing received yet — expected if Dota is closed; if it is open, Dota is not reaching TheTracker".to_string()
            },
        ),
    };
    let gsi = ServiceCheck { name: "Dota GSI", ok, detail, latency_ms: None };
    let convex_url = convex_sync::convex_url();
    let (convex, opendota, deadlock) = tokio::join!(
        probe_service("Cloud sync", &convex_url),
        probe_service("OpenDota", "https://api.opendota.com/api/constants/heroes"),
        probe_service("Deadlock API", "https://api.deadlock-api.com/v1/assets/heroes"),
    );
    Diagnostics { steam, gsi, convex, opendota, deadlock }
}

#[tauri::command]
fn get_live_state(app_state: tauri::State<AppState>) -> LiveStatus {
    let tracker = app_state.tracker.lock().unwrap();
    let server_error = app_state.server_error.lock().unwrap().clone();
    LiveStatus {
        current: tracker.current.clone(),
        tracking_enabled: tracker.tracking_enabled,
        server_error,
        gsi_age_secs: tracker.last_payload_at.map(|at| at.elapsed().as_secs()),
    }
}

// ---------- Running in the background ----------

#[derive(Serialize)]
struct BackgroundSettings {
    #[serde(rename = "startWithWindows")]
    start_with_windows: bool,
    #[serde(rename = "closeToTray")]
    close_to_tray: bool,
    #[serde(rename = "autostartAsked")]
    autostart_asked: bool,
    /// False if the tray icon could not be created, in which case closing
    /// the window quits regardless of the setting.
    #[serde(rename = "trayAvailable")]
    tray_available: bool,
}

fn background_settings_of(app: &tauri::AppHandle) -> BackgroundSettings {
    let general = prefs::load().general;
    BackgroundSettings {
        // The registry is the source of truth, not a copy in prefs: the
        // player can remove the entry from Task Manager's Startup tab, and
        // the app should not go on claiming it is set.
        start_with_windows: app.autolaunch().is_enabled().unwrap_or(false),
        close_to_tray: general.close_to_tray,
        autostart_asked: general.autostart_asked,
        tray_available: TRAY_READY.load(Ordering::SeqCst),
    }
}

#[tauri::command]
fn background_settings(app: tauri::AppHandle) -> BackgroundSettings {
    background_settings_of(&app)
}

#[tauri::command]
fn set_start_with_windows(enabled: bool, app: tauri::AppHandle) -> Result<BackgroundSettings, String> {
    let launcher = app.autolaunch();
    let result = if enabled { launcher.enable() } else { launcher.disable() };
    result.map_err(|e| format!("Couldn't change the startup setting: {e}"))?;
    // Answering either way counts as having been asked.
    let mut general = prefs::load().general;
    general.autostart_asked = true;
    prefs::save_general(general);
    Ok(background_settings_of(&app))
}

#[tauri::command]
fn set_close_to_tray(enabled: bool, app: tauri::AppHandle) -> BackgroundSettings {
    let mut general = prefs::load().general;
    general.close_to_tray = enabled;
    prefs::save_general(general);
    background_settings_of(&app)
}

/// "Not now" on the startup prompt — record it so the question is not asked
/// again, without changing anything.
#[tauri::command]
fn dismiss_autostart_prompt(app: tauri::AppHandle) -> BackgroundSettings {
    let mut general = prefs::load().general;
    general.autostart_asked = true;
    prefs::save_general(general);
    background_settings_of(&app)
}

fn show_main_window(app: &tauri::AppHandle) {
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.unminimize();
        let _ = window.show();
        let _ = window.set_focus();
    }
}

fn build_tray(app: &tauri::AppHandle) -> tauri::Result<TrayIcon> {
    let open = MenuItem::with_id(app, "open", "Open TheTracker", true, None::<&str>)?;
    let quit = MenuItem::with_id(app, "quit", "Quit TheTracker", true, None::<&str>)?;
    let menu = Menu::with_items(app, &[&open, &quit])?;

    let mut builder = TrayIconBuilder::with_id("main")
        .tooltip("TheTracker — tracking in the background")
        .menu(&menu)
        // Left click opens the app; the menu is on right click, the way
        // every other tray app on Windows behaves.
        .show_menu_on_left_click(false)
        .on_menu_event(|app, event| match event.id().as_ref() {
            "open" => show_main_window(app),
            "quit" => app.exit(0),
            _ => {}
        })
        .on_tray_icon_event(|tray, event| {
            if let TrayIconEvent::Click { button: MouseButton::Left, button_state: MouseButtonState::Up, .. } = event {
                show_main_window(tray.app_handle());
            }
        });
    if let Some(icon) = app.default_window_icon() {
        builder = builder.icon(icon.clone());
    }
    builder.build(app)
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

#[derive(Serialize)]
struct MonitorInfo {
    name: String,
    width: u32,
    height: u32,
    primary: bool,
}

/// The displays the overlay can be pinned to.
#[tauri::command]
fn list_monitors(app: tauri::AppHandle) -> Vec<MonitorInfo> {
    let primary = app.primary_monitor().ok().flatten().and_then(|m| m.name().cloned());

    app.available_monitors()
        .map(|list| {
            list.into_iter()
                .map(|m| {
                    let name = m.name().cloned().unwrap_or_default();
                    MonitorInfo {
                        primary: Some(&name) == primary.as_ref(),
                        width: m.size().width,
                        height: m.size().height,
                        name,
                    }
                })
                .collect()
        })
        .unwrap_or_default()
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

/// Opens the overlay when a match starts and closes it when the match ends.
///
/// Polled rather than event-driven because the tracker state is already
/// polled by the UI; one more cheap read every two seconds is far simpler
/// than threading a callback through the GSI listener.
///
/// Only acts on transitions, so a player who closes the overlay by hand
/// mid-match does not get it forced back open a second later.
fn spawn_overlay_watcher(app: tauri::AppHandle, tracker: Arc<Mutex<Tracker>>) {
    std::thread::spawn(move || {
        let mut was_live = false;
        loop {
            std::thread::sleep(std::time::Duration::from_secs(2));

            // A match is live only while Dota is still reporting on it.
            // Leaving a game early sends the player to the menu, whose
            // payloads the tracker ignores — so `in_progress` stayed true
            // from the last in-game update and the overlay stayed over the
            // desktop until the next match started.
            let live = tracker
                .lock()
                .ok()
                .map(|t| {
                    let in_match = t.current.as_ref().map(|m| !m.ended && m.in_progress).unwrap_or(false);
                    let still_reporting =
                        t.last_match_payload_at.map(|at| at.elapsed() < MATCH_FEED_TIMEOUT).unwrap_or(false);
                    in_match && still_reporting
                })
                .unwrap_or(false);

            if live == was_live {
                continue;
            }
            was_live = live;

            if !prefs::load().overlay.auto {
                continue;
            }

            let _ = if live { overlay::show(&app) } else { overlay::hide(&app) };
        }
    });
}

/// Periodically asks OpenDota to name the game types GSI cannot.
///
/// Valve's GSI feed carries no game mode or lobby type — the map block has
/// the clock, the game state and the match id, and nothing else — so a match
/// tracked live lands in history as "unspecified" and used to need tagging by
/// hand. OpenDota knows, but only once it has ingested the finished game,
/// which takes a few minutes; hence a retry loop rather than one lookup.
///
/// It only fills blanks. A type the player chose themselves is never
/// overwritten, and when nothing is untagged the call returns without
/// touching the network.
fn spawn_game_type_backfill() {
    tauri::async_runtime::spawn(async move {
        loop {
            // A short first delay keeps startup free of network work.
            tokio::time::sleep(std::time::Duration::from_secs(45)).await;
            let _ = dota_api::backfill_game_types().await;
            tokio::time::sleep(std::time::Duration::from_secs(300)).await;
        }
    });
}


// ---------- Meta ----------

/// Which heroes are strong right now, and which way they are moving.
#[tauri::command]
async fn dota_meta(app_state: tauri::State<'_, AppState>) -> Result<meta::DotaMeta, String> {
    let cache = app_state.meta.clone();
    meta::dota(&cache).await
}

#[tauri::command]
async fn deadlock_meta(app_state: tauri::State<'_, AppState>) -> Result<meta::DeadlockMeta, String> {
    let cache = app_state.meta.clone();
    let heroes = app_state.heroes.clone();
    meta::deadlock(&cache, &heroes).await
}


// ---------- GSI setup ----------

#[tauri::command]
fn gsi_status() -> gsi_setup::GsiStatus {
    gsi_setup::status()
}

#[tauri::command]
fn gsi_install() -> Result<Vec<String>, String> {
    gsi_setup::install()
}

#[tauri::command]
fn gsi_remove() -> Result<Vec<String>, String> {
    gsi_setup::remove()
}

/// Starts Dota with the GSI flag, so nothing has to be configured in Steam.
#[tauri::command]
fn launch_dota() -> Result<(), String> {
    gsi_setup::launch_dota()
}

fn main() {
    // Must run before anything reads history/profile files.
    storage::migrate_legacy_dir();

    // Write Dota's GSI config if it is missing or stale. Asking players to
    // copy a file by hand meant it simply never happened, and the Live tab
    // sat empty with nothing explaining why.
    if let Some(written) = gsi_setup::ensure_installed() {
        for path in written {
            eprintln!("GSI config installed: {path}");
        }
    }
    for path in gsi_setup::remove_legacy_configs() {
        eprintln!("Removed the old tracker's GSI config: {path}");
    }

    let tracker = Arc::new(Mutex::new(Tracker::new()));
    let server_error: Arc<Mutex<Option<String>>> = Arc::new(Mutex::new(None));

    let tracker_for_setup = tracker.clone();
    tauri::Builder::default()
        // First, so a second launch is handed to the running copy before
        // anything else in it starts. Without it a second copy failed to
        // bind the GSI port, showed an error, and both wrote history.json.
        .plugin(tauri_plugin_single_instance::init(|app, _argv, _cwd| {
            show_main_window(app);
        }))
        .plugin(tauri_plugin_autostart::init(MacosLauncher::LaunchAgent, Some(vec![MINIMIZED_ARG])))
        .plugin(tauri_plugin_updater::Builder::new().build())
        // Closing the main window either hides it to the tray or quits —
        // explicitly. Left to the default, the hidden overlay window would
        // keep the process alive with no window, no tray icon and the GSI
        // port still held.
        .on_window_event(|window, event| {
            if window.label() != "main" {
                return;
            }
            if let tauri::WindowEvent::CloseRequested { api, .. } = event {
                if TRAY_READY.load(Ordering::SeqCst) && prefs::load().general.close_to_tray {
                    api.prevent_close();
                    let _ = window.hide();
                } else {
                    window.app_handle().exit(0);
                }
            }
        })
        .setup(move |app| {
            // The sync worker runs on Tauri's async runtime, so it can only
            // start once the app is being set up — not before the builder.
            let auth: SharedAuth = Arc::new(Mutex::new(AuthState::default()));
            // Restores a previous session from the stored refresh token.
            auth::restore(auth.clone());

            let syncer = convex_sync::spawn(device_id::device_id(), auth.clone());
            tracker_for_setup.lock().unwrap().syncer = Some(syncer.clone());

            // Managed state has to exist before anything can call a command,
            // and creating a window counts: the overlay's page starts polling
            // get_live_state the moment it loads, and the main window is
            // already loading by the time setup runs. Registering last left a
            // gap where an early call failed with "state not managed" — which
            // the main window hit as a hard boot failure whenever the page
            // won the race, and which the overlay hid by simply retrying a
            // second later.
            app.manage(AppState {
                tracker: tracker_for_setup.clone(),
                server_error: server_error.clone(),
                syncer,
                auth,
                heroes: Arc::new(Mutex::new(deadlock::HeroCache::default())),
                dota_heroes: Arc::new(Mutex::new(dota_api::DotaHeroCache::default())),
                items: Arc::new(Mutex::new(popular::ItemNameCache::default())),
                meta: Arc::new(Mutex::new(meta::MetaCache::default())),
            });

            // Started here rather than at the top of main() so the
            // single-instance check runs first: a second launch now exits
            // before it ever tries for the port.
            {
                let tracker_for_server = tracker_for_setup.clone();
                let server_error_for_server = server_error.clone();
                gsi::spawn_server(tracker_for_server, move |status| {
                    if let gsi::ServerStatus::Failed(msg) = status {
                        *server_error_for_server.lock().unwrap() = Some(msg);
                    }
                });
            }

            // Build the overlay once, hidden. Creating it on demand raced
            // and could produce two stacked windows.
            let _ = overlay::ensure(&app.handle().clone());

            // Never fatal: a missing tray costs the app its background mode,
            // not its ability to start. Without one, closing quits.
            match build_tray(app.handle()) {
                Ok(tray) => {
                    app.manage(TrayState(tray));
                    TRAY_READY.store(true, Ordering::SeqCst);
                }
                Err(e) => eprintln!("Tray icon unavailable, closing the window will quit: {e}"),
            }

            // The window is created hidden (tauri.conf.json) and shown here,
            // so a login launch goes straight to the tray without flashing a
            // window up first. A launch without a working tray is always
            // shown: hidden with no tray would be unreachable.
            let minimized = std::env::args().any(|a| a == MINIMIZED_ARG);
            if !minimized || !TRAY_READY.load(Ordering::SeqCst) {
                show_main_window(app.handle());
            }

            // Diagnostic: report window labels and which monitor each is on.
            if std::env::var("THETRACKER_WINDOW_DEBUG").is_ok() {
                let h = app.handle().clone();
                std::thread::spawn(move || {
                    std::thread::sleep(std::time::Duration::from_secs(3));
                    for (label, w) in h.webview_windows() {
                        let mon = w.current_monitor().ok().flatten();
                        let name = mon.as_ref().and_then(|m| m.name().cloned()).unwrap_or_default();
                        let pos = mon.as_ref().map(|m| format!("{:?}", m.position())).unwrap_or_default();
                        let size = mon.as_ref().map(|m| format!("{:?}", m.size())).unwrap_or_default();
                        eprintln!("WINDOW_DEBUG label={label} monitor='{name}' origin={pos} size={size}");
                    }
                });
            }

            spawn_overlay_watcher(app.handle().clone(), tracker_for_setup.clone());
            spawn_game_type_backfill();
            Ok(())
        })
        .invoke_handler(tauri::generate_handler![
            get_live_state,
            background_settings,
            set_start_with_windows,
            set_close_to_tray,
            dismiss_autostart_prompt,
            run_diagnostics,
            gsi_status,
            gsi_install,
            gsi_remove,
            launch_dota,
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
            list_monitors,
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
            dota_meta,
            deadlock_meta,
        ])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
