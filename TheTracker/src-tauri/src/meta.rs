//! Meta: which heroes are strong right now, and which way they are moving.
//!
//! # Inspiration, and the line
//!
//! The shape of these pages is borrowed from dota2protracker and Statlocker
//! — strongest heroes grouped by role, a rating you can sort by, items that
//! are trending, a sense of the patch. That framing is worth learning from.
//!
//! None of the *data* comes from either site. Their numbers are collected
//! and rated by them, and that collection is the product; D2PT's rating in
//! particular is their own model over their own high-MMR sample. Taking it
//! would be lifting the thing they built. So everything below is computed
//! here, from the same public APIs the rest of the app already reads:
//!
//! - **Dota** — OpenDota's `/heroStats`, which reports picks and wins per
//!   hero across public matches, per rank bracket, for pro games, and as a
//!   seven-bucket trend.
//! - **Deadlock** — deadlock-api's `/v1/analytics/hero-stats` and
//!   `/v1/analytics/item-stats`.
//!
//! Where the public data cannot answer something the reference sites show,
//! the app says so rather than inventing it. OpenDota publishes Dota's role
//! *tags* (Carry, Support, Nuker…), not positions one through five, so the
//! grouping here is by tag and is labelled that way.

use std::collections::HashMap;
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use serde::Serialize;

/// Meta moves on the scale of days, so a cached snapshot is fine and keeps
/// the app off the network on every tab switch.
const TTL: Duration = Duration::from_secs(60 * 30);

#[derive(Default)]
pub struct MetaCache {
    dota: Option<(Instant, DotaMeta)>,
    deadlock: Option<(Instant, DeadlockMeta)>,
}

pub type SharedMeta = Arc<Mutex<MetaCache>>;

// ---------- Dota ----------

#[derive(Debug, Clone, Serialize)]
pub struct MetaHero {
    pub id: u32,
    pub name: String,
    /// Slug for Valve's portrait CDN, e.g. "queenofpain".
    pub slug: String,
    pub roles: Vec<String>,
    /// Public matches the hero was picked in, over OpenDota's sample.
    pub picks: u64,
    #[serde(rename = "winRate")]
    pub win_rate: f64,
    /// Share of matches the hero appears in. Ten heroes are picked per
    /// match, so this is picks over (total picks / 10) — the number people
    /// mean by "contest rate", not a share of all picks.
    #[serde(rename = "pickRate")]
    pub pick_rate: f64,
    /// Win-rate change across the sample window, in points. Positive means
    /// the hero has been winning more lately.
    pub trend: f64,
    /// Immortal/Divine bracket win rate, where OpenDota has a sample.
    #[serde(rename = "highWinRate")]
    pub high_win_rate: Option<f64>,
    #[serde(rename = "proPicks")]
    pub pro_picks: u64,
    #[serde(rename = "proBans")]
    pub pro_bans: u64,
    #[serde(rename = "proWinRate")]
    pub pro_win_rate: Option<f64>,
    #[serde(rename = "turboWinRate")]
    pub turbo_win_rate: Option<f64>,
}

#[derive(Debug, Clone, Serialize)]
pub struct DotaMeta {
    /// Public matches behind the sample, derived from total picks.
    pub matches: u64,
    pub heroes: Vec<MetaHero>,
    /// Role tags present, in the order the UI should offer them.
    pub roles: Vec<String>,
}

/// Win rate over a slice of the trend buckets, as a percentage.
fn bucket_win_rate(wins: &[u64], picks: &[u64], from: usize, to: usize) -> Option<f64> {
    let w: u64 = wins.get(from..to)?.iter().sum();
    let p: u64 = picks.get(from..to)?.iter().sum();
    if p == 0 {
        return None;
    }
    Some(w as f64 / p as f64 * 100.0)
}

fn numbers(v: Option<&serde_json::Value>) -> Vec<u64> {
    v.and_then(|x| x.as_array())
        .map(|a| a.iter().filter_map(|n| n.as_u64()).collect())
        .unwrap_or_default()
}

pub async fn dota(cache: &SharedMeta) -> Result<DotaMeta, String> {
    {
        let c = cache.lock().unwrap();
        if let Some((at, ref meta)) = c.dota {
            if at.elapsed() < TTL {
                return Ok(meta.clone());
            }
        }
    }

    let value = crate::dota_api::get_json_public("/heroStats").await?;
    let rows = value.as_array().ok_or("OpenDota returned an unexpected shape")?;

    let total_picks: u64 = rows.iter().filter_map(|r| r.get("pub_pick").and_then(|v| v.as_u64())).sum();
    // Ten heroes are picked per match.
    let matches = (total_picks / 10).max(1);

    let mut heroes: Vec<MetaHero> = rows
        .iter()
        .filter_map(|r| {
            let picks = r.get("pub_pick").and_then(|v| v.as_u64())?;
            if picks == 0 {
                return None;
            }
            let wins = r.get("pub_win").and_then(|v| v.as_u64()).unwrap_or(0);
            let name = r.get("localized_name").and_then(|v| v.as_str())?.to_string();
            let raw = r.get("name").and_then(|v| v.as_str()).unwrap_or_default();
            let slug = raw.strip_prefix("npc_dota_hero_").unwrap_or(raw).to_string();

            let pick_trend = numbers(r.get("pub_pick_trend"));
            let win_trend = numbers(r.get("pub_win_trend"));
            // The final bucket is the week in progress and is always short,
            // so comparing it against the rest would read every hero as
            // falling. It is dropped and the remainder split in half.
            let usable = pick_trend.len().saturating_sub(1);
            let trend = if usable >= 2 && win_trend.len() >= usable {
                let mid = usable / 2;
                match (
                    bucket_win_rate(&win_trend, &pick_trend, 0, mid),
                    bucket_win_rate(&win_trend, &pick_trend, mid, usable),
                ) {
                    (Some(earlier), Some(recent)) => recent - earlier,
                    _ => 0.0,
                }
            } else {
                0.0
            };

            let bracket = |n: &str| -> Option<(u64, u64)> {
                let p = r.get(format!("{n}_pick")).and_then(|v| v.as_u64())?;
                let w = r.get(format!("{n}_win")).and_then(|v| v.as_u64())?;
                (p > 0).then_some((p, w))
            };
            // Bracket 8 is Immortal and is often empty in the snapshot; 7 is
            // Divine and is not. Prefer the higher one that actually has data.
            let high = bracket("8").or_else(|| bracket("7"));

            let pro_picks = r.get("pro_pick").and_then(|v| v.as_u64()).unwrap_or(0);
            let pro_wins = r.get("pro_win").and_then(|v| v.as_u64()).unwrap_or(0);
            let turbo_picks = r.get("turbo_picks").and_then(|v| v.as_u64()).unwrap_or(0);
            let turbo_wins = r.get("turbo_wins").and_then(|v| v.as_u64()).unwrap_or(0);

            Some(MetaHero {
                id: r.get("id").and_then(|v| v.as_u64()).unwrap_or(0) as u32,
                name,
                slug,
                roles: r
                    .get("roles")
                    .and_then(|v| v.as_array())
                    .map(|a| a.iter().filter_map(|s| s.as_str().map(String::from)).collect())
                    .unwrap_or_default(),
                picks,
                win_rate: wins as f64 / picks as f64 * 100.0,
                pick_rate: picks as f64 / matches as f64 * 100.0,
                trend,
                high_win_rate: high.map(|(p, w)| w as f64 / p as f64 * 100.0),
                pro_picks,
                pro_bans: r.get("pro_ban").and_then(|v| v.as_u64()).unwrap_or(0),
                pro_win_rate: (pro_picks > 0).then(|| pro_wins as f64 / pro_picks as f64 * 100.0),
                turbo_win_rate: (turbo_picks > 0).then(|| turbo_wins as f64 / turbo_picks as f64 * 100.0),
            })
        })
        .collect();

    heroes.sort_by(|a, b| b.win_rate.partial_cmp(&a.win_rate).unwrap_or(std::cmp::Ordering::Equal));

    // Role tags, most common first, so the filter row leads with the ones
    // that actually have heroes behind them.
    let mut counts: HashMap<String, usize> = HashMap::new();
    for h in &heroes {
        for r in &h.roles {
            *counts.entry(r.clone()).or_default() += 1;
        }
    }
    let mut roles: Vec<(String, usize)> = counts.into_iter().collect();
    roles.sort_by(|a, b| b.1.cmp(&a.1).then(a.0.cmp(&b.0)));

    let meta = DotaMeta {
        matches,
        heroes,
        roles: roles.into_iter().map(|(r, _)| r).collect(),
    };

    cache.lock().unwrap().dota = Some((Instant::now(), meta.clone()));
    Ok(meta)
}

// ---------- Deadlock ----------

#[derive(Debug, Clone, Serialize)]
pub struct MetaDlHero {
    pub id: u32,
    pub name: String,
    pub image: Option<String>,
    pub matches: u64,
    #[serde(rename = "winRate")]
    pub win_rate: f64,
    #[serde(rename = "pickRate")]
    pub pick_rate: f64,
    pub kda: f64,
    #[serde(rename = "avgSouls")]
    pub avg_souls: u64,
    #[serde(rename = "avgDamage")]
    pub avg_damage: u64,
}

#[derive(Debug, Clone, Serialize)]
pub struct MetaDlItem {
    pub id: u64,
    pub name: String,
    /// Matches the item was bought in. Deliberately a raw count, not a
    /// percentage: the item endpoint covers a different and much larger
    /// sample than the hero endpoint — its busiest item appears in roughly
    /// six times more matches than the hero sample contains — so dividing
    /// one by the other produced "pick rates" over 100%. Rather than invent
    /// a denominator, the count is shown as what it is.
    pub matches: u64,
    #[serde(rename = "winRate")]
    pub win_rate: f64,
    /// How common this item is next to the most-bought one, 0–100. A share
    /// of a known maximum is honest in a way a fabricated pick rate is not.
    pub share: f64,
    /// Average minute the item is bought. Deadlock's own API publishes this
    /// and nothing on the Dota side has an equivalent — when an item is
    /// bought says as much as how often.
    #[serde(rename = "buyMinute")]
    pub buy_minute: Option<f64>,
}

#[derive(Debug, Clone, Serialize)]
pub struct DeadlockMeta {
    pub matches: u64,
    pub heroes: Vec<MetaDlHero>,
    pub items: Vec<MetaDlItem>,
}

pub async fn deadlock(cache: &SharedMeta, hero_cache: &crate::deadlock::SharedHeroes) -> Result<DeadlockMeta, String> {
    {
        let c = cache.lock().unwrap();
        if let Some((at, ref meta)) = c.deadlock {
            if at.elapsed() < TTL {
                return Ok(meta.clone());
            }
        }
    }

    // Four requests, and one of them (the item asset list) is 726 entries.
    // Run sequentially the page sat on "Reading the meta…" for the better
    // part of half a minute, and the asset call timed out often enough that
    // items fell back to showing their numeric ids. Concurrently it is as
    // slow as the slowest one rather than the sum of all four.
    let (hero_rows, names, item_rows, item_names) = tokio::join!(
        crate::deadlock::get_json_public("/v1/analytics/hero-stats"),
        crate::deadlock::heroes(hero_cache),
        crate::deadlock::get_json_public("/v1/analytics/item-stats"),
        crate::popular::deadlock_item_names(),
    );
    let hero_rows = hero_rows?;

    let rows = hero_rows.as_array().ok_or("Deadlock API returned an unexpected shape")?;
    let total: u64 = rows.iter().filter_map(|r| r.get("matches").and_then(|v| v.as_u64())).sum();
    // Twelve players per Deadlock match.
    let matches = (total / 12).max(1);

    let num = |r: &serde_json::Value, k: &str| r.get(k).and_then(|v| v.as_u64()).unwrap_or(0);

    let mut heroes: Vec<MetaDlHero> = rows
        .iter()
        .filter_map(|r| {
            let played = num(r, "matches");
            if played == 0 {
                return None;
            }
            let id = num(r, "hero_id") as u32;
            let deaths = num(r, "total_deaths").max(1);
            Some(MetaDlHero {
                id,
                name: names.get(&id).map(|h| h.name.clone()).unwrap_or_else(|| format!("Hero {id}")),
                image: names.get(&id).and_then(|h| h.image.clone()),
                matches: played,
                win_rate: num(r, "wins") as f64 / played as f64 * 100.0,
                pick_rate: played as f64 / matches as f64 * 100.0,
                kda: (num(r, "total_kills") + num(r, "total_assists")) as f64 / deaths as f64,
                avg_souls: num(r, "total_net_worth") / played,
                avg_damage: num(r, "total_player_damage") / played,
            })
        })
        .collect();
    heroes.sort_by(|a, b| b.win_rate.partial_cmp(&a.win_rate).unwrap_or(std::cmp::Ordering::Equal));

    let items = deadlock_items(item_rows.unwrap_or(serde_json::Value::Null), &item_names);

    let meta = DeadlockMeta { matches, heroes, items };
    cache.lock().unwrap().deadlock = Some((Instant::now(), meta.clone()));
    Ok(meta)
}

fn deadlock_items(value: serde_json::Value, names: &HashMap<u64, String>) -> Vec<MetaDlItem> {
    let Some(rows) = value.as_array() else { return Vec::new() };

    let busiest = rows
        .iter()
        .filter_map(|r| r.get("matches").and_then(|v| v.as_u64()))
        .max()
        .unwrap_or(1)
        .max(1);

    let mut items: Vec<MetaDlItem> = rows
        .iter()
        .filter_map(|r| {
            let played = r.get("matches").and_then(|v| v.as_u64())?;
            if played == 0 {
                return None;
            }
            let id = r.get("item_id").and_then(|v| v.as_u64())?;
            let wins = r.get("wins").and_then(|v| v.as_u64()).unwrap_or(0);
            Some(MetaDlItem {
                id,
                // An unresolved name means the asset list did not load, not
                // that the item is nameless — say so rather than printing a
                // bare id and hoping nobody notices.
                name: names.get(&id).cloned().unwrap_or_else(|| "Name unavailable".to_string()),
                matches: played,
                win_rate: wins as f64 / played as f64 * 100.0,
                share: played as f64 / busiest as f64 * 100.0,
                buy_minute: r
                    .get("avg_buy_time_s")
                    .and_then(|v| v.as_f64())
                    .filter(|s| *s > 0.0)
                    .map(|s| s / 60.0),
            })
        })
        .collect();

    items.sort_by(|a, b| b.win_rate.partial_cmp(&a.win_rate).unwrap_or(std::cmp::Ordering::Equal));
    items
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn the_partial_final_bucket_is_excluded_from_the_trend() {
        // OpenDota's last trend bucket is the week in progress, so it is
        // always short. Including it would read as every hero falling at
        // once, which is the tell that the window is wrong rather than the
        // meta having moved.
        let picks = vec![1000, 1000, 1000, 1000, 1000, 1000, 90];
        let wins = vec![500, 500, 500, 600, 600, 600, 10];

        let usable = picks.len() - 1;
        let mid = usable / 2;
        let earlier = bucket_win_rate(&wins, &picks, 0, mid).unwrap();
        let recent = bucket_win_rate(&wins, &picks, mid, usable).unwrap();

        assert_eq!(earlier, 50.0);
        assert_eq!(recent, 60.0);
        assert_eq!(recent - earlier, 10.0, "a hero winning 10 points more lately should read as +10");
    }

    #[test]
    fn a_flat_hero_has_no_trend() {
        let picks = vec![500u64; 7];
        let wins = vec![250u64; 7];
        let usable = picks.len() - 1;
        let mid = usable / 2;
        let earlier = bucket_win_rate(&wins, &picks, 0, mid).unwrap();
        let recent = bucket_win_rate(&wins, &picks, mid, usable).unwrap();
        assert_eq!(recent - earlier, 0.0);
    }

    #[test]
    fn an_empty_window_yields_no_win_rate_rather_than_a_division_by_zero() {
        assert_eq!(bucket_win_rate(&[0, 0], &[0, 0], 0, 2), None);
        assert_eq!(bucket_win_rate(&[1], &[2], 5, 9), None);
    }
}
