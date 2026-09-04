//! Convex sync: pushes finished matches and the profile up to a Convex
//! deployment, and reads the cross-player leaderboard back down.
//!
//! Local `history.json` stays the source of truth — every write hits disk
//! first (see `state::finalize_match`), and syncing happens afterwards on a
//! background task. If the network is down, Convex is unreachable, or the
//! deployment is paused, the tracker keeps working exactly as before and
//! the failure is surfaced as a status string rather than lost data. That
//! matters here: this thing runs during live games.

use std::collections::BTreeMap;
use std::sync::{Arc, Mutex};

use convex::{ConvexClient, FunctionResult, Value};
use serde::Serialize;
use tokio::sync::mpsc::{unbounded_channel, UnboundedSender};

use crate::model::{MatchSummary, Profile};
use crate::storage;

/// Production deployment for this app. Override at runtime with
/// `DOTA_TRACKER_CONVEX_URL` (handy for pointing at the dev deployment).
pub const DEFAULT_CONVEX_URL: &str = "https://calculating-seahorse-132.convex.cloud";

pub fn convex_url() -> String {
    std::env::var("DOTA_TRACKER_CONVEX_URL").unwrap_or_else(|_| DEFAULT_CONVEX_URL.to_string())
}

#[derive(Debug, Clone, Default, Serialize)]
pub struct SyncStatus {
    pub connected: bool,
    /// Jobs handed to the worker but not yet confirmed.
    pub pending: usize,
    /// Matches successfully pushed this session.
    pub synced: usize,
    #[serde(rename = "lastError")]
    pub last_error: Option<String>,
    #[serde(rename = "lastSync")]
    pub last_sync: Option<String>,
}

pub enum SyncJob {
    Match(Box<MatchSummary>),
    Matches(Vec<MatchSummary>),
    Profile(Box<Profile>),
}

#[derive(Clone)]
pub struct Syncer {
    tx: UnboundedSender<SyncJob>,
    pub status: Arc<Mutex<SyncStatus>>,
    pub device_id: String,
}

impl Syncer {
    /// Queues a job. Never blocks and never fails loudly — sync is
    /// best-effort by design, and the local files already hold the data.
    pub fn send(&self, job: SyncJob) {
        let count = match &job {
            SyncJob::Matches(v) => v.len(),
            _ => 1,
        };
        if self.tx.send(job).is_ok() {
            if let Ok(mut s) = self.status.lock() {
                s.pending += count;
            }
        }
    }
}

/// Starts the background sync worker on Tauri's async runtime and returns a
/// handle for queueing work.
pub fn spawn(device_id: String) -> Syncer {
    let (tx, mut rx) = unbounded_channel::<SyncJob>();
    let status = Arc::new(Mutex::new(SyncStatus::default()));

    let worker_status = status.clone();
    let worker_device = device_id.clone();
    tauri::async_runtime::spawn(async move {
        let mut client: Option<ConvexClient> = None;

        while let Some(job) = rx.recv().await {
            // Connect lazily, and reconnect if a previous attempt failed —
            // so the app starting up offline isn't a permanent condition.
            if client.is_none() {
                match ConvexClient::new(&convex_url()).await {
                    Ok(c) => {
                        client = Some(c);
                        set_status(&worker_status, |s| {
                            s.connected = true;
                            s.last_error = None;
                        });
                    }
                    Err(e) => {
                        let msg = format!("Couldn't reach Convex: {e}");
                        set_status(&worker_status, |s| {
                            s.connected = false;
                            s.last_error = Some(msg.clone());
                            s.pending = s.pending.saturating_sub(1);
                        });
                        continue;
                    }
                }
            }
            let c = client.as_mut().unwrap();
            let username = storage::load_profile().username;

            let result = match job {
                SyncJob::Match(m) => push_match(c, &worker_device, &username, &m).await.map(|_| 1),
                SyncJob::Matches(list) => {
                    let mut ok = 0usize;
                    let mut err = None;
                    for m in &list {
                        match push_match(c, &worker_device, &username, m).await {
                            Ok(()) => ok += 1,
                            Err(e) => {
                                err = Some(e);
                                break;
                            }
                        }
                    }
                    match err {
                        Some(e) => Err(e),
                        None => Ok(ok),
                    }
                }
                SyncJob::Profile(p) => push_profile(c, &worker_device, &p).await.map(|_| 0),
            };

            match result {
                Ok(n) => set_status(&worker_status, |s| {
                    s.connected = true;
                    s.synced += n;
                    s.pending = s.pending.saturating_sub(n.max(1));
                    s.last_error = None;
                    s.last_sync = Some(chrono::Local::now().format("%H:%M:%S").to_string());
                }),
                Err(e) => {
                    // Drop the client so the next job reconnects from scratch.
                    client = None;
                    set_status(&worker_status, |s| {
                        s.connected = false;
                        s.pending = s.pending.saturating_sub(1);
                        s.last_error = Some(e);
                    });
                }
            }
        }
    });

    Syncer { tx, status, device_id }
}

fn set_status(status: &Arc<Mutex<SyncStatus>>, f: impl FnOnce(&mut SyncStatus)) {
    if let Ok(mut s) = status.lock() {
        f(&mut s);
    }
}

// ---------- Mutations ----------

async fn push_match(
    client: &mut ConvexClient,
    device_id: &str,
    username: &str,
    m: &MatchSummary,
) -> Result<(), String> {
    let args = match_args(device_id, username, m);
    unwrap_result(client.mutation("matches:upsert", args).await)
}

async fn push_profile(client: &mut ConvexClient, device_id: &str, p: &Profile) -> Result<(), String> {
    let mut args = BTreeMap::new();
    args.insert("deviceId".to_string(), Value::String(device_id.to_string()));
    args.insert("username".to_string(), Value::String(p.username.clone()));
    args.insert("rank".to_string(), opt_string(p.rank.clone()));
    args.insert("role".to_string(), opt_string(p.role.clone()));
    unwrap_result(client.mutation("profiles:upsert", args).await)
}

// ---------- Queries ----------

/// Cross-player leaderboard. Takes its own short-lived client: it's called
/// on a tab switch, not in a hot path.
pub async fn global_leaderboard(
    metric: &str,
    game_type: &str,
    limit: f64,
) -> Result<serde_json::Value, String> {
    let mut client = ConvexClient::new(&convex_url())
        .await
        .map_err(|e| format!("Couldn't reach Convex: {e}"))?;
    let mut args = BTreeMap::new();
    args.insert("metric".to_string(), Value::String(metric.to_string()));
    args.insert("gameType".to_string(), Value::String(game_type.to_string()));
    args.insert("limit".to_string(), Value::Float64(limit));

    match client.query("leaderboard:globalTop", args).await {
        Ok(FunctionResult::Value(v)) => Ok(serde_json::Value::from(v)),
        Ok(FunctionResult::ErrorMessage(e)) => Err(e),
        Ok(FunctionResult::ConvexError(e)) => Err(format!("{e:?}")),
        Err(e) => Err(e.to_string()),
    }
}

/// Everything this install has previously synced — used to restore history
/// onto a fresh machine.
pub async fn cloud_history(device_id: &str) -> Result<serde_json::Value, String> {
    let mut client = ConvexClient::new(&convex_url())
        .await
        .map_err(|e| format!("Couldn't reach Convex: {e}"))?;
    let mut args = BTreeMap::new();
    args.insert("deviceId".to_string(), Value::String(device_id.to_string()));
    match client.query("matches:listForDevice", args).await {
        Ok(FunctionResult::Value(v)) => Ok(serde_json::Value::from(v)),
        Ok(FunctionResult::ErrorMessage(e)) => Err(e),
        Ok(FunctionResult::ConvexError(e)) => Err(format!("{e:?}")),
        Err(e) => Err(e.to_string()),
    }
}

fn unwrap_result(res: anyhow::Result<FunctionResult>) -> Result<(), String> {
    match res {
        Ok(FunctionResult::Value(_)) => Ok(()),
        Ok(FunctionResult::ErrorMessage(e)) => Err(e),
        Ok(FunctionResult::ConvexError(e)) => Err(format!("{e:?}")),
        Err(e) => Err(e.to_string()),
    }
}

// ---------- Argument building ----------
//
// Built field by field rather than by serializing MatchSummary wholesale:
// Convex validators reject unknown arguments, and `comparison` /
// `gamesComparedAgainst` are local-only (they're recomputed from whatever
// peer group the local history has). Every number goes up as Float64 —
// Convex's Int64 arrives in JS as a BigInt, which `v.number()` rejects.

fn opt_string(v: Option<String>) -> Value {
    match v {
        Some(s) => Value::String(s),
        None => Value::Null,
    }
}

fn opt_num(v: Option<f64>) -> Value {
    match v {
        Some(n) => Value::Float64(n),
        None => Value::Null,
    }
}

fn match_args(device_id: &str, username: &str, m: &MatchSummary) -> BTreeMap<String, Value> {
    let checkpoints: BTreeMap<String, Value> = m
        .checkpoints
        .iter()
        .map(|(minute, cp)| {
            let val = match cp {
                Some(c) => Value::Object(BTreeMap::from([
                    ("lastHits".to_string(), Value::Float64(c.last_hits as f64)),
                    ("denies".to_string(), Value::Float64(c.denies as f64)),
                ])),
                None => Value::Null,
            };
            (minute.to_string(), val)
        })
        .collect();

    let deaths: Vec<Value> = m
        .deaths
        .iter()
        .map(|d| {
            Value::Object(BTreeMap::from([
                ("clock".to_string(), Value::String(d.clock.clone())),
                ("goldLost".to_string(), opt_num(d.gold_lost.map(|g| g as f64))),
            ]))
        })
        .collect();

    let key_items: Vec<Value> = m
        .key_items
        .iter()
        .map(|k| {
            Value::Object(BTreeMap::from([
                ("clock".to_string(), Value::String(k.clock.clone())),
                ("item".to_string(), Value::String(k.item.clone())),
            ]))
        })
        .collect();

    let last_hits_25 = m.checkpoints.get(&25).and_then(|c| c.map(|cc| cc.last_hits as f64));

    BTreeMap::from([
        ("deviceId".to_string(), Value::String(device_id.to_string())),
        ("username".to_string(), Value::String(username.to_string())),
        ("matchid".to_string(), Value::String(m.matchid.clone())),
        ("heroName".to_string(), opt_string(m.hero_name.clone())),
        ("date".to_string(), Value::String(m.date.clone())),
        ("duration".to_string(), Value::String(m.duration.clone())),
        ("kills".to_string(), Value::Float64(m.kills as f64)),
        ("totalDeaths".to_string(), Value::Float64(m.total_deaths as f64)),
        ("totalGoldLost".to_string(), Value::Float64(m.total_gold_lost as f64)),
        ("roshanDeaths".to_string(), Value::Float64(m.roshan_deaths as f64)),
        ("gameType".to_string(), Value::String(m.game_type.clone())),
        ("lastHits25".to_string(), opt_num(last_hits_25)),
        ("checkpoints".to_string(), Value::Object(checkpoints)),
        ("deaths".to_string(), Value::Array(deaths)),
        ("keyItems".to_string(), Value::Array(key_items)),
    ])
}
