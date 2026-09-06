//! "Popular builds" — what people actually buy on a hero, per game.
//!
//! # Where this comes from, and where it deliberately doesn't
//!
//! Sites like dota2protracker and Statlocker publish curated builds, and
//! that curation *is* their product. Lifting it into another app would
//! breach their terms, so nothing here touches them.
//!
//! Instead this uses the same public, documented APIs the rest of the app
//! already reads:
//!
//! - **Dota** — OpenDota's `/heroes/{id}/itemPopularity`, which reports how
//!   often each item is bought on a hero, split into start / early / mid /
//!   late game. That is real aggregate behaviour across a large sample.
//! - **Deadlock** — deadlock-api's `/v1/analytics/build-item-stats`, which
//!   reports how many published builds include each item for a hero.
//!
//! So these are "what most people build", drawn from aggregate data — not
//! somebody's hand-written guide. The UI says so, rather than implying a
//! pro wrote it.

use std::collections::HashMap;
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use serde::Serialize;

// ---------- Dota ----------

#[derive(Debug, Clone, Serialize)]
pub struct PopularItem {
    /// Internal name, e.g. "black_king_bar" — resolves against Valve's icon
    /// CDN and matches the live key-item tracker.
    pub key: String,
    /// Human-readable name, e.g. "Black King Bar".
    pub name: String,
    /// How many sampled matches bought it, for a share bar in the UI.
    pub count: u64,
}

#[derive(Debug, Clone, Serialize)]
pub struct PopularBuild {
    pub phase: String,
    pub items: Vec<PopularItem>,
}

/// Item id -> (internal name, display name). Around 500 entries and only
/// changes on a patch, so it's fetched once and kept.
#[derive(Default)]
pub struct ItemNameCache {
    names: HashMap<u64, (String, String)>,
    fetched_at: Option<Instant>,
}

pub type SharedItemNames = Arc<Mutex<ItemNameCache>>;

async fn dota_item_names(cache: &SharedItemNames) -> HashMap<u64, (String, String)> {
    const TTL: Duration = Duration::from_secs(60 * 60 * 24);
    {
        let c = cache.lock().unwrap();
        if let Some(at) = c.fetched_at {
            if at.elapsed() < TTL && !c.names.is_empty() {
                return c.names.clone();
            }
        }
    }

    let Ok(value) = crate::dota_api::get_json_public("/constants/items").await else {
        return cache.lock().unwrap().names.clone();
    };

    let mut map = HashMap::new();
    if let Some(obj) = value.as_object() {
        for (key, v) in obj {
            let Some(id) = v.get("id").and_then(|i| i.as_u64()) else { continue };
            let dname = v.get("dname").and_then(|d| d.as_str()).unwrap_or(key).to_string();
            map.insert(id, (key.clone(), dname));
        }
    }

    if !map.is_empty() {
        let mut c = cache.lock().unwrap();
        c.names = map.clone();
        c.fetched_at = Some(Instant::now());
    }
    map
}

/// Top `limit` items per game phase for one hero.
pub async fn dota_builds(
    hero_id: u32,
    cache: &SharedItemNames,
    limit: usize,
) -> Result<Vec<PopularBuild>, String> {
    let value = crate::dota_api::get_json_public(&format!("/heroes/{hero_id}/itemPopularity")).await?;
    let names = dota_item_names(cache).await;

    // OpenDota's own ordering of a match's shopping trip.
    let phases = [
        ("start_game_items", "Starting"),
        ("early_game_items", "Early game"),
        ("mid_game_items", "Mid game"),
        ("late_game_items", "Late game"),
    ];

    let mut out = Vec::new();
    for (key, label) in phases {
        let Some(obj) = value.get(key).and_then(|v| v.as_object()) else { continue };

        let mut items: Vec<PopularItem> = obj
            .iter()
            .filter_map(|(id, count)| {
                let id: u64 = id.parse().ok()?;
                let count = count.as_u64()?;
                let (key, name) = names.get(&id)?;
                Some(PopularItem { key: key.clone(), name: name.clone(), count })
            })
            .collect();

        items.sort_by(|a, b| b.count.cmp(&a.count));
        items.truncate(limit);

        if !items.is_empty() {
            out.push(PopularBuild { phase: label.to_string(), items });
        }
    }

    Ok(out)
}

// ---------- Deadlock ----------

#[derive(Debug, Clone, Serialize)]
pub struct DeadlockPopularItem {
    #[serde(rename = "itemId")]
    pub item_id: u64,
    pub name: String,
    pub builds: u64,
}

/// Deadlock item id -> display name, from the same assets endpoint the hero
/// list comes from.
pub async fn deadlock_item_names() -> HashMap<u64, String> {
    let mut map = HashMap::new();
    let Ok(value) = crate::deadlock::get_json_public("/v1/assets/items").await else {
        return map;
    };
    if let Some(list) = value.as_array() {
        for it in list {
            let Some(id) = it.get("id").and_then(|v| v.as_u64()) else { continue };
            let name = it
                .get("name")
                .and_then(|v| v.as_str())
                .unwrap_or("Unknown item")
                .to_string();
            map.insert(id, name);
        }
    }
    map
}

pub async fn deadlock_builds(hero_id: u32, limit: usize) -> Result<Vec<DeadlockPopularItem>, String> {
    let value =
        crate::deadlock::get_json_public(&format!("/v1/analytics/build-item-stats?hero_id={hero_id}")).await?;
    let names = deadlock_item_names().await;

    let mut items: Vec<DeadlockPopularItem> = value
        .as_array()
        .map(|list| {
            list.iter()
                .filter_map(|row| {
                    let item_id = row.get("item_id").and_then(|v| v.as_u64())?;
                    let builds = row.get("builds").and_then(|v| v.as_u64())?;
                    Some(DeadlockPopularItem {
                        item_id,
                        name: names.get(&item_id).cloned().unwrap_or_else(|| format!("Item {item_id}")),
                        builds,
                    })
                })
                .collect()
        })
        .unwrap_or_default();

    items.sort_by(|a, b| b.builds.cmp(&a.builds));
    items.truncate(limit);
    Ok(items)
}
