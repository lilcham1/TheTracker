//! Data structures for match state, history and profile.
//!
//! Field names are chosen to match the JSON shape the original Node/Electron
//! tracker used (`history.json` / `profile.json`), so an existing history
//! file from that app can be dropped in and read straight away.

use std::collections::BTreeMap;

use serde::{Deserialize, Serialize};

pub const CHECKPOINT_MINUTES: [u32; 5] = [5, 10, 15, 20, 25];

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Death {
    pub clock: String,
    #[serde(rename = "goldLost")]
    pub gold_lost: Option<i64>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct KeyItemEntry {
    pub clock: String,
    pub item: String,
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize)]
pub struct Checkpoint {
    #[serde(rename = "lastHits")]
    pub last_hits: i64,
    pub denies: i64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RoshanState {
    pub deaths: u32,
    #[serde(rename = "lastDeathClock")]
    pub last_death_clock: Option<f64>,
    #[serde(rename = "wasAlive")]
    pub was_alive: bool,
}

impl Default for RoshanState {
    fn default() -> Self {
        RoshanState { deaths: 0, last_death_clock: None, was_alive: true }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CompareMetric {
    pub value: Option<f64>,
    pub avg: Option<f64>,
    pub verdict: String, // "better" | "worse" | "similar" | "no_data"
    #[serde(rename = "isBest")]
    pub is_best: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Comparison {
    pub deaths: CompareMetric,
    #[serde(rename = "goldLost")]
    pub gold_lost: CompareMetric,
    pub checkpoints: BTreeMap<u32, CompareMetric>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MatchSummary {
    pub matchid: String,
    #[serde(rename = "heroName")]
    pub hero_name: Option<String>,
    pub date: String,
    pub duration: String,
    pub kills: i64,
    #[serde(rename = "totalDeaths")]
    pub total_deaths: usize,
    #[serde(rename = "totalGoldLost")]
    pub total_gold_lost: i64,
    pub deaths: Vec<Death>,
    #[serde(rename = "keyItems")]
    pub key_items: Vec<KeyItemEntry>,
    pub checkpoints: BTreeMap<u32, Option<Checkpoint>>,
    #[serde(rename = "roshanDeaths")]
    pub roshan_deaths: u32,
    #[serde(rename = "gameType")]
    pub game_type: String,
    pub comparison: Option<Comparison>,
    #[serde(rename = "gamesComparedAgainst")]
    pub games_compared_against: Option<usize>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MatchState {
    pub matchid: String,
    #[serde(rename = "heroName")]
    pub hero_name: Option<String>,
    #[serde(rename = "startedAt")]
    pub started_at: String,
    #[serde(rename = "wasAlive")]
    pub was_alive: bool,
    #[serde(rename = "ownedItemCounts")]
    pub owned_item_counts: BTreeMap<String, i64>,
    pub deaths: Vec<Death>,
    #[serde(rename = "keyItemLog")]
    pub key_item_log: Vec<KeyItemEntry>,
    pub checkpoints: BTreeMap<u32, Option<Checkpoint>>,
    #[serde(rename = "lastClockTime")]
    pub last_clock_time: f64,
    /// Day/night, straight from GSI. The overlay shows the flip countdown,
    /// and computing it from the clock alone would drift after a pause.
    #[serde(default)]
    pub daytime: Option<bool>,
    #[serde(rename = "lastHits")]
    pub last_hits: i64,
    pub denies: i64,
    pub kills: i64,
    #[serde(rename = "prevGold")]
    pub prev_gold: Option<i64>,
    pub ended: bool,
    pub summary: Option<MatchSummary>,
    #[serde(rename = "gameType")]
    pub game_type: String,
    pub roshan: RoshanState,
}

impl MatchState {
    pub fn new(matchid: String, hero_name_raw: Option<String>) -> Self {
        let mut checkpoints = BTreeMap::new();
        for m in CHECKPOINT_MINUTES {
            checkpoints.insert(m, None);
        }
        MatchState {
            matchid,
            hero_name: hero_name_raw,
            started_at: chrono::Local::now().to_rfc3339(),
            was_alive: true,
            owned_item_counts: BTreeMap::new(),
            deaths: Vec::new(),
            key_item_log: Vec::new(),
            checkpoints,
            last_clock_time: 0.0,
            daytime: None,
            last_hits: 0,
            denies: 0,
            kills: 0,
            prev_gold: None,
            ended: false,
            summary: None,
            game_type: "unspecified".to_string(),
            roshan: RoshanState::default(),
        }
    }

    pub fn total_gold_lost(&self) -> i64 {
        self.deaths.iter().filter_map(|d| d.gold_lost).sum()
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Profile {
    pub username: String,
    pub rank: Option<String>,
    pub role: Option<String>,
}

impl Default for Profile {
    fn default() -> Self {
        Profile { username: String::new(), rank: None, role: None }
    }
}

pub const GAME_TYPES: [&str; 4] = ["ranked", "all_pick", "turbo", "other"];

pub fn game_type_label(t: &str) -> &'static str {
    match t {
        "ranked" => "Ranked",
        "all_pick" => "All Pick",
        "turbo" => "Turbo",
        "other" => "Other",
        // Old egui-version saves used "unranked" for this category; keep it
        // mapped to the same label so any pre-existing history still reads
        // sensibly instead of falling through to "Unspecified".
        "unranked" => "All Pick",
        _ => "Unspecified",
    }
}

// Rank/role display metadata (labels and colors) lives in the frontend —
// see RANKS/ROLES in `ui/app.js`. The backend only stores the raw id
// strings the user picked, in profile.json.
