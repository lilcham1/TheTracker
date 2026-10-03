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

    // Everything below is newer than the original history format, so each
    // field defaults when absent and older history.json files still load.
    //
    // None of these are sent to Convex: convex_sync builds the cloud row
    // from an explicit field list, and its validator would reject anything
    // it does not know.
    /// Whether the match was won, from GSI's `map.win_team` compared with
    /// `player.team_name`. `None` for matches recorded before this existed,
    /// and for any match where Dota stopped reporting before the ancient
    /// fell — never guessed.
    #[serde(default)]
    pub won: Option<bool>,
    /// Final values at the end of the match. History only kept last-hit
    /// checkpoints and kills before, which is too little to show a locally
    /// tracked game alongside the OpenDota ones.
    #[serde(rename = "lastHits", default)]
    pub last_hits: Option<i64>,
    #[serde(default)]
    pub denies: Option<i64>,
    #[serde(default)]
    pub assists: Option<i64>,
    #[serde(default)]
    pub gpm: Option<i64>,
    #[serde(default)]
    pub xpm: Option<i64>,
    /// Saved without ever seeing the post-game state: the player left early,
    /// or the next match began before this one reported its end. The stats
    /// stop wherever Dota stopped sending them.
    #[serde(default)]
    pub incomplete: bool,
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
    /// True only once the horn has gone and the game clock is real.
    ///
    /// GSI reports a clock during hero selection and strategy time too, so
    /// anything derived from it — rune spawns, stacks, the day/night flip —
    /// is meaningless until this is true. Without it the overlay counts down
    /// to events during the draft.
    #[serde(rename = "inProgress", default)]
    pub in_progress: bool,
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
    /// "radiant" or "dire", from GSI's `player.team_name`.
    #[serde(default)]
    pub team: Option<String>,
    /// Set once GSI's `map.win_team` names a side, which it does when the
    /// ancient falls.
    #[serde(default)]
    pub won: Option<bool>,
    #[serde(default)]
    pub assists: Option<i64>,
    #[serde(default)]
    pub gpm: Option<i64>,
    #[serde(default)]
    pub xpm: Option<i64>,
    /// True once the horn has gone at least once. `in_progress` drops back
    /// to false at the end of the game; this does not, so it can tell a
    /// match that was actually played from one abandoned in the draft.
    #[serde(rename = "reachedGame", default)]
    pub reached_game: bool,
    /// Wall-clock time of the last payload for this match. A match that is
    /// saved late — because the next one started before this one reported
    /// its end — is dated by this, not by when it happened to be saved.
    #[serde(rename = "lastSeenAt", default)]
    pub last_seen_at: Option<String>,
    /// Payloads from the bundled simulator carry a marker, and a match built
    /// from them is never written to history or synced. Testing the overlay
    /// used to leave fake games in the player's Sessions and Leaderboard.
    #[serde(default)]
    pub simulated: bool,
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
            in_progress: false,
            last_hits: 0,
            denies: 0,
            kills: 0,
            prev_gold: None,
            ended: false,
            summary: None,
            game_type: "unspecified".to_string(),
            roshan: RoshanState::default(),
            team: None,
            won: None,
            assists: None,
            gpm: None,
            xpm: None,
            reached_game: false,
            last_seen_at: None,
            simulated: false,
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
