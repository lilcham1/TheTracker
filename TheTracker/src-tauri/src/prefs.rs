//! User preferences that aren't match data: the favourite hero for each
//! game, saved item builds, and the overlay's appearance.
//!
//! Kept in one `prefs.json` beside the history rather than scattered across
//! files, since it's all small, all read at startup, and all written by the
//! same settings screens. Serde defaults mean an older build won't wipe
//! settings a newer one wrote.

use serde::{Deserialize, Serialize};

use crate::storage::log_dir;

/// Which events the Dota overlay counts down to.
///
/// Three of them, all arithmetic on the game clock the player can already
/// see: rune spawns, lotuses, and the :53 stack pull. None of it reveals an
/// opponent's state.
///
/// The list has been cut down deliberately, and each removal has its own
/// reason. The player's own scoreboard line went because the overlay shows
/// a countdown in the last five seconds before an event and nothing at any
/// other time, and a line pinned there permanently is exactly the sort of
/// thing you stop seeing. Roshan went because his timer can only start from
/// a death the player happened to witness, so it would be missing whenever
/// it mattered most; his deaths are still recorded in match history. The
/// day/night flip went because it is a five-minute cycle you can read off
/// the clock, and it fires all game whether or not anything hangs on it.
///
/// A prefs.json written before any of those removals has its extra keys
/// ignored.
///
/// The overlay is Dota-only: Deadlock publishes no live feed, so there is
/// nothing to drive a timer from.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DotaPanels {
    /// Bounty / water / power / wisdom rune countdowns.
    #[serde(default = "yes")]
    pub runes: bool,
    /// Healing lotus spawns.
    #[serde(default = "yes")]
    pub lotus: bool,
    /// Neutral camp stack timer (the :53 pull).
    #[serde(default = "yes")]
    pub stacks: bool,
}

impl Default for DotaPanels {
    fn default() -> Self {
        DotaPanels { runes: true, lotus: true, stacks: true }
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
    /// Show the overlay by itself when a match starts and hide it when the
    /// match ends. On by default: an overlay you have to remember to open
    /// is one you forget to open.
    #[serde(default = "yes")]
    pub auto: bool,
    /// Which display to put the overlay on, by name. Empty means "follow the
    /// main window".
    ///
    /// Following the app window is the wrong default for anyone who plays on
    /// one screen and keeps the tracker on another — the overlay lands where
    /// the tracker is, not where the game is. There is no reliable way to ask
    /// which monitor Dota is fullscreen on, so this is an explicit choice
    /// rather than a guess.
    #[serde(default)]
    pub monitor: String,
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
            auto: true,
            monitor: String::new(),
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
        assert!(d.dota.lotus, "lotus warnings are on by default like every other timer");
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
        assert!(p.overlay.dota.stacks, "missing panel block falls back to defaults");
        assert!(p.overlay.dota.runes, "new panels default on for existing users");
    }

    #[test]
    fn removed_panel_keys_do_not_break_an_existing_prefs_file() {
        // `stats`, `roshan` and `daynight` were all panels once. Anyone who
        // used the app before they were dropped has them sitting in
        // prefs.json, and serde must ignore them rather than refuse the whole
        // file and reset every other setting.
        let old = r#"{"overlay":{"opacity":0.5,"dota":{"stats":true,"roshan":false,"runes":true,"stacks":true,"daynight":true}}}"#;
        let p: Prefs = serde_json::from_str(old).expect("prefs with removed keys should still parse");
        assert_eq!(p.overlay.opacity, 0.5);
        assert!(p.overlay.dota.runes);
    }
}
