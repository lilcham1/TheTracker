//! Core tracking logic: turning raw Dota 2 GSI payloads into match state,
//! and finalizing a match into a history entry with historical comparisons.
//! Ported 1:1 from the original tracker's `handleUpdate`/`finalizeMatch`.

use std::collections::BTreeMap;

use chrono::SecondsFormat;
use serde_json::Value;

use crate::heroes::{self, is_key_item};
use crate::model::{
    Checkpoint, Comparison, CompareMetric, Death, KeyItemEntry, MatchState, MatchSummary,
    CHECKPOINT_MINUTES,
};
use crate::storage;

pub struct Tracker {
    pub current: Option<MatchState>,
    pub tracking_enabled: bool,
    pub log_lines: Vec<String>,
    /// Set once Convex sync is wired up (see main.rs). Stays `None` if sync
    /// is unavailable — the tracker is fully functional without it.
    pub syncer: Option<crate::convex_sync::Syncer>,
}

impl Tracker {
    pub fn new() -> Self {
        Tracker { current: None, tracking_enabled: true, log_lines: Vec::new(), syncer: None }
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

        let hero_name_raw = hero.get("name").and_then(|v| v.as_str()).map(|s| s.to_string());

        let needs_new_match = match &self.current {
            None => true,
            Some(m) => m.matchid != matchid,
        };
        if needs_new_match {
            self.current = Some(MatchState::new(matchid.clone(), hero_name_raw.clone()));
            self.log(format!("=== New match detected ({matchid}) ==="));
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
            // Only GAME_IN_PROGRESS means the clock is the real match
            // clock. HERO_SELECTION, STRATEGY_TIME and PRE_GAME all report a
            // clock too, and timers built on those are nonsense.
            m.in_progress = map.get("game_state").and_then(|v| v.as_str())
                == Some("DOTA_GAMERULES_STATE_GAME_IN_PROGRESS");

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
            self.finalize_match();
        }
    }

    fn finalize_match(&mut self) {
        let m = match self.current.as_ref() {
            Some(m) => m.clone(),
            None => return,
        };
        let summary = build_summary(&m);
        let mut full_history = storage::load_history();
        full_history.push(summary);
        // Recomputing the whole history keeps every match's comparison
        // consistent with the others in its (possibly just-changed) peer
        // group, not just the new one.
        recompute_all_comparisons(&mut full_history);
        storage::save_history(&full_history);

        let finalized = full_history.last().cloned();
        let peers_len = finalized.as_ref().and_then(|s| s.games_compared_against).unwrap_or(0);

        // Local disk is already written above; pushing to Convex is
        // best-effort and never blocks the GSI thread.
        if let (Some(syncer), Some(summary)) = (&self.syncer, &finalized) {
            syncer.send(crate::convex_sync::SyncJob::Match(Box::new(summary.clone())));
        }

        if let Some(cur) = self.current.as_mut() {
            cur.ended = true;
            cur.summary = finalized;
        }
        self.log(format!(
            "\u{1F3C1} Match ended \u{2014} {} deaths, {}g lost, {peers_len} past {} games to compare against",
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

fn build_summary(m: &MatchState) -> MatchSummary {
    MatchSummary {
        matchid: m.matchid.clone(),
        hero_name: m.hero_name.clone(),
        date: chrono::Utc::now().to_rfc3339_opts(SecondsFormat::Millis, true),
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
