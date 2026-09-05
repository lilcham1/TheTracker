//! User preferences that aren't match data: the favourite hero for each
//! game, saved item builds, and the overlay's appearance.
//!
//! Kept in one `prefs.json` beside the history rather than scattered across
//! files, since it's all small, all read at startup, and all written by the
//! same settings screens. Serde defaults mean an older build won't wipe
//! settings a newer one wrote.

use serde::{Deserialize, Serialize};

use crate::storage::log_dir;

/// Panels the Dota overlay can draw.
///
/// These are the timers every established Dota overlay shows, and all of
/// them are arithmetic on the game clock the player can already see —
/// Roshan's respawn window, rune spawns, the :53 stack pull, and the
/// day/night flip. None of it reveals an opponent's state.
///
/// The overlay is Dota-only: Deadlock publishes no live feed, so there is
/// nothing to drive a timer or a stats line from.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DotaPanels {
    /// Your own scoreboard line: kills, deaths, last hits, gold lost.
    #[serde(default = "yes")]
    pub stats: bool,
    #[serde(default = "yes")]
    pub roshan: bool,
    /// Bounty / water / power / wisdom rune countdowns.
    #[serde(default = "yes")]
    pub runes: bool,
    /// Neutral camp stack timer (the :53 pull).
    #[serde(default = "yes")]
    pub stacks: bool,
    #[serde(default = "yes")]
    pub daynight: bool,
}

impl Default for DotaPanels {
    fn default() -> Self {
        DotaPanels { stats: true, roshan: true, runes: true, stacks: true, daynight: true }
    }
}

fn yes() -> bool {
    true
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct OverlaySettings {
    /// 0.25–1.0. Below a quarter the overlay is effectively invisible, which
    /// reads as a bug rather than a setting.
    #[serde(default = "default_opacity")]
    pub opacity: f64,
    /// 0.75–1.5 — keeps the overlay legible on a 1440p/4K screen.
    #[serde(default = "default_scale")]
    pub scale: f64,
    /// "top-left" | "top-right" | "bottom-left" | "bottom-right"
    #[serde(default = "default_corner")]
    pub corner: String,
    #[serde(rename = "clickThrough", default = "yes")]
    pub click_through: bool,
    #[serde(default)]
    pub dota: DotaPanels,
}

fn default_opacity() -> f64 {
    0.85
}
fn default_scale() -> f64 {
    1.0
}
fn default_corner() -> String {
    "top-left".to_string()
}

impl Default for OverlaySettings {
    fn default() -> Self {
        OverlaySettings {
            opacity: default_opacity(),
            scale: default_scale(),
            corner: default_corner(),
            click_through: true,
            dota: DotaPanels::default(),
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
            self.corner = default_corner();
        }
        self
    }
}

/// A saved item build for one hero. Dota items are stored by their internal
/// name ("mage_slayer"), which is what Valve's icon CDN is keyed on and what
/// the live key-item matcher compares against. Deadlock builds are free text
/// because Deadlock has no equivalent public icon set.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Build {
    pub id: String,
    pub game: String,
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
    #[serde(default)]
    pub dota: Option<String>,
    #[serde(default)]
    pub deadlock: Option<String>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct Prefs {
    #[serde(default)]
    pub favorites: Favorites,
    #[serde(default)]
    pub builds: Vec<Build>,
    #[serde(default)]
    pub overlay: OverlaySettings,
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
        assert!(d.dota.roshan, "Roshan timer is the point of the Dota overlay");

    }

    #[test]
    fn overlay_is_dota_only() {
        // The overlay covers Dota alone. Deadlock publishes no live feed, so
        // there was nothing honest to put behind a Deadlock panel.
        let d = OverlaySettings::default();
        assert!(d.dota.runes);
        assert!(d.dota.stacks);
    }

    #[test]
    fn older_prefs_files_still_load() {
        // A prefs.json written before per-game panels existed must not wipe
        // the user's opacity/corner just because the panel keys moved.
        let old = r#"{"favorites":{"dota":"juggernaut"},"builds":[],"overlay":{"opacity":0.5,"corner":"bottom-right"}}"#;
        let p: Prefs = serde_json::from_str(old).expect("old prefs should still parse");
        assert_eq!(p.favorites.dota.as_deref(), Some("juggernaut"));
        assert_eq!(p.overlay.opacity, 0.5);
        assert_eq!(p.overlay.corner, "bottom-right");
        assert!(p.overlay.dota.roshan, "missing panel block falls back to defaults");
        assert!(p.overlay.dota.runes, "new panels default on for existing users");
    }
}
