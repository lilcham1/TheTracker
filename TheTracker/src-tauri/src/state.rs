//! Core tracking logic: turning raw Dota 2 GSI payloads into match state,
//! and finalizing a match into a history entry with historical comparisons.
//! Ported 1:1 from the original tracker's `handleUpdate`/`finalizeMatch`.

use std::collections::BTreeMap;
use std::time::Instant;

use chrono::SecondsFormat;
use serde_json::Value;

use crate::heroes::{self, is_key_item};
use crate::model::{
    Checkpoint, Comparison, CompareMetric, Death, KeyItemEntry, MatchState, MatchSummary,
    CHECKPOINT_MINUTES,
};
use crate::storage;

/// Top-level key the bundled simulator (`test-overlay.ps1`) adds to every
/// payload it posts. Real Dota never sends it.
pub const SIMULATED_MARKER: &str = "thetracker_simulated";

/// A match that ends without Dota reporting the post-game state is kept only
/// if it got at least this far. Anything shorter is almost always a remake or
/// an abandon in the first minutes, and saving it would put a junk row in
/// history.
const MIN_INCOMPLETE_CLOCK: f64 = 5.0 * 60.0;

pub struct Tracker {
    pub current: Option<MatchState>,
    pub tracking_enabled: bool,
    pub log_lines: Vec<String>,
    /// Set once Convex sync is wired up (see main.rs). Stays `None` if sync
    /// is unavailable — the tracker is fully functional without it.
    pub syncer: Option<crate::convex_sync::Syncer>,
    /// When Dota last posted anything at all, menus included.
    ///
    /// The app used to have no record of this, so a missing launch option, a
    /// stale config or simply not having the app open during a game were all
    /// indistinguishable from a quiet afternoon. Twelve matches went
    /// untracked that way before anyone could tell.
    pub last_payload_at: Option<Instant>,
    /// When Dota last posted while the player was actually in a match. Lets
    /// the overlay notice the player has left a game it never saw end.
    pub last_match_payload_at: Option<Instant>,
    /// Off only in tests, so a test match can never reach the real history
    /// file or the cloud.
    pub(crate) persist: bool,
}

impl Tracker {
    pub fn new() -> Self {
        Tracker {
            current: None,
            tracking_enabled: true,
            log_lines: Vec::new(),
            syncer: None,
            last_payload_at: None,
            last_match_payload_at: None,
            persist: true,
        }
    }

    /// Whether a finished match goes to disk and the cloud. Simulator runs
    /// never do, whatever the build.
    fn should_save(&self, m: &MatchState) -> bool {
        self.persist && !m.simulated
    }

    fn log(&mut self, line: String) {
        let ts = chrono::Local::now().format("%H:%M:%S");
        self.log_lines.push(format!("[{ts}] {line}"));
        if self.log_lines.len() > 300 {
            let excess = self.log_lines.len() - 300;
            self.log_lines.drain(0..excess);
        }
    }

    pub fn mark_roshan_death(&mut self, source: &str) {
        let clock_time = self.current.as_ref().map(|m| m.last_clock_time).unwrap_or(0.0);
        if let Some(m) = self.current.as_mut() {
            if m.ended {
                return;
            }
            m.roshan.deaths += 1;
            m.roshan.last_death_clock = Some(clock_time);
            m.roshan.was_alive = false;
            let deaths = m.roshan.deaths;
            self.log(format!(
                "\u{1F409} Roshan death #{deaths} at {} ({source}) \u{2014} drops: {}",
                fmt_clock(Some(clock_time)),
                heroes::roshan_drops(deaths)
            ));
        }
    }

    pub fn set_game_type(&mut self, game_type: &str) {
        if let Some(m) = self.current.as_mut() {
            if !m.ended && crate::model::GAME_TYPES.contains(&game_type) {
                m.game_type = game_type.to_string();
            }
        }
    }

    pub fn handle_update(&mut self, body: &Value) {
        // Recorded before anything else, the tracking toggle included: this
        // answers "is Dota talking to us at all", which is a different
        // question from "are we recording".
        self.last_payload_at = Some(Instant::now());

        if !self.tracking_enabled {
            return;
        }
        let map = body.get("map").cloned().unwrap_or(Value::Null);
        let player = body.get("player").cloned().unwrap_or(Value::Null);
        let hero = body.get("hero").cloned().unwrap_or(Value::Null);
        let items = body.get("items").cloned().unwrap_or(Value::Null);

        if let Some(activity) = player.get("activity").and_then(|v| v.as_str()) {
            if activity != "playing" {
                return;
            }
        }

        let matchid = match json_to_string(map.get("matchid")) {
            Some(id) if id != "0" => id,
            _ => return,
        };
        self.last_match_payload_at = Some(Instant::now());

        let hero_name_raw = hero.get("name").and_then(|v| v.as_str()).map(|s| s.to_string());
        let simulated = body.get(SIMULATED_MARKER).and_then(|v| v.as_bool()).unwrap_or(false);

        let needs_new_match = match &self.current {
            None => true,
            Some(m) => m.matchid != matchid,
        };
        if needs_new_match {
            self.close_previous_match();
            let mut fresh = MatchState::new(matchid.clone(), hero_name_raw.clone());
            fresh.simulated = simulated;
            self.current = Some(fresh);
            self.log(format!(
                "=== New match detected ({matchid}){} ===",
                if simulated { " — simulated, will not be saved" } else { "" }
            ));
        }

        if self.current.as_ref().map(|m| m.ended).unwrap_or(true) {
            return;
        }

        let clock_time = map
            .get("clock_time")
            .and_then(|v| v.as_f64())
            .unwrap_or_else(|| self.current.as_ref().unwrap().last_clock_time);

        let mut death_line: Option<String> = None;
        let mut checkpoint_lines: Vec<String> = Vec::new();
        let mut item_lines: Vec<String> = Vec::new();
        let mut roshan_auto = false;

        {
            let m = self.current.as_mut().unwrap();
            m.last_clock_time = clock_time;
            m.last_seen_at = Some(chrono::Utc::now().to_rfc3339_opts(SecondsFormat::Millis, true));
            // Only GAME_IN_PROGRESS means the clock is the real match
            // clock. HERO_SELECTION, STRATEGY_TIME and PRE_GAME all report a
            // clock too, and timers built on those are nonsense.
            m.in_progress = map.get("game_state").and_then(|v| v.as_str())
                == Some("DOTA_GAMERULES_STATE_GAME_IN_PROGRESS");
            if m.in_progress {
                m.reached_game = true;
            }

            // The result. GSI reports the player's side as player.team_name
            // and, once the ancient falls, the winning side as map.win_team
            // ("none" until then). The tracker used to assume GSI never says
            // who won, so every locally recorded match — including every
            // Turbo game, which OpenDota does not hold for this account — had
            // no result at all. Read in this order because the winning
            // payload can carry both.
            if let Some(team) = player.get("team_name").and_then(|v| v.as_str()) {
                let team = team.to_ascii_lowercase();
                if team == "radiant" || team == "dire" {
                    m.team = Some(team);
                }
            }
            if let Some(winner) = map.get("win_team").and_then(|v| v.as_str()) {
                let winner = winner.to_ascii_lowercase();
                if winner == "radiant" || winner == "dire" {
                    if let Some(team) = &m.team {
                        m.won = Some(*team == winner);
                    }
                }
            }
            if let Some(assists) = player.get("assists").and_then(|v| v.as_i64()) {
                m.assists = Some(assists);
            }
            if let Some(gpm) = player.get("gpm").and_then(|v| v.as_i64()) {
                m.gpm = Some(gpm);
            }
            if let Some(xpm) = player.get("xpm").and_then(|v| v.as_i64()) {
                m.xpm = Some(xpm);
            }

            if let Some(day) = map.get("daytime").and_then(|v| v.as_bool()) {
                m.daytime = Some(day);
            }
            if let Some(name) = &hero_name_raw {
                m.hero_name = Some(name.clone());
            }
            if let Some(kills) = player.get("kills").and_then(|v| v.as_i64()) {
                m.kills = kills;
            }
            let gold = player.get("gold").and_then(|v| v.as_i64());

            if let Some(alive) = hero.get("alive").and_then(|v| v.as_bool()) {
                if m.was_alive && !alive {
                    let gold_lost = match (gold, m.prev_gold) {
                        (Some(g), Some(pg)) => Some((pg - g).max(0)),
                        _ => None,
                    };
                    let clock_str = fmt_clock(Some(clock_time));
                    death_line = Some(format!(
                        "\u{1F480} Death at {clock_str} \u{2014} lost {}",
                        gold_lost.map(|g| format!("{g}g")).unwrap_or_else(|| "?g".to_string())
                    ));
                    m.deaths.push(Death { clock: clock_str, gold_lost });
                }
                m.was_alive = alive;
            }
            if let Some(g) = gold {
                m.prev_gold = Some(g);
            }

            if let Some(lh) = player.get("last_hits").and_then(|v| v.as_i64()) {
                m.last_hits = lh;
            }
            if let Some(dn) = player.get("denies").and_then(|v| v.as_i64()) {
                m.denies = dn;
            }
            for minute in CHECKPOINT_MINUTES {
                let slot = m.checkpoints.entry(minute).or_insert(None);
                if clock_time >= (minute as f64) * 60.0 && slot.is_none() {
                    *slot = Some(Checkpoint { last_hits: m.last_hits, denies: m.denies });
                    checkpoint_lines.push(format!(
                        "\u{23F1}  {minute}min \u{2014} {} LH / {} DN",
                        m.last_hits, m.denies
                    ));
                }
            }

            if let Some(state) = map.get("roshan_state").and_then(|v| v.as_str()) {
                let state = state.to_lowercase();
                if state.contains("dead") && m.roshan.was_alive {
                    roshan_auto = true;
                } else if state.contains("alive") {
                    m.roshan.was_alive = true;
                }
            }

            // Item ownership: total count across inventory/backpack/neutral/teleport slots.
            let mut current_counts: BTreeMap<String, i64> = BTreeMap::new();
            if let Some(obj) = items.as_object() {
                for (slot, item_data) in obj.iter() {
                    if !(slot.starts_with("slot") || slot.starts_with("teleport") || slot.starts_with("neutral")) {
                        continue;
                    }
                    let raw_name = item_data.get("name").and_then(|v| v.as_str());
                    let raw_name = match raw_name {
                        Some(n) if n != "empty" => n,
                        _ => continue,
                    };
                    let clean_name = raw_name.strip_prefix("item_").unwrap_or(raw_name).to_string();
                    *current_counts.entry(clean_name).or_insert(0) += 1;
                }
            }
            for (item_name, &count) in current_counts.iter() {
                if !is_key_item(item_name) {
                    continue;
                }
                let prev_count = *m.owned_item_counts.get(item_name).unwrap_or(&0);
                if count > prev_count {
                    for _ in 0..(count - prev_count) {
                        let clock_str = fmt_clock(Some(clock_time));
                        item_lines.push(format!("\u{2B50} {item_name} at {clock_str}"));
                        m.key_item_log.push(KeyItemEntry { clock: clock_str, item: item_name.clone() });
                    }
                }
            }
            m.owned_item_counts = current_counts;
        }

        if roshan_auto {
            self.mark_roshan_death("auto");
        }
        if let Some(line) = death_line {
            self.log(line);
        }
        for line in checkpoint_lines {
            self.log(line);
        }
        for line in item_lines {
            self.log(line);
        }

        let game_state = map.get("game_state").and_then(|v| v.as_str()).unwrap_or("");
        if game_state == "DOTA_GAMERULES_STATE_POST_GAME"
            && !self.current.as_ref().map(|m| m.ended).unwrap_or(true)
        {
            self.finalize_match(false);
        }
    }

    /// A new match id proves the previous match is over, whether or not Dota
    /// ever said so. Previously it was replaced without a word, so any game
    /// whose post-game state went unseen — the player left early, or quit to
    /// the menu before the ancient fell — vanished from history entirely.
    /// It is kept now if it was actually played, and marked incomplete.
    fn close_previous_match(&mut self) {
        let worth_keeping = self
            .current
            .as_ref()
            .map(|m| !m.ended && m.reached_game && m.last_clock_time >= MIN_INCOMPLETE_CLOCK)
            .unwrap_or(false);
        if worth_keeping {
            self.finalize_match(true);
        }
    }

    fn finalize_match(&mut self, incomplete: bool) {
        let m = match self.current.as_ref() {
            Some(m) => m.clone(),
            None => return,
        };
        let summary = build_summary(&m, incomplete);

        let mut saved = false;
        let finalized = if self.should_save(&m) {
            let mut full_history = storage::load_history();
            // The same match can reach this twice: restart the app on the
            // post-game screen — the in-app updater does exactly that — and
            // the new instance sees POST_GAME for a match the old one already
            // saved. The first record is the complete one; a second would be
            // a near-empty duplicate.
            if let Some(existing) = full_history.iter().find(|h| h.matchid == m.matchid).cloned() {
                Some(existing)
            } else {
                full_history.push(summary);
                // Recomputing the whole history keeps every match's
                // comparison consistent with the others in its (possibly
                // just-changed) peer group, not just the new one.
                recompute_all_comparisons(&mut full_history);
                storage::save_history(&full_history);
                saved = true;
                full_history.last().cloned()
            }
        } else {
            Some(summary)
        };

        // Local disk is already written above; pushing to Convex is
        // best-effort and never blocks the GSI thread.
        if saved {
            if let (Some(syncer), Some(summary)) = (&self.syncer, &finalized) {
                syncer.send(crate::convex_sync::SyncJob::Match(Box::new(summary.clone())));
            }
        }

        let peers_len = finalized.as_ref().and_then(|s| s.games_compared_against).unwrap_or(0);
        if let Some(cur) = self.current.as_mut() {
            cur.ended = true;
            cur.summary = finalized;
        }

        let result = match m.won {
            Some(true) => "won",
            Some(false) => "lost",
            None => "result unknown",
        };
        let how = if m.simulated {
            " \u{2014} simulated, not saved"
        } else if incomplete {
            " \u{2014} saved as incomplete, Dota never reported the end"
        } else {
            ""
        };
        self.log(format!(
            "\u{1F3C1} Match {} ended ({result}){how} \u{2014} {} deaths, {}g lost, {peers_len} past {} games to compare against",
            m.matchid,
            m.deaths.len(),
            m.total_gold_lost(),
            crate::model::game_type_label(&m.game_type)
        ));
    }
}

/// Re-tags a finished match in history with a new game type (e.g. the player
/// correcting Ranked/Turbo/All Pick/Other after the fact, since GSI doesn't
/// always report lobby type reliably) and recomputes every match's
/// comparison so peer-group stats stay consistent. Returns false if the
/// matchid wasn't found or the type isn't a recognized one.
pub fn set_history_game_type(history: &mut Vec<MatchSummary>, matchid: &str, new_type: &str) -> bool {
    if !crate::model::GAME_TYPES.contains(&new_type) {
        return false;
    }
    let Some(entry) = history.iter_mut().find(|h| h.matchid == matchid) else {
        return false;
    };
    entry.game_type = new_type.to_string();
    recompute_all_comparisons(history);
    true
}

/// Recomputes each match's `comparison`/`games_compared_against` against its
/// current peers (same game_type, excluding itself) in the given history.
pub fn recompute_all_comparisons(history: &mut [MatchSummary]) {
    let snapshot = history.to_vec();
    for s in history.iter_mut() {
        let peers: Vec<&MatchSummary> =
            snapshot.iter().filter(|p| p.matchid != s.matchid && p.game_type == s.game_type).collect();
        let (comparison, peer_count) = compute_comparison(s, &peers);
        s.comparison = Some(comparison);
        s.games_compared_against = Some(peer_count);
    }
}

fn compute_comparison(summary: &MatchSummary, peers: &[&MatchSummary]) -> (Comparison, usize) {
    let avg = |get: &dyn Fn(&MatchSummary) -> Option<f64>| -> Option<f64> {
        let vals: Vec<f64> = peers.iter().filter_map(|p| get(p)).collect();
        if vals.is_empty() { None } else { Some(vals.iter().sum::<f64>() / vals.len() as f64) }
    };
    let best = |get: &dyn Fn(&MatchSummary) -> Option<f64>, want_min: bool| -> Option<f64> {
        let vals: Vec<f64> = peers.iter().filter_map(|p| get(p)).collect();
        if vals.is_empty() {
            return None;
        }
        Some(if want_min {
            vals.iter().cloned().fold(f64::INFINITY, f64::min)
        } else {
            vals.iter().cloned().fold(f64::NEG_INFINITY, f64::max)
        })
    };

    let mut deaths_cmp =
        compare_metric(Some(summary.total_deaths as f64), avg(&|p| Some(p.total_deaths as f64)), false);
    if let Some(b) = best(&|p| Some(p.total_deaths as f64), true) {
        deaths_cmp.is_best = (summary.total_deaths as f64) < b;
    }

    let mut gold_cmp =
        compare_metric(Some(summary.total_gold_lost as f64), avg(&|p| Some(p.total_gold_lost as f64)), false);
    if let Some(b) = best(&|p| Some(p.total_gold_lost as f64), true) {
        gold_cmp.is_best = (summary.total_gold_lost as f64) < b;
    }

    let mut checkpoints_cmp: BTreeMap<u32, CompareMetric> = BTreeMap::new();
    for minute in CHECKPOINT_MINUTES {
        let value = summary.checkpoints.get(&minute).and_then(|c| c.map(|cc| cc.last_hits as f64));
        let get_min = move |p: &MatchSummary| -> Option<f64> {
            p.checkpoints.get(&minute).and_then(|c| c.map(|cc| cc.last_hits as f64))
        };
        let avg_val = avg(&get_min);
        let mut comp = compare_metric(value, avg_val, true);
        if let (Some(v), Some(b)) = (value, best(&get_min, false)) {
            comp.is_best = v > b;
        }
        checkpoints_cmp.insert(minute, comp);
    }

    (
        Comparison { deaths: deaths_cmp, gold_lost: gold_cmp, checkpoints: checkpoints_cmp },
        peers.len(),
    )
}

fn build_summary(m: &MatchState, incomplete: bool) -> MatchSummary {
    let now = chrono::Utc::now().to_rfc3339_opts(SecondsFormat::Millis, true);
    // A late-saved match is dated by the last time Dota reported on it. Using
    // the save time would put a game left at 9pm under whenever the next
    // match happened to start, which could be the following day.
    let date = if incomplete { m.last_seen_at.clone().unwrap_or(now) } else { now };
    MatchSummary {
        matchid: m.matchid.clone(),
        hero_name: m.hero_name.clone(),
        date,
        duration: fmt_clock(Some(m.last_clock_time)),
        kills: m.kills,
        total_deaths: m.deaths.len(),
        total_gold_lost: m.total_gold_lost(),
        deaths: m.deaths.clone(),
        key_items: m.key_item_log.clone(),
        checkpoints: m.checkpoints.clone(),
        roshan_deaths: m.roshan.deaths,
        game_type: m.game_type.clone(),
        comparison: None,
        games_compared_against: None,
        won: m.won,
        last_hits: Some(m.last_hits),
        denies: Some(m.denies),
        assists: m.assists,
        gpm: m.gpm,
        xpm: m.xpm,
        incomplete,
    }
}

pub fn compare_metric(value: Option<f64>, avg: Option<f64>, higher_is_better: bool) -> CompareMetric {
    let (value, avg) = match (value, avg) {
        (Some(v), Some(a)) => (v, a),
        _ => return CompareMetric { value, avg: None, verdict: "no_data".to_string(), is_best: false },
    };
    let diff = value - avg;
    let threshold = (avg.abs() * 0.08).max(0.5);
    let verdict = if diff.abs() > threshold {
        let better = if higher_is_better { diff > 0.0 } else { diff < 0.0 };
        if better { "better" } else { "worse" }
    } else {
        "similar"
    };
    CompareMetric {
        value: Some(value),
        avg: Some((avg * 10.0).round() / 10.0),
        verdict: verdict.to_string(),
        is_best: false,
    }
}

pub fn fmt_clock(seconds: Option<f64>) -> String {
    match seconds {
        None => "??:??".to_string(),
        Some(s) => {
            let neg = s < 0.0;
            let abs = s.abs().floor() as i64;
            let m = abs / 60;
            let sec = abs % 60;
            format!("{}{}:{:02}", if neg { "-" } else { "" }, m, sec)
        }
    }
}

fn json_to_string(v: Option<&Value>) -> Option<String> {
    match v {
        Some(Value::String(s)) => Some(s.clone()),
        Some(Value::Number(n)) => Some(n.to_string()),
        _ => None,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    const DRAFT: &str = "DOTA_GAMERULES_STATE_HERO_SELECTION";
    const IN_GAME: &str = "DOTA_GAMERULES_STATE_GAME_IN_PROGRESS";
    const POST_GAME: &str = "DOTA_GAMERULES_STATE_POST_GAME";

    /// Every test tracker has persistence off, so nothing here can reach the
    /// real history file or the cloud.
    fn tracker() -> Tracker {
        let mut t = Tracker::new();
        t.persist = false;
        t
    }

    fn payload(matchid: &str, game_state: &str, clock: f64, team: Option<&str>, win_team: &str) -> Value {
        let mut player = json!({
            "activity": "playing",
            "kills": 5, "assists": 9, "last_hits": 120, "denies": 8,
            "gpm": 512, "xpm": 640, "gold": 900,
        });
        if let Some(team) = team {
            player["team_name"] = json!(team);
        }
        json!({
            "map": { "matchid": matchid, "game_state": game_state, "clock_time": clock, "win_team": win_team },
            "player": player,
            "hero": { "name": "npc_dota_hero_kez", "alive": true },
        })
    }

    fn logged(t: &Tracker, needle: &str) -> bool {
        t.log_lines.iter().any(|l| l.contains(needle))
    }

    #[test]
    fn a_win_is_read_from_win_team() {
        let mut t = tracker();
        t.handle_update(&payload("9001", IN_GAME, 600.0, Some("radiant"), "none"));
        assert_eq!(t.current.as_ref().unwrap().won, None, "no result while the game is running");

        t.handle_update(&payload("9001", POST_GAME, 1900.0, Some("radiant"), "radiant"));
        let s = t.current.as_ref().unwrap().summary.clone().expect("match should be finalized");
        assert_eq!(s.won, Some(true));
        assert!(!s.incomplete);
        // The fields a local session needs to stand next to an OpenDota row.
        assert_eq!(s.assists, Some(9));
        assert_eq!(s.gpm, Some(512));
        assert_eq!(s.xpm, Some(640));
        assert_eq!(s.last_hits, Some(120));
    }

    #[test]
    fn a_loss_is_read_from_win_team() {
        let mut t = tracker();
        t.handle_update(&payload("9002", IN_GAME, 600.0, Some("dire"), "none"));
        t.handle_update(&payload("9002", POST_GAME, 1900.0, Some("dire"), "radiant"));
        let s = t.current.as_ref().unwrap().summary.clone().unwrap();
        assert_eq!(s.won, Some(false));
    }

    #[test]
    fn without_a_team_there_is_no_result_rather_than_a_guess() {
        let mut t = tracker();
        t.handle_update(&payload("9003", IN_GAME, 600.0, None, "none"));
        t.handle_update(&payload("9003", POST_GAME, 1900.0, None, "radiant"));
        assert_eq!(t.current.as_ref().unwrap().summary.as_ref().unwrap().won, None);
    }

    #[test]
    fn an_unended_match_is_kept_when_the_next_one_starts() {
        // Twenty-five minutes in, then Dota never reports the end — the
        // player left — and the next match begins.
        let mut t = tracker();
        t.handle_update(&payload("1", IN_GAME, 1500.0, Some("radiant"), "none"));
        t.handle_update(&payload("2", DRAFT, -60.0, Some("dire"), "none"));

        assert!(logged(&t, "Match 1 ended"), "the previous match must not vanish");
        assert!(logged(&t, "incomplete"));
        assert_eq!(t.current.as_ref().unwrap().matchid, "2");
    }

    #[test]
    fn a_match_abandoned_in_the_draft_is_not_kept() {
        let mut t = tracker();
        t.handle_update(&payload("1", DRAFT, -60.0, Some("radiant"), "none"));
        t.handle_update(&payload("2", DRAFT, -60.0, Some("radiant"), "none"));
        assert!(!logged(&t, "Match 1 ended"), "a game that never started is not a match");
    }

    #[test]
    fn a_remake_in_the_first_minutes_is_not_kept() {
        let mut t = tracker();
        t.handle_update(&payload("1", IN_GAME, 120.0, Some("radiant"), "none"));
        t.handle_update(&payload("2", DRAFT, -60.0, Some("radiant"), "none"));
        assert!(!logged(&t, "Match 1 ended"));
    }

    #[test]
    fn simulated_matches_are_never_saved() {
        // Checked on a tracker with persistence ON — the real configuration —
        // because the marker has to win even there.
        let t = Tracker::new();
        let mut fake = MatchState::new("1".into(), None);
        fake.simulated = true;
        assert!(!t.should_save(&fake));
        assert!(t.should_save(&MatchState::new("2".into(), None)));

        // And the marker is actually read off the payload.
        let mut t = tracker();
        let mut body = payload("3", IN_GAME, 300.0, Some("radiant"), "none");
        body[SIMULATED_MARKER] = json!(true);
        t.handle_update(&body);
        assert!(t.current.as_ref().unwrap().simulated);
    }

    #[test]
    fn dota_talking_is_noticed_outside_a_match() {
        // The main menu: Dota is running and posting, but there is no match.
        let mut t = tracker();
        t.handle_update(&json!({ "provider": { "appid": 570 }, "player": { "activity": "menu" } }));
        assert!(t.last_payload_at.is_some(), "this is what proves the setup works");
        assert!(t.last_match_payload_at.is_none());
        assert!(t.current.is_none());
    }

    #[test]
    fn dota_talking_is_noticed_with_tracking_switched_off() {
        let mut t = tracker();
        t.tracking_enabled = false;
        t.handle_update(&payload("1", IN_GAME, 600.0, Some("radiant"), "none"));
        assert!(t.last_payload_at.is_some());
        assert!(t.current.is_none(), "but nothing is recorded");
    }

    #[test]
    fn a_late_saved_match_is_dated_by_its_last_payload() {
        let mut m = MatchState::new("1".into(), None);
        m.last_seen_at = Some("2026-09-19T21:00:00.000Z".into());
        assert_eq!(build_summary(&m, true).date, "2026-09-19T21:00:00.000Z");
        assert_ne!(build_summary(&m, false).date, "2026-09-19T21:00:00.000Z");
    }

    #[test]
    fn history_written_before_these_fields_still_loads() {
        let old = r#"[{"matchid":"1","heroName":null,"date":"2026-09-05T00:00:00Z","duration":"25:00",
            "kills":6,"totalDeaths":0,"totalGoldLost":0,"deaths":[],"keyItems":[],"checkpoints":{},
            "roshanDeaths":0,"gameType":"unspecified","comparison":null,"gamesComparedAgainst":null}]"#;
        let h: Vec<MatchSummary> = serde_json::from_str(old).expect("old history must still parse");
        assert_eq!(h[0].won, None);
        assert!(!h[0].incomplete);
    }
}
