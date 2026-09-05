//! Deadlock tracking, via the community-run Deadlock API.
//!
//! This works fundamentally differently from the Dota side, and it's worth
//! being clear about why. Dota 2 ships Valve's official Game State
//! Integration: the game itself POSTs live state to us several times a
//! second while you play. **Deadlock has no such feed.** Valve publishes no
//! GSI for it, so nothing can read your live Deadlock match locally.
//!
//! What exists instead is <https://deadlock-api.com> — an independent,
//! community-run service that aggregates match data from Valve's own client
//! APIs. So the Deadlock side here is:
//!
//! - **post-match**, not live: matches appear once the API has ingested
//!   them, typically shortly after the game ends;
//! - **keyed on a Steam account**, not on a local feed, so you link your
//!   account once;
//! - **dependent on a third party** that Valve has been rate-limiting, so
//!   matches can be missing or delayed. Failures here are surfaced plainly
//!   rather than dressed up as empty state.
//!
//! Deliberately *not* done: nothing here reads Deadlock's memory, injects
//! into the game, or surfaces information a player couldn't already see.
//! Live data is limited to "are you currently in a match", which the game
//! shows you anyway.

use std::collections::HashMap;
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use serde::{Deserialize, Serialize};

use crate::storage::log_dir;

const API: &str = "https://api.deadlock-api.com";
const UA: &str = concat!("dota-tracker/", env!("CARGO_PKG_VERSION"), " (+desktop)");

// ---------- Linked account ----------

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct DeadlockLink {
    #[serde(rename = "accountId")]
    pub account_id: Option<u64>,
    pub personaname: Option<String>,
    pub avatar: Option<String>,
}

fn link_file() -> std::path::PathBuf {
    log_dir().join("deadlock.json")
}

pub fn load_link() -> DeadlockLink {
    match std::fs::read_to_string(link_file()) {
        Ok(s) => serde_json::from_str(&s).unwrap_or_default(),
        Err(_) => DeadlockLink::default(),
    }
}

pub fn save_link(link: &DeadlockLink) {
    let _ = std::fs::create_dir_all(log_dir());
    if let Ok(json) = serde_json::to_string_pretty(link) {
        let _ = std::fs::write(link_file(), json);
    }
}

// ---------- HTTP ----------

async fn get_json(path: &str) -> Result<serde_json::Value, String> {
    let url = format!("{API}{path}");
    let client = reqwest::Client::builder()
        .user_agent(UA)
        .timeout(Duration::from_secs(20))
        .build()
        .map_err(|e| format!("HTTP client error: {e}"))?;

    let resp = client.get(&url).send().await.map_err(|e| {
        if e.is_timeout() {
            "The Deadlock API timed out — it's community-run and sometimes slow.".to_string()
        } else if e.is_connect() {
            "Couldn't reach the Deadlock API — check your connection.".to_string()
        } else {
            format!("Request failed: {e}")
        }
    })?;

    let status = resp.status();
    if status == reqwest::StatusCode::TOO_MANY_REQUESTS {
        return Err("The Deadlock API is rate-limiting requests right now — try again shortly.".to_string());
    }
    if !status.is_success() {
        return Err(format!("Deadlock API returned {}.", status.as_u16()));
    }
    resp.json().await.map_err(|e| format!("Couldn't parse the API response: {e}"))
}

// ---------- Hero assets (cached) ----------

#[derive(Debug, Clone, Serialize)]
pub struct Hero {
    pub id: u32,
    pub name: String,
    pub image: Option<String>,
}

/// Hero id -> name/portrait. The asset list is a few hundred KB and only
/// changes when Valve ships a hero, so it's fetched once per run and then
/// reused; every match row needs it to turn `hero_id` into something a
/// person can read.
#[derive(Default)]
pub struct HeroCache {
    heroes: HashMap<u32, Hero>,
    fetched_at: Option<Instant>,
}

pub type SharedHeroes = Arc<Mutex<HeroCache>>;

pub async fn heroes(cache: &SharedHeroes) -> HashMap<u32, Hero> {
    const TTL: Duration = Duration::from_secs(60 * 60 * 6);
    {
        let c = cache.lock().unwrap();
        if let Some(at) = c.fetched_at {
            if at.elapsed() < TTL && !c.heroes.is_empty() {
                return c.heroes.clone();
            }
        }
    }

    let Ok(value) = get_json("/v1/assets/heroes?only_active=true").await else {
        // Fall back to whatever was cached; unnamed heroes beat no matches.
        return cache.lock().unwrap().heroes.clone();
    };

    let mut map = HashMap::new();
    if let Some(list) = value.as_array() {
        for h in list {
            let Some(id) = h.get("id").and_then(|v| v.as_u64()) else { continue };
            let name = h.get("name").and_then(|v| v.as_str()).unwrap_or("Unknown").to_string();
            let image = h
                .get("images")
                .and_then(|i| i.get("icon_image_small"))
                .or_else(|| h.get("images").and_then(|i| i.get("icon_hero_card")))
                .and_then(|v| v.as_str())
                .map(|s| s.to_string());
            map.insert(id as u32, Hero { id: id as u32, name, image });
        }
    }

    if !map.is_empty() {
        let mut c = cache.lock().unwrap();
        c.heroes = map.clone();
        c.fetched_at = Some(Instant::now());
    }
    map
}

// ---------- Steam search ----------

#[derive(Debug, Clone, Serialize)]
pub struct SteamProfile {
    #[serde(rename = "accountId")]
    pub account_id: u64,
    pub personaname: String,
    pub avatar: Option<String>,
    #[serde(rename = "profileUrl")]
    pub profile_url: Option<String>,
}

pub async fn search_players(query: &str) -> Result<Vec<SteamProfile>, String> {
    let q = urlencode(query);
    let value = get_json(&format!("/v1/players/steam-search?search_query={q}")).await?;
    let mut out = Vec::new();
    if let Some(list) = value.as_array() {
        for p in list.iter().take(20) {
            let Some(account_id) = p.get("account_id").and_then(|v| v.as_u64()) else { continue };
            out.push(SteamProfile {
                account_id,
                personaname: p
                    .get("personaname")
                    .and_then(|v| v.as_str())
                    .unwrap_or("(no name)")
                    .to_string(),
                avatar: p.get("avatarmedium").or_else(|| p.get("avatar")).and_then(|v| v.as_str()).map(String::from),
                profile_url: p.get("profileurl").and_then(|v| v.as_str()).map(String::from),
            });
        }
    }
    Ok(out)
}

/// Minimal percent-encoding for the one query parameter this module sends.
fn urlencode(s: &str) -> String {
    s.bytes()
        .map(|b| match b {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => (b as char).to_string(),
            b' ' => "+".to_string(),
            other => format!("%{other:02X}"),
        })
        .collect()
}

// ---------- Rank ----------

const RANK_TIERS: [&str; 12] = [
    "Obscurus", "Initiate", "Seeker", "Acolyte", "Sentinel", "Mystic", "Ritualist", "Emissary",
    "Oracle", "Phantom", "Ascendant", "Eternus",
];

#[derive(Debug, Clone, Serialize)]
pub struct DeadlockRank {
    pub badge: u32,
    pub tier: u32,
    pub subrank: u32,
    #[serde(rename = "tierName")]
    pub tier_name: String,
    pub label: String,
}

pub async fn rank(account_id: u64) -> Result<Option<DeadlockRank>, String> {
    let value = get_json(&format!("/v1/players/{account_id}/rank")).await?;
    let Some(badge) = value.get("badge").and_then(|v| v.as_u64()) else {
        return Ok(None);
    };
    // `badge` packs the tier and subrank as tier*10 + subrank, which is why
    // badge 26 reads as Seeker 6.
    let tier = (badge / 10) as u32;
    let subrank = (badge % 10) as u32;
    let tier_name = RANK_TIERS.get(tier as usize).copied().unwrap_or("Unranked").to_string();
    Ok(Some(DeadlockRank {
        badge: badge as u32,
        tier,
        subrank,
        label: if subrank > 0 { format!("{tier_name} {subrank}") } else { tier_name.clone() },
        tier_name,
    }))
}

// ---------- Match history ----------

#[derive(Debug, Clone, Serialize)]
pub struct DeadlockMatch {
    #[serde(rename = "matchId")]
    pub match_id: u64,
    #[serde(rename = "heroId")]
    pub hero_id: u32,
    #[serde(rename = "heroName")]
    pub hero_name: String,
    #[serde(rename = "heroImage")]
    pub hero_image: Option<String>,
    #[serde(rename = "startTime")]
    pub start_time: i64,
    #[serde(rename = "durationSeconds")]
    pub duration_seconds: u32,
    pub kills: u32,
    pub deaths: u32,
    pub assists: u32,
    #[serde(rename = "netWorth")]
    pub net_worth: u64,
    #[serde(rename = "lastHits")]
    pub last_hits: u32,
    pub denies: u32,
    #[serde(rename = "heroLevel")]
    pub hero_level: u32,
    /// "win" | "loss" | "abandoned" | "unscored"
    pub outcome: String,
    pub abandoned: bool,
}

pub async fn match_history(
    account_id: u64,
    cache: &SharedHeroes,
    limit: usize,
) -> Result<Vec<DeadlockMatch>, String> {
    let value = get_json(&format!("/v1/players/{account_id}/match-history")).await?;
    let hero_map = heroes(cache).await;

    let mut out = Vec::new();
    let Some(list) = value.as_array() else { return Ok(out) };

    for m in list.iter().take(limit) {
        let hero_id = m.get("hero_id").and_then(|v| v.as_u64()).unwrap_or(0) as u32;
        let hero = hero_map.get(&hero_id);
        let abandoned = m.get("team_abandoned").and_then(|v| v.as_bool()).unwrap_or(false);

        // Win/loss comes from comparing the player's team to the winning
        // team, NOT from `player_match_outcome`.
        //
        // The docs describe player_match_outcome as authoritative (0 invalid,
        // 1 win, 2 loss, 3/4 penalized, 5 not scored) — and it is, when it's
        // populated. Across a real 232-match history it was 0 on 173 of them,
        // so trusting it left three quarters of a player's games showing no
        // result at all. `match_result` and `player_team` were present on
        // every record, and where both signals existed they agreed 56 out
        // of 56.
        //
        // So the team comparison decides win/loss, and player_match_outcome
        // is consulted only for the penalty cases it uniquely reports.
        let outcome_code = m.get("player_match_outcome").and_then(|v| v.as_u64());
        let player_team = m.get("player_team").and_then(|v| v.as_u64());
        let match_result = m.get("match_result").and_then(|v| v.as_u64());

        let outcome = match outcome_code {
            // An abandon is the one thing the team comparison can't express.
            Some(3) | Some(4) => "abandoned".to_string(),
            _ => match (player_team, match_result) {
                (Some(team), Some(winner)) => {
                    if team == winner { "win".to_string() } else { "loss".to_string() }
                }
                // Neither signal available — genuinely unknown.
                _ => match outcome_code {
                    Some(1) => "win".to_string(),
                    Some(2) => "loss".to_string(),
                    _ => "unscored".to_string(),
                },
            },
        };

        out.push(DeadlockMatch {
            match_id: m.get("match_id").and_then(|v| v.as_u64()).unwrap_or(0),
            hero_id,
            hero_name: hero.map(|h| h.name.clone()).unwrap_or_else(|| format!("Hero {hero_id}")),
            hero_image: hero.and_then(|h| h.image.clone()),
            start_time: m.get("start_time").and_then(|v| v.as_i64()).unwrap_or(0),
            duration_seconds: m.get("match_duration_s").and_then(|v| v.as_u64()).unwrap_or(0) as u32,
            kills: m.get("player_kills").and_then(|v| v.as_u64()).unwrap_or(0) as u32,
            deaths: m.get("player_deaths").and_then(|v| v.as_u64()).unwrap_or(0) as u32,
            assists: m.get("player_assists").and_then(|v| v.as_u64()).unwrap_or(0) as u32,
            net_worth: m.get("net_worth").and_then(|v| v.as_u64()).unwrap_or(0),
            last_hits: m.get("last_hits").and_then(|v| v.as_u64()).unwrap_or(0) as u32,
            denies: m.get("denies").and_then(|v| v.as_u64()).unwrap_or(0) as u32,
            hero_level: m.get("hero_level").and_then(|v| v.as_u64()).unwrap_or(0) as u32,
            outcome,
            abandoned,
        });
    }
    Ok(out)
}

// ---------- Live match presence ----------

#[derive(Debug, Clone, Serialize)]
pub struct DeadlockLive {
    #[serde(rename = "matchId")]
    pub match_id: u64,
    #[serde(rename = "startTime")]
    pub start_time: i64,
    #[serde(rename = "heroId")]
    pub hero_id: u32,
    #[serde(rename = "heroName")]
    pub hero_name: String,
    #[serde(rename = "heroImage")]
    pub hero_image: Option<String>,
    /// Heroes on each side. Deadlock shows every player the full hero
    /// lineup in-game, so this reveals nothing hidden — and deliberately
    /// carries no opponent identities, ranks or stats.
    #[serde(rename = "allyHeroes")]
    pub ally_heroes: Vec<String>,
    #[serde(rename = "enemyHeroes")]
    pub enemy_heroes: Vec<String>,
}

pub async fn live_match(account_id: u64, cache: &SharedHeroes) -> Result<Option<DeadlockLive>, String> {
    let value = get_json("/v1/matches/active").await?;
    let hero_map = heroes(cache).await;
    let Some(list) = value.as_array() else { return Ok(None) };

    for m in list {
        let Some(players) = m.get("players").and_then(|p| p.as_array()) else { continue };
        let Some(me) = players
            .iter()
            .find(|p| p.get("account_id").and_then(|v| v.as_u64()) == Some(account_id))
        else {
            continue;
        };

        let my_team = me.get("team").and_then(|v| v.as_u64());
        let my_hero_id = me.get("hero_id").and_then(|v| v.as_u64()).unwrap_or(0) as u32;
        let hero = hero_map.get(&my_hero_id);

        let name_of = |p: &serde_json::Value| -> String {
            let id = p.get("hero_id").and_then(|v| v.as_u64()).unwrap_or(0) as u32;
            hero_map.get(&id).map(|h| h.name.clone()).unwrap_or_else(|| format!("Hero {id}"))
        };
        let (mut allies, mut enemies) = (Vec::new(), Vec::new());
        for p in players {
            if p.get("account_id").and_then(|v| v.as_u64()) == Some(account_id) {
                continue;
            }
            if p.get("team").and_then(|v| v.as_u64()) == my_team {
                allies.push(name_of(p));
            } else {
                enemies.push(name_of(p));
            }
        }

        return Ok(Some(DeadlockLive {
            match_id: m.get("match_id").and_then(|v| v.as_u64()).unwrap_or(0),
            start_time: m.get("start_time").and_then(|v| v.as_i64()).unwrap_or(0),
            hero_id: my_hero_id,
            hero_name: hero.map(|h| h.name.clone()).unwrap_or_else(|| "Unknown".to_string()),
            hero_image: hero.and_then(|h| h.image.clone()),
            ally_heroes: allies,
            enemy_heroes: enemies,
        }));
    }
    Ok(None)
}

// ---------- Single match detail ----------

#[derive(Debug, Clone, Serialize)]
pub struct DeadlockPlayer {
    #[serde(rename = "accountId")]
    pub account_id: u64,
    #[serde(rename = "heroName")]
    pub hero_name: String,
    #[serde(rename = "heroImage")]
    pub hero_image: Option<String>,
    pub team: u64,
    pub kills: u32,
    pub deaths: u32,
    pub assists: u32,
    #[serde(rename = "netWorth")]
    pub net_worth: u64,
    #[serde(rename = "lastHits")]
    pub last_hits: u32,
    pub denies: u32,
    pub level: u32,
    #[serde(rename = "isMe")]
    pub is_me: bool,
}

#[derive(Debug, Clone, Serialize)]
pub struct DeadlockMatchDetail {
    #[serde(rename = "matchId")]
    pub match_id: u64,
    #[serde(rename = "durationSeconds")]
    pub duration_seconds: u32,
    #[serde(rename = "winningTeam")]
    pub winning_team: u64,
    pub players: Vec<DeadlockPlayer>,
}

/// Full scoreboard for one match.
///
/// The metadata payload is around a megabyte — it carries every death
/// position, damage matrix and ability event — so this is only ever fetched
/// when a row is actually expanded, and the frontend caches the result.
pub async fn match_detail(
    match_id: u64,
    me: Option<u64>,
    cache: &SharedHeroes,
) -> Result<DeadlockMatchDetail, String> {
    let value = get_json(&format!("/v1/matches/{match_id}/metadata")).await?;
    let hero_map = heroes(cache).await;

    let info = value.get("match_info").unwrap_or(&value);

    let mut players = Vec::new();
    if let Some(list) = info.get("players").and_then(|p| p.as_array()) {
        for p in list {
            let hero_id = p.get("hero_id").and_then(|v| v.as_u64()).unwrap_or(0) as u32;
            let hero = hero_map.get(&hero_id);
            let account_id = p.get("account_id").and_then(|v| v.as_u64()).unwrap_or(0);

            players.push(DeadlockPlayer {
                account_id,
                hero_name: hero.map(|h| h.name.clone()).unwrap_or_else(|| format!("Hero {hero_id}")),
                hero_image: hero.and_then(|h| h.image.clone()),
                team: p.get("team").and_then(|v| v.as_u64()).unwrap_or(0),
                kills: p.get("kills").and_then(|v| v.as_u64()).unwrap_or(0) as u32,
                deaths: p.get("deaths").and_then(|v| v.as_u64()).unwrap_or(0) as u32,
                assists: p.get("assists").and_then(|v| v.as_u64()).unwrap_or(0) as u32,
                net_worth: p.get("net_worth").and_then(|v| v.as_u64()).unwrap_or(0),
                last_hits: p.get("last_hits").and_then(|v| v.as_u64()).unwrap_or(0) as u32,
                denies: p.get("denies").and_then(|v| v.as_u64()).unwrap_or(0) as u32,
                level: p.get("level").and_then(|v| v.as_u64()).unwrap_or(0) as u32,
                is_me: me.is_some_and(|m| m == account_id),
            });
        }
    }

    // Sort by team then net worth, which is how a scoreboard is read.
    players.sort_by(|a, b| a.team.cmp(&b.team).then(b.net_worth.cmp(&a.net_worth)));

    Ok(DeadlockMatchDetail {
        match_id,
        duration_seconds: info.get("duration_s").and_then(|v| v.as_u64()).unwrap_or(0) as u32,
        winning_team: info.get("winning_team").and_then(|v| v.as_u64()).unwrap_or(0),
        players,
    })
}

// ---------- Derived summary ----------

#[derive(Debug, Clone, Serialize)]
pub struct DeadlockSummary {
    pub matches: usize,
    pub wins: usize,
    pub losses: usize,
    #[serde(rename = "winRate")]
    pub win_rate: f64,
    pub kills: u32,
    pub deaths: u32,
    pub assists: u32,
    pub kda: f64,
    #[serde(rename = "avgSouls")]
    pub avg_souls: u64,
    #[serde(rename = "bestHero")]
    pub best_hero: Option<String>,
}

/// Rolls a match list into headline numbers. Only scored games count
/// towards the win rate — abandons would otherwise quietly drag it down.
pub fn summarize(matches: &[DeadlockMatch]) -> DeadlockSummary {
    let scored: Vec<&DeadlockMatch> = matches.iter().filter(|m| m.outcome == "win" || m.outcome == "loss").collect();
    let wins = scored.iter().filter(|m| m.outcome == "win").count();
    let losses = scored.len() - wins;

    let (mut k, mut d, mut a, mut souls) = (0u32, 0u32, 0u32, 0u64);
    for m in matches {
        k += m.kills;
        d += m.deaths;
        a += m.assists;
        souls += m.net_worth;
    }

    // Most-won hero, falling back to most-played when nothing is won yet.
    let mut per_hero: HashMap<&str, (usize, usize)> = HashMap::new();
    for m in matches {
        let e = per_hero.entry(m.hero_name.as_str()).or_insert((0, 0));
        e.0 += 1;
        if m.outcome == "win" {
            e.1 += 1;
        }
    }
    let best_hero = per_hero
        .iter()
        .max_by_key(|(_, (played, won))| (*won, *played))
        .map(|(name, _)| (*name).to_string());

    DeadlockSummary {
        matches: matches.len(),
        wins,
        losses,
        win_rate: if scored.is_empty() { 0.0 } else { (wins as f64 / scored.len() as f64) * 100.0 },
        kills: k,
        deaths: d,
        assists: a,
        // Deaths of zero would divide by zero; treat a deathless run as 1
        // for the ratio, the usual convention.
        kda: (k + a) as f64 / d.max(1) as f64,
        avg_souls: if matches.is_empty() { 0 } else { souls / matches.len() as u64 },
        best_hero,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Reference account used to check parsing against real responses.
    const TEST_ACCOUNT: u64 = 850402858;

    fn cache() -> SharedHeroes {
        Arc::new(Mutex::new(HeroCache::default()))
    }

    /// These hit the live community API, so they're `#[ignore]`d by default
    /// and run on demand with `cargo test -- --ignored`. They exist because
    /// the risk in this module isn't logic, it's the API's shape changing
    /// underneath us — which a mocked test would never catch.
    #[tokio::test]
    #[ignore]
    async fn heroes_resolve_names_and_images() {
        let map = heroes(&cache()).await;
        assert!(!map.is_empty(), "hero asset list came back empty");
        let named = map.values().filter(|h| !h.name.is_empty() && h.name != "Unknown").count();
        assert!(named > 10, "expected many named heroes, got {named}");
        assert!(map.values().any(|h| h.image.is_some()), "no hero had a portrait");
    }

    #[tokio::test]
    #[ignore]
    async fn match_history_parses_and_classifies_outcomes() {
        let matches = match_history(TEST_ACCOUNT, &cache(), 25).await.expect("request failed");
        assert!(!matches.is_empty(), "no matches returned for the test account");

        for m in &matches {
            assert!(m.match_id > 0, "match id missing");
            assert!(
                ["win", "loss", "abandoned", "unscored"].contains(&m.outcome.as_str()),
                "unexpected outcome {:?}",
                m.outcome
            );
            // A hero that failed to resolve still gets a readable fallback.
            assert!(!m.hero_name.is_empty());
        }
        // Sanity: a real account should have at least one scored result.
        assert!(
            matches.iter().any(|m| m.outcome == "win" || m.outcome == "loss"),
            "no scored matches at all — outcome mapping is probably wrong"
        );
    }

    #[tokio::test]
    #[ignore]
    async fn summary_only_counts_scored_games() {
        let matches = match_history(TEST_ACCOUNT, &cache(), 50).await.expect("request failed");
        let s = summarize(&matches);
        assert_eq!(s.matches, matches.len());
        assert_eq!(s.wins + s.losses, matches.iter().filter(|m| m.outcome == "win" || m.outcome == "loss").count());
        assert!((0.0..=100.0).contains(&s.win_rate), "win rate out of range: {}", s.win_rate);
    }

    #[tokio::test]
    #[ignore]
    async fn rank_decodes_badge_into_tier_and_subrank() {
        if let Some(r) = rank(TEST_ACCOUNT).await.expect("request failed") {
            assert_eq!(r.tier, r.badge / 10);
            assert_eq!(r.subrank, r.badge % 10);
            assert!(!r.tier_name.is_empty());
        }
    }

    #[test]
    fn summarize_handles_an_empty_history() {
        let s = summarize(&[]);
        assert_eq!(s.matches, 0);
        assert_eq!(s.win_rate, 0.0);
        assert_eq!(s.avg_souls, 0);
        assert!(s.best_hero.is_none());
    }

    #[test]
    fn kda_does_not_divide_by_zero_on_a_deathless_game() {
        let m = DeadlockMatch {
            match_id: 1,
            hero_id: 1,
            hero_name: "Infernus".into(),
            hero_image: None,
            start_time: 0,
            duration_seconds: 600,
            kills: 5,
            deaths: 0,
            assists: 3,
            net_worth: 1000,
            last_hits: 10,
            denies: 0,
            hero_level: 5,
            outcome: "win".into(),
            abandoned: false,
        };
        let s = summarize(std::slice::from_ref(&m));
        assert!(s.kda.is_finite(), "KDA became {} on a deathless game", s.kda);
        assert_eq!(s.kda, 8.0);
    }
}
