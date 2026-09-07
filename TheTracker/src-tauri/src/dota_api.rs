//! Rich Dota 2 match history, via the public OpenDota API.
//!
//! The GSI feed this app is built on is excellent for *live* tracking, but
//! it is deliberately narrow: Valve only exposes the local player's own
//! state, and it never says who won. That's why matches finalized from GSI
//! alone have no result and an unspecified game type.
//!
//! OpenDota fills that gap after the fact. It's the same public data
//! Dotabuff and friends are built on, needs no API key, and is keyed on the
//! Steam32 account id — the same id space the Deadlock side uses.
//!
//! What this adds over GSI: win/loss, authoritative game mode and lobby
//! type (so Ranked/Turbo/All Pick stop being a manual guess), GPM/XPM, hero
//! damage, and the full ten-player scoreboard for a single match.

use std::collections::HashMap;
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use serde::{Deserialize, Serialize};

use crate::storage::log_dir;

const API: &str = "https://api.opendota.com/api";
const UA: &str = concat!("TheTracker/", env!("CARGO_PKG_VERSION"), " (+desktop)");

// ---------- Linked Steam account ----------

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct DotaLink {
    #[serde(rename = "accountId")]
    pub account_id: Option<u64>,
    pub personaname: Option<String>,
    pub avatar: Option<String>,
}

fn link_file() -> std::path::PathBuf {
    log_dir().join("dota_account.json")
}

pub fn load_link() -> DotaLink {
    match std::fs::read_to_string(link_file()) {
        Ok(s) => serde_json::from_str(&s).unwrap_or_default(),
        Err(_) => DotaLink::default(),
    }
}

pub fn save_link(link: &DotaLink) {
    let _ = std::fs::create_dir_all(log_dir());
    if let Ok(json) = serde_json::to_string_pretty(link) {
        let _ = std::fs::write(link_file(), json);
    }
}

// ---------- HTTP ----------

/// Shared with popular.rs so the two modules use one HTTP path.
pub async fn get_json_public(path: &str) -> Result<serde_json::Value, String> {
    get_json(path).await
}

/// How many times a request is attempted before the error reaches the UI.
///
/// OpenDota is free and frequently slow, and a single cold request timing out
/// was enough to surface a hard failure on the page — which is what made the
/// app look broken for the first few seconds after every launch. Three
/// attempts with a growing pause turns the common blip into a delay instead.
const ATTEMPTS: u32 = 3;

async fn get_json(path: &str) -> Result<serde_json::Value, String> {
    let client = reqwest::Client::builder()
        .user_agent(UA)
        .timeout(Duration::from_secs(20))
        .build()
        .map_err(|e| format!("HTTP client error: {e}"))?;

    let mut last = String::new();
    for attempt in 0..ATTEMPTS {
        if attempt > 0 {
            // 400ms, then 1.6s. Short enough not to be felt as a hang, long
            // enough to clear a rate-limit window.
            let backoff = 400u64 * 4u64.pow(attempt - 1);
            tokio::time::sleep(Duration::from_millis(backoff)).await;
        }

        match attempt_get(&client, path).await {
            Ok(v) => return Ok(v),
            // Only worth retrying what is plausibly transient. A 404 or a
            // malformed body will fail the same way three times over.
            Err((msg, retryable)) => {
                last = msg;
                if !retryable {
                    return Err(last);
                }
            }
        }
    }
    Err(last)
}

/// Returns the error text plus whether retrying it could plausibly help.
async fn attempt_get(
    client: &reqwest::Client,
    path: &str,
) -> Result<serde_json::Value, (String, bool)> {
    let resp = client.get(format!("{API}{path}")).send().await.map_err(|e| {
        if e.is_timeout() {
            ("OpenDota timed out — try again in a moment.".to_string(), true)
        } else if e.is_connect() {
            ("Couldn't reach OpenDota — check your connection.".to_string(), true)
        } else {
            (format!("Request failed: {e}"), false)
        }
    })?;

    let status = resp.status();
    if status == reqwest::StatusCode::TOO_MANY_REQUESTS {
        return Err(("OpenDota is rate-limiting right now — try again shortly.".to_string(), true));
    }
    if status.is_server_error() {
        return Err((format!("OpenDota returned {}.", status.as_u16()), true));
    }
    if !status.is_success() {
        return Err((format!("OpenDota returned {}.", status.as_u16()), false));
    }
    resp.json()
        .await
        .map_err(|e| (format!("Couldn't parse the OpenDota response: {e}"), false))
}

// ---------- Hero constants (cached) ----------

#[derive(Debug, Clone, Serialize)]
pub struct DotaHero {
    pub id: u32,
    pub name: String,
    /// Internal name minus the npc_dota_hero_ prefix, for portrait URLs.
    pub slug: String,
}

#[derive(Default)]
pub struct DotaHeroCache {
    heroes: HashMap<u32, DotaHero>,
    fetched_at: Option<Instant>,
}

pub type SharedDotaHeroes = Arc<Mutex<DotaHeroCache>>;

/// Hero names and portrait slugs. These change when Valve adds a hero, so a
/// day-old copy is fine and a network round trip on every launch is not.
pub async fn heroes(cache: &SharedDotaHeroes) -> HashMap<u32, DotaHero> {
    const TTL: Duration = Duration::from_secs(60 * 60 * 24);
    const TTL_SECS: u64 = 60 * 60 * 24;

    {
        let c = cache.lock().unwrap();
        if let Some(at) = c.fetched_at {
            if at.elapsed() < TTL && !c.heroes.is_empty() {
                return c.heroes.clone();
            }
        }
    }

    // Warm start: last launch's copy, if it is still young enough. Without
    // this the 24-hour TTL above only ever applied within one run.
    if let Some(cached) = crate::storage::read_cache("dota_heroes", TTL_SECS) {
        let map = parse_heroes(&cached);
        if !map.is_empty() {
            let mut c = cache.lock().unwrap();
            c.heroes = map.clone();
            c.fetched_at = Some(Instant::now());
            return map;
        }
    }

    let Ok(value) = get_json("/heroes").await else {
        return cache.lock().unwrap().heroes.clone();
    };

    let map = parse_heroes(&value);
    if !map.is_empty() {
        crate::storage::write_cache("dota_heroes", &value);
        let mut c = cache.lock().unwrap();
        c.heroes = map.clone();
        c.fetched_at = Some(Instant::now());
    }
    map
}

fn parse_heroes(value: &serde_json::Value) -> HashMap<u32, DotaHero> {
    let mut map = HashMap::new();
    if let Some(list) = value.as_array() {
        for h in list {
            let Some(id) = h.get("id").and_then(|v| v.as_u64()) else { continue };
            let raw = h.get("name").and_then(|v| v.as_str()).unwrap_or("");
            map.insert(
                id as u32,
                DotaHero {
                    id: id as u32,
                    name: h.get("localized_name").and_then(|v| v.as_str()).unwrap_or("Unknown").to_string(),
                    slug: raw.strip_prefix("npc_dota_hero_").unwrap_or(raw).to_string(),
                },
            );
        }
    }
    map
}

// ---------- Game type classification ----------

/// Mode and queue are two independent facts about a match, and collapsing
/// them into one bucket is why filtering was wrong.
///
/// `game_mode` says how heroes get picked — All Pick, Turbo, Random Draft,
/// Ability Draft. `lobby_type` says which queue it came from — Ranked or
/// unranked. A game is both at once: Ranked Turbo is a real thing, so is
/// unranked All Pick. The old classifier forced a single answer in priority
/// order, so a ranked All Pick game came back "ranked" with the mode thrown
/// away, and a ranked Turbo came back "turbo" with the queue thrown away.
///
/// Both are now kept, and the UI filters on each axis separately.
///
/// `classify` stays because history.json, the leaderboard's type filter and
/// the cloud rows are all keyed on it, and rewriting stored matches to a new
/// scheme would lose the types the player set by hand. It is a comparison
/// bucket, not a description of the match.
pub fn classify(game_mode: u32, lobby_type: u32) -> &'static str {
    match (game_mode, lobby_type) {
        (23, _) => "turbo",
        (_, 7) => "ranked",
        (1 | 22, _) => "all_pick",
        _ => "other",
    }
}

/// Stable key for the mode filter. Modes 1 and 22 are both "All Pick" to a
/// player — 22 is `all_draft`, the mode the ranked All Pick queue actually
/// runs — so they share a key rather than splitting the filter in two.
pub fn game_mode_key(id: u32) -> &'static str {
    match id {
        1 | 22 => "all_pick",
        2 => "captains_mode",
        3 => "random_draft",
        4 => "single_draft",
        5 => "all_random",
        12 => "least_played",
        16 => "captains_draft",
        17 => "balanced_draft",
        18 => "ability_draft",
        20 => "ardm",
        21 => "1v1_mid",
        23 => "turbo",
        24 => "mutation",
        _ => "other",
    }
}

/// Names taken from OpenDota's `constants/game_mode` table rather than
/// memory. Everything it lists is here, so a match stops reporting
/// "Unknown Mode" for a mode that has a perfectly good name.
pub fn game_mode_name(id: u32) -> &'static str {
    match id {
        1 | 22 => "All Pick",
        2 => "Captains Mode",
        3 => "Random Draft",
        4 => "Single Draft",
        5 => "All Random",
        6 => "Intro",
        7 => "Diretide",
        8 => "Reverse Captains Mode",
        9 => "Greeviling",
        10 => "Tutorial",
        11 => "Mid Only",
        12 => "Least Played",
        13 => "Limited Heroes",
        14 => "Compendium Matchmaking",
        15 => "Custom Game",
        16 => "Captains Draft",
        17 => "Balanced Draft",
        18 => "Ability Draft",
        19 => "Event",
        20 => "All Random Deathmatch",
        21 => "1v1 Mid",
        23 => "Turbo",
        24 => "Mutation",
        25 => "Coaches Challenge",
        _ => "Unknown Mode",
    }
}

/// True only for the matchmaking queues that actually affect rank: ranked
/// solo, ranked team, and plain ranked. Battle Cup and tournaments are
/// competitive but are not the ranked ladder, so they are not folded in.
pub fn is_ranked_lobby(lobby_type: u32) -> bool {
    matches!(lobby_type, 5 | 6 | 7)
}

/// Also from OpenDota's `constants/lobby_type`.
pub fn lobby_type_name(id: u32) -> &'static str {
    match id {
        0 => "Unranked",
        1 => "Practice",
        2 => "Tournament",
        3 => "Tutorial",
        4 => "Bots",
        5 | 6 | 7 => "Ranked",
        8 => "1v1 Mid",
        9 => "Battle Cup",
        10 => "Local Bots",
        11 => "Spectator",
        12 => "Event",
        13 => "Gauntlet",
        14 => "New Player",
        15 => "Featured",
        _ => "Other",
    }
}

// ---------- Match history ----------

#[derive(Debug, Clone, Serialize)]
pub struct DotaApiMatch {
    #[serde(rename = "matchId")]
    pub match_id: u64,
    #[serde(rename = "heroId")]
    pub hero_id: u32,
    #[serde(rename = "heroName")]
    pub hero_name: String,
    #[serde(rename = "heroSlug")]
    pub hero_slug: String,
    #[serde(rename = "startTime")]
    pub start_time: i64,
    #[serde(rename = "durationSeconds")]
    pub duration_seconds: u32,
    pub won: bool,
    pub radiant: bool,
    pub kills: u32,
    pub deaths: u32,
    pub assists: u32,
    pub kda: f64,
    #[serde(rename = "lastHits")]
    pub last_hits: u32,
    pub denies: u32,
    #[serde(rename = "goldPerMin")]
    pub gold_per_min: u32,
    #[serde(rename = "xpPerMin")]
    pub xp_per_min: u32,
    #[serde(rename = "heroDamage")]
    pub hero_damage: u64,
    #[serde(rename = "towerDamage")]
    pub tower_damage: u64,
    #[serde(rename = "heroHealing")]
    pub hero_healing: u64,
    #[serde(rename = "gameType")]
    pub game_type: String,
    /// Stable key for the mode filter, independent of the queue.
    #[serde(rename = "modeKey")]
    pub mode_key: String,
    #[serde(rename = "modeName")]
    pub mode_name: String,
    #[serde(rename = "lobbyName")]
    pub lobby_name: String,
    /// Whether this came from the ranked ladder, independent of the mode.
    pub ranked: bool,
    #[serde(rename = "partySize")]
    pub party_size: Option<u32>,
    pub abandoned: bool,
}

fn parse_match(m: &serde_json::Value, heroes: &HashMap<u32, DotaHero>) -> Option<DotaApiMatch> {
    let match_id = m.get("match_id").and_then(|v| v.as_u64())?;
    let hero_id = m.get("hero_id").and_then(|v| v.as_u64()).unwrap_or(0) as u32;
    let hero = heroes.get(&hero_id);

    // Slots 0-4 are Radiant, 128-132 are Dire; the winner comes back as a
    // single radiant_win flag, so the player's side decides the result.
    let player_slot = m.get("player_slot").and_then(|v| v.as_u64()).unwrap_or(0);
    let radiant = player_slot < 128;
    let radiant_win = m.get("radiant_win").and_then(|v| v.as_bool()).unwrap_or(false);

    let kills = m.get("kills").and_then(|v| v.as_u64()).unwrap_or(0) as u32;
    let deaths = m.get("deaths").and_then(|v| v.as_u64()).unwrap_or(0) as u32;
    let assists = m.get("assists").and_then(|v| v.as_u64()).unwrap_or(0) as u32;
    let game_mode = m.get("game_mode").and_then(|v| v.as_u64()).unwrap_or(0) as u32;
    let lobby_type = m.get("lobby_type").and_then(|v| v.as_u64()).unwrap_or(0) as u32;

    Some(DotaApiMatch {
        match_id,
        hero_id,
        hero_name: hero.map(|h| h.name.clone()).unwrap_or_else(|| format!("Hero {hero_id}")),
        hero_slug: hero.map(|h| h.slug.clone()).unwrap_or_default(),
        start_time: m.get("start_time").and_then(|v| v.as_i64()).unwrap_or(0),
        duration_seconds: m.get("duration").and_then(|v| v.as_u64()).unwrap_or(0) as u32,
        won: radiant == radiant_win,
        radiant,
        kills,
        deaths,
        assists,
        // Deathless games would divide by zero; the usual convention is to
        // treat them as one death.
        kda: (kills + assists) as f64 / deaths.max(1) as f64,
        last_hits: m.get("last_hits").and_then(|v| v.as_u64()).unwrap_or(0) as u32,
        denies: m.get("denies").and_then(|v| v.as_u64()).unwrap_or(0) as u32,
        gold_per_min: m.get("gold_per_min").and_then(|v| v.as_u64()).unwrap_or(0) as u32,
        xp_per_min: m.get("xp_per_min").and_then(|v| v.as_u64()).unwrap_or(0) as u32,
        hero_damage: m.get("hero_damage").and_then(|v| v.as_u64()).unwrap_or(0),
        tower_damage: m.get("tower_damage").and_then(|v| v.as_u64()).unwrap_or(0),
        hero_healing: m.get("hero_healing").and_then(|v| v.as_u64()).unwrap_or(0),
        game_type: classify(game_mode, lobby_type).to_string(),
        mode_key: game_mode_key(game_mode).to_string(),
        mode_name: game_mode_name(game_mode).to_string(),
        lobby_name: lobby_type_name(lobby_type).to_string(),
        ranked: is_ranked_lobby(lobby_type),
        party_size: m.get("party_size").and_then(|v| v.as_u64()).map(|p| p as u32),
        abandoned: m.get("leaver_status").and_then(|v| v.as_u64()).unwrap_or(0) > 1,
    })
}

pub async fn match_history(
    account_id: u64,
    cache: &SharedDotaHeroes,
    limit: usize,
) -> Result<Vec<DotaApiMatch>, String> {
    // OpenDota's /matches endpoint returns a lean projection by default:
    // no GPM, XPM, last hits or damage, and asking for any projection also
    // drops hero_id/kills/deaths/assists unless they are requested too. So
    // every field this view renders is listed explicitly — otherwise those
    // columns silently render as zero.
    const FIELDS: [&str; 14] = [
        "hero_id",
        "start_time",
        "kills",
        "deaths",
        "assists",
        "gold_per_min",
        "xp_per_min",
        "last_hits",
        "denies",
        "hero_damage",
        "tower_damage",
        "hero_healing",
        "party_size",
        "leaver_status",
    ];
    let projection: String = FIELDS.iter().map(|f| format!("&project={f}")).collect();
    let value = get_json(&format!("/players/{account_id}/matches?limit={limit}{projection}")).await?;
    let hero_map = heroes(cache).await;
    let mut out = Vec::new();
    if let Some(list) = value.as_array() {
        for m in list {
            if let Some(parsed) = parse_match(m, &hero_map) {
                out.push(parsed);
            }
        }
    }
    Ok(out)
}

// ---------- Single match detail ----------

#[derive(Debug, Clone, Serialize)]
pub struct ScoreboardPlayer {
    #[serde(rename = "accountId")]
    pub account_id: Option<u64>,
    pub name: String,
    #[serde(rename = "heroName")]
    pub hero_name: String,
    #[serde(rename = "heroSlug")]
    pub hero_slug: String,
    pub radiant: bool,
    pub kills: u32,
    pub deaths: u32,
    pub assists: u32,
    #[serde(rename = "lastHits")]
    pub last_hits: u32,
    pub denies: u32,
    #[serde(rename = "goldPerMin")]
    pub gold_per_min: u32,
    #[serde(rename = "xpPerMin")]
    pub xp_per_min: u32,
    #[serde(rename = "heroDamage")]
    pub hero_damage: u64,
    #[serde(rename = "netWorth")]
    pub net_worth: u64,
    pub level: u32,
    #[serde(rename = "isMe")]
    pub is_me: bool,
    pub items: Vec<u32>,
}

#[derive(Debug, Clone, Serialize)]
pub struct DotaMatchDetail {
    #[serde(rename = "matchId")]
    pub match_id: u64,
    #[serde(rename = "durationSeconds")]
    pub duration_seconds: u32,
    #[serde(rename = "startTime")]
    pub start_time: i64,
    #[serde(rename = "radiantWin")]
    pub radiant_win: bool,
    #[serde(rename = "radiantScore")]
    pub radiant_score: u32,
    #[serde(rename = "direScore")]
    pub dire_score: u32,
    #[serde(rename = "modeName")]
    pub mode_name: String,
    #[serde(rename = "lobbyName")]
    pub lobby_name: String,
    #[serde(rename = "gameType")]
    pub game_type: String,
    pub players: Vec<ScoreboardPlayer>,
}

pub async fn match_detail(
    match_id: u64,
    me: Option<u64>,
    cache: &SharedDotaHeroes,
) -> Result<DotaMatchDetail, String> {
    let m = get_json(&format!("/matches/{match_id}")).await?;
    let hero_map = heroes(cache).await;

    let game_mode = m.get("game_mode").and_then(|v| v.as_u64()).unwrap_or(0) as u32;
    let lobby_type = m.get("lobby_type").and_then(|v| v.as_u64()).unwrap_or(0) as u32;

    let mut players = Vec::new();
    if let Some(list) = m.get("players").and_then(|p| p.as_array()) {
        for p in list {
            let hero_id = p.get("hero_id").and_then(|v| v.as_u64()).unwrap_or(0) as u32;
            let hero = hero_map.get(&hero_id);
            let slot = p.get("player_slot").and_then(|v| v.as_u64()).unwrap_or(0);
            let account_id = p.get("account_id").and_then(|v| v.as_u64());

            let mut items = Vec::new();
            for key in ["item_0", "item_1", "item_2", "item_3", "item_4", "item_5"] {
                if let Some(id) = p.get(key).and_then(|v| v.as_u64()) {
                    if id > 0 {
                        items.push(id as u32);
                    }
                }
            }

            players.push(ScoreboardPlayer {
                account_id,
                // Anonymous profiles legitimately have no name attached.
                name: p.get("personaname").and_then(|v| v.as_str()).unwrap_or("Anonymous").to_string(),
                hero_name: hero.map(|h| h.name.clone()).unwrap_or_else(|| format!("Hero {hero_id}")),
                hero_slug: hero.map(|h| h.slug.clone()).unwrap_or_default(),
                radiant: slot < 128,
                kills: p.get("kills").and_then(|v| v.as_u64()).unwrap_or(0) as u32,
                deaths: p.get("deaths").and_then(|v| v.as_u64()).unwrap_or(0) as u32,
                assists: p.get("assists").and_then(|v| v.as_u64()).unwrap_or(0) as u32,
                last_hits: p.get("last_hits").and_then(|v| v.as_u64()).unwrap_or(0) as u32,
                denies: p.get("denies").and_then(|v| v.as_u64()).unwrap_or(0) as u32,
                gold_per_min: p.get("gold_per_min").and_then(|v| v.as_u64()).unwrap_or(0) as u32,
                xp_per_min: p.get("xp_per_min").and_then(|v| v.as_u64()).unwrap_or(0) as u32,
                hero_damage: p.get("hero_damage").and_then(|v| v.as_u64()).unwrap_or(0),
                net_worth: p.get("net_worth").and_then(|v| v.as_u64()).unwrap_or(0),
                level: p.get("level").and_then(|v| v.as_u64()).unwrap_or(0) as u32,
                is_me: me.is_some() && account_id == me,
                items,
            });
        }
    }

    Ok(DotaMatchDetail {
        match_id,
        duration_seconds: m.get("duration").and_then(|v| v.as_u64()).unwrap_or(0) as u32,
        start_time: m.get("start_time").and_then(|v| v.as_i64()).unwrap_or(0),
        radiant_win: m.get("radiant_win").and_then(|v| v.as_bool()).unwrap_or(false),
        radiant_score: m.get("radiant_score").and_then(|v| v.as_u64()).unwrap_or(0) as u32,
        dire_score: m.get("dire_score").and_then(|v| v.as_u64()).unwrap_or(0) as u32,
        mode_name: game_mode_name(game_mode).to_string(),
        lobby_name: lobby_type_name(lobby_type).to_string(),
        game_type: classify(game_mode, lobby_type).to_string(),
        players,
    })
}

// ---------- Steam search ----------

#[derive(Debug, Clone, Serialize)]
pub struct DotaProfile {
    #[serde(rename = "accountId")]
    pub account_id: u64,
    pub personaname: String,
    pub avatar: Option<String>,
}

pub async fn search_players(query: &str) -> Result<Vec<DotaProfile>, String> {
    let q: String = query
        .bytes()
        .map(|b| match b {
            b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => (b as char).to_string(),
            b' ' => "+".to_string(),
            other => format!("%{other:02X}"),
        })
        .collect();

    let value = get_json(&format!("/search?q={q}")).await?;
    let mut out = Vec::new();
    if let Some(list) = value.as_array() {
        for p in list.iter().take(20) {
            let Some(account_id) = p.get("account_id").and_then(|v| v.as_u64()) else { continue };
            out.push(DotaProfile {
                account_id,
                personaname: p.get("personaname").and_then(|v| v.as_str()).unwrap_or("(no name)").to_string(),
                avatar: p.get("avatarfull").or_else(|| p.get("avatar")).and_then(|v| v.as_str()).map(String::from),
            });
        }
    }
    Ok(out)
}

// ---------- Summary ----------

#[derive(Debug, Clone, Serialize)]
pub struct DotaApiSummary {
    pub matches: usize,
    pub wins: usize,
    pub losses: usize,
    #[serde(rename = "winRate")]
    pub win_rate: f64,
    pub kills: u32,
    pub deaths: u32,
    pub assists: u32,
    pub kda: f64,
    #[serde(rename = "avgGpm")]
    pub avg_gpm: u32,
    #[serde(rename = "avgXpm")]
    pub avg_xpm: u32,
    #[serde(rename = "avgLastHits")]
    pub avg_last_hits: u32,
}

pub fn summarize(matches: &[DotaApiMatch]) -> DotaApiSummary {
    let n = matches.len();
    let wins = matches.iter().filter(|m| m.won).count();
    let (mut k, mut d, mut a) = (0u32, 0u32, 0u32);
    let (mut gpm, mut xpm, mut lh) = (0u64, 0u64, 0u64);
    for m in matches {
        k += m.kills;
        d += m.deaths;
        a += m.assists;
        gpm += m.gold_per_min as u64;
        xpm += m.xp_per_min as u64;
        lh += m.last_hits as u64;
    }
    let div = n.max(1) as u64;
    DotaApiSummary {
        matches: n,
        wins,
        losses: n - wins,
        win_rate: if n == 0 { 0.0 } else { (wins as f64 / n as f64) * 100.0 },
        kills: k,
        deaths: d,
        assists: a,
        kda: (k + a) as f64 / d.max(1) as f64,
        avg_gpm: (gpm / div) as u32,
        avg_xpm: (xpm / div) as u32,
        avg_last_hits: (lh / div) as u32,
    }
}

// ---------- Automatic game-type tagging ----------

/// Asks OpenDota what game types the linked account's recent matches were,
/// keyed by match id.
///
/// GSI never says which mode you are in — Valve's map block carries the
/// clock, the game state and the match id, but no `game_mode` or
/// `lobby_type` — so a match finalized from GSI alone lands in history as
/// "unspecified" and used to need tagging by hand.
///
/// This is one lean request covering the whole recent tail rather than a
/// lookup per match: `/matches` on a player is cheap when projected, while
/// `/matches/{id}` pulls a full ten-player record for a single answer.
async fn recent_game_types(account_id: u64, limit: usize) -> Result<HashMap<String, String>, String> {
    let path = format!(
        "/players/{account_id}/matches?limit={limit}&project=match_id&project=game_mode&project=lobby_type"
    );
    let value = get_json(&path).await?;
    let rows = value.as_array().ok_or("OpenDota returned an unexpected shape")?;

    Ok(rows
        .iter()
        .filter_map(|m| {
            let id = m.get("match_id").and_then(|v| v.as_u64())?;
            let mode = m.get("game_mode").and_then(|v| v.as_u64()).unwrap_or(0) as u32;
            let lobby = m.get("lobby_type").and_then(|v| v.as_u64()).unwrap_or(0) as u32;
            Some((id.to_string(), classify(mode, lobby).to_string()))
        })
        .collect())
}

/// Fills in the game type of any locally tracked match still marked
/// "unspecified", and returns how many were resolved.
///
/// Only untagged matches are touched: a type the player set by hand is
/// their call and is left alone. OpenDota takes a few minutes to ingest a
/// finished game, so this is retried periodically rather than once.
pub async fn backfill_game_types() -> Result<usize, String> {
    let Some(account_id) = load_link().account_id else {
        return Ok(0);
    };

    let mut history = crate::storage::load_history();
    if !history.iter().any(|m| m.game_type == "unspecified") {
        return Ok(0);
    }

    let types = recent_game_types(account_id, 50).await?;

    let mut filled = 0;
    for entry in history.iter_mut() {
        if entry.game_type != "unspecified" {
            continue;
        }
        if let Some(t) = types.get(&entry.matchid) {
            entry.game_type = t.clone();
            filled += 1;
        }
    }

    if filled > 0 {
        crate::storage::save_history(&history);
    }
    Ok(filled)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn turbo_is_decided_before_ranked() {
        // Ranked Turbo exists (mode 23 in lobby 7). Checking the lobby first
        // would file those as plain "ranked" and the overlay would then use
        // full-length lotus and rune timings in a game that runs at double
        // speed — late every time, which is the one thing a warning must not
        // be.
        assert_eq!(classify(23, 7), "turbo");
        assert_eq!(classify(23, 0), "turbo");
        assert_eq!(classify(22, 7), "ranked");
        assert_eq!(classify(22, 0), "all_pick");
        assert_eq!(classify(1, 0), "all_pick");
        assert_eq!(classify(2, 1), "other");
    }

    #[test]
    fn every_classification_is_one_the_ui_knows() {
        // History entries are filed under these names, and the type filter
        // matches on them exactly. A new label here would silently vanish
        // from every filter.
        for (mode, lobby) in [(23u32, 7u32), (22, 7), (22, 0), (1, 0), (2, 1), (18, 0)] {
            let t = classify(mode, lobby);
            assert!(
                crate::model::GAME_TYPES.contains(&t),
                "classify({mode},{lobby}) produced {t}, which the UI has no filter for"
            );
        }
    }

    #[test]
    fn mode_and_queue_are_independent() {
        // The whole point of splitting them. A ranked Turbo game is both
        // ranked and Turbo; the old single bucket could only say one.
        assert_eq!(game_mode_key(23), "turbo");
        assert!(is_ranked_lobby(7));

        // Same mode, different queues.
        assert_eq!(game_mode_key(22), "all_pick");
        assert!(is_ranked_lobby(7));
        assert!(!is_ranked_lobby(0));

        // Modes 1 and 22 are both All Pick to a player: 22 is `all_draft`,
        // which is what the ranked All Pick queue actually runs. Splitting
        // them would put a player's games in two filters for no reason.
        assert_eq!(game_mode_key(1), game_mode_key(22));
        assert_eq!(game_mode_name(1), game_mode_name(22));
    }

    #[test]
    fn only_the_ranked_ladder_counts_as_ranked() {
        // Battle Cup and tournaments are competitive but are not the ranked
        // ladder, and folding them in would inflate a ranked win rate.
        for ranked in [5, 6, 7] {
            assert!(is_ranked_lobby(ranked), "lobby {ranked} is a ranked queue");
        }
        for other in [0, 1, 2, 4, 8, 9, 12, 14] {
            assert!(!is_ranked_lobby(other), "lobby {other} is not the ranked ladder");
        }
    }

    #[test]
    fn known_modes_have_names() {
        // Every id OpenDota's constants table lists should resolve. The old
        // table stopped at a handful and everything else read "Unknown Mode"
        // even though it had a perfectly good name.
        for id in [1u32, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25] {
            assert_ne!(game_mode_name(id), "Unknown Mode", "mode {id} should have a name");
        }
        assert_eq!(game_mode_name(999), "Unknown Mode");
    }
}
