//! User preferences that aren't match data: the favourite hero for each
//! game, saved item builds, and the overlay's appearance.
//!
//! Kept in one `prefs.json` beside the history rather than scattered across
//! files, since it's all small, all read at startup, and all written by the
//! same settings screens. Unknown fields survive a round trip through serde
//! defaults, so an older build won't wipe settings a newer one wrote.

use serde::{Deserialize, Serialize};

use crate::storage::log_dir;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct OverlaySettings {
    /// 0.25–1.0. Below a quarter the overlay is effectively invisible, which
    /// reads as a bug rather than a setting.
    pub opacity: f64,
    /// 0.75–1.5 — lets the overlay stay legible on a 1440p/4K screen.
    pub scale: f64,
    /// "top-left" | "top-right" | "bottom-left" | "bottom-right"
    pub corner: String,
    #[serde(rename = "clickThrough")]
    pub click_through: bool,
    /// Panels the overlay draws, so it can be trimmed to just what's wanted.
    #[serde(rename = "showStats")]
    pub show_stats: bool,
    #[serde(rename = "showRoshan")]
    pub show_roshan: bool,
    #[serde(rename = "showCheckpoints")]
    pub show_checkpoints: bool,
    #[serde(rename = "showItems")]
    pub show_items: bool,
    #[serde(rename = "showDeaths")]
    pub show_deaths: bool,
}

impl Default for OverlaySettings {
    fn default() -> Self {
        OverlaySettings {
            opacity: 0.85,
            scale: 1.0,
            corner: "top-left".to_string(),
            click_through: true,
            show_stats: true,
            show_roshan: true,
            show_checkpoints: true,
            show_items: false,
            show_deaths: false,
        }
    }
}

impl OverlaySettings {
    /// Values arrive from the UI, so they're clamped rather than trusted —
    /// an opacity of 0 would produce an invisible window with no way back.
    fn sanitized(mut self) -> Self {
        self.opacity = self.opacity.clamp(0.25, 1.0);
        self.scale = self.scale.clamp(0.75, 1.5);
        if !matches!(self.corner.as_str(), "top-left" | "top-right" | "bottom-left" | "bottom-right") {
            self.corner = "top-left".to_string();
        }
        self
    }
}

/// A saved item build for one hero. Items are stored by their internal name
/// (e.g. "mage_slayer") so icons resolve from Valve's CDN without a lookup
/// table, and the same string works for the key-item matcher.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Build {
    pub id: String,
    pub game: String,
    /// Dota hero slug ("juggernaut") or Deadlock hero name.
    pub hero: String,
    pub name: String,
    #[serde(default)]
    pub items: Vec<String>,
    #[serde(default)]
    pub notes: String,
    #[serde(rename = "updatedAt", default)]
    pub updated_at: String,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct Favorites {
    /// Dota hero slug, e.g. "juggernaut".
    #[serde(default)]
    pub dota: Option<String>,
    /// Deadlock hero name, e.g. "Yamato".
    #[serde(default)]
    pub deadlock: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Prefs {
    #[serde(default)]
    pub favorites: Favorites,
    #[serde(default)]
    pub builds: Vec<Build>,
    #[serde(default)]
    pub overlay: OverlaySettings,
}

impl Default for Prefs {
    fn default() -> Self {
        Prefs { favorites: Favorites::default(), builds: Vec::new(), overlay: OverlaySettings::default() }
    }
}

fn prefs_file() -> std::path::PathBuf {
    log_dir().join("prefs.json")
}

pub fn load() -> Prefs {
    match std::fs::read_to_string(prefs_file()) {
        Ok(s) => serde_json::from_str(&s).unwrap_or_default(),
        Err(_) => Prefs::default(),
    }
}

pub fn save(prefs: &Prefs) {
    let _ = std::fs::create_dir_all(log_dir());
    if let Ok(json) = serde_json::to_string_pretty(prefs) {
        let _ = std::fs::write(prefs_file(), json);
    }
}

pub fn set_favorite(game: &str, hero: Option<String>) -> Prefs {
    let mut p = load();
    match game {
        "deadlock" => p.favorites.deadlock = hero,
        _ => p.favorites.dota = hero,
    }
    save(&p);
    p
}

pub fn save_overlay(settings: OverlaySettings) -> Prefs {
    let mut p = load();
    p.overlay = settings.sanitized();
    save(&p);
    p
}

/// Inserts a build, or replaces the existing one with the same id.
pub fn upsert_build(mut build: Build) -> Prefs {
    let mut p = load();
    build.updated_at = chrono::Local::now().to_rfc3339();
    if build.id.trim().is_empty() {
        build.id = uuid::Uuid::new_v4().to_string();
    }
    match p.builds.iter_mut().find(|b| b.id == build.id) {
        Some(existing) => *existing = build,
        None => p.builds.push(build),
    }
    save(&p);
    p
}

pub fn delete_build(id: &str) -> Prefs {
    let mut p = load();
    p.builds.retain(|b| b.id != id);
    save(&p);
    p
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn overlay_values_are_clamped() {
        let wild = OverlaySettings { opacity: 0.0, scale: 9.0, corner: "middle".into(), ..Default::default() };
        let safe = wild.sanitized();
        // An opacity of 0 would leave an invisible window and no way back.
        assert_eq!(safe.opacity, 0.25);
        assert_eq!(safe.scale, 1.5);
        assert_eq!(safe.corner, "top-left");
    }

    #[test]
    fn defaults_are_sane() {
        let d = OverlaySettings::default();
        assert!(d.click_through, "click-through must default on, or the overlay eats game input");
        assert_eq!(d.clone().sanitized().opacity, d.opacity);
    }
}
