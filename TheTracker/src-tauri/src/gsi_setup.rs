//! Getting Dota to actually send us data, without asking the player to copy
//! a file into their game folder.
//!
//! GSI needs two things: a `.cfg` in Dota's `gamestate_integration` folder
//! naming the port to post to, and `-gamestateintegration` in the game's
//! launch options. Until now the README asked the player to do the first by
//! hand. Nobody does — this machine had the app installed for days with the
//! flag already set in Steam and no config file, so the Live tab and every
//! locally tracked session sat empty and nothing said why.
//!
//! So the app writes the config itself, on every launch, and reports what it
//! could not do.
//!
//! # Why the app and not the installer
//!
//! An installer runs once, at a moment when Dota may not be installed yet,
//! on a drive the player may later move the game off. Steam libraries move,
//! games get installed afterwards, and "verify integrity of game files" can
//! clear the folder. Checking at every launch survives all of that; an
//! installer step survives none of it.
//!
//! # What is deliberately not automated
//!
//! The launch option lives in Steam's `localconfig.vdf`, which Steam holds
//! open and rewrites from memory when it exits. Editing it under a running
//! Steam would be silently reverted at best and would corrupt the file at
//! worst, so that half is detected and reported for the player to set.

use std::path::{Path, PathBuf};

use serde::Serialize;

use crate::gsi::GSI_PORT;

/// Named for the app so it sits alongside other tools' configs rather than
/// competing with them — Dota reads every file in the folder.
const CFG_NAME: &str = "gamestate_integration_thetracker.cfg";

/// Generated rather than shipped as a file, so it can never disagree with
/// the port the listener actually binds.
fn config_text() -> String {
    format!(
        r#""Dota 2 Integration Configuration"
{{
    "uri"           "http://localhost:{GSI_PORT}/"
    "timeout"       "5.0"
    "buffer"        "0.1"
    "throttle"      "0.1"
    "heartbeat"     "30.0"
    "data"
    {{
        "provider"      "1"
        "map"           "1"
        "player"        "1"
        "hero"          "1"
        "abilities"     "0"
        "items"         "1"
    }}
}}
"#
    )
}

#[derive(Debug, Clone, Serialize)]
pub struct GsiStatus {
    /// Every Dota install found, across every Steam library.
    #[serde(rename = "cfgDirs")]
    pub cfg_dirs: Vec<String>,
    /// True when the config is present and matches the port we listen on.
    pub installed: bool,
    /// Dota was found but the config was stale — a port change, or an older
    /// hand-copied file.
    pub stale: bool,
    /// Whether any Steam user on this machine has `-gamestateintegration`.
    /// `None` means the launch options could not be read at all.
    #[serde(rename = "launchOption")]
    pub launch_option: Option<bool>,
    pub port: u16,
    /// When the config was last written, as Unix seconds — the point from
    /// which every Dota match should have reached the app. Comparing
    /// OpenDota's list against local history from here shows how many games
    /// the app missed; it is rewritten only when missing or stale, so this
    /// is stable across launches.
    #[serde(rename = "installedAt")]
    pub installed_at: Option<u64>,
}

/// Steam spreads games across libraries listed in `libraryfolders.vdf`. Only
/// looking under the Steam root finds nothing for anyone whose games are on
/// a second drive, which is most people with an SSD and a spinning disk.
fn steam_libraries() -> Vec<PathBuf> {
    let Some(root) = crate::steam::steam_path() else { return Vec::new() };
    let mut libs = vec![root.clone()];

    let vdf = root.join("steamapps").join("libraryfolders.vdf");
    if let Ok(text) = std::fs::read_to_string(&vdf) {
        for line in text.lines() {
            let trimmed = line.trim();
            if !trimmed.starts_with("\"path\"") {
                continue;
            }
            // "path"		"D:\\SteamLibrary"
            if let Some(value) = trimmed.split('"').nth(3) {
                let cleaned = value.replace("\\\\", "\\");
                let p = PathBuf::from(&cleaned);
                if p.is_dir() && !libs.contains(&p) {
                    libs.push(p);
                }
            }
        }
    }
    libs
}

fn cfg_dir_in(library: &Path) -> Option<PathBuf> {
    let cfg = library
        .join("steamapps")
        .join("common")
        .join("dota 2 beta")
        .join("game")
        .join("dota")
        .join("cfg");
    cfg.is_dir().then_some(cfg)
}

/// Every Dota `cfg` folder on this machine. More than one is possible if the
/// game has been installed to two libraries.
pub fn dota_cfg_dirs() -> Vec<PathBuf> {
    steam_libraries().iter().filter_map(|l| cfg_dir_in(l)).collect()
}

/// Reads `-gamestateintegration` out of Steam's per-user config.
///
/// Read-only on purpose: Steam keeps this file open and rewrites it from
/// memory on exit, so writing to it while Steam runs achieves nothing.
fn launch_option_set() -> Option<bool> {
    let root = crate::steam::steam_path()?;
    let userdata = root.join("userdata");
    let entries = std::fs::read_dir(&userdata).ok()?;

    let mut saw_any = false;
    for entry in entries.flatten() {
        let local = entry.path().join("config").join("localconfig.vdf");
        let Ok(text) = std::fs::read_to_string(&local) else { continue };
        saw_any = true;
        // Dota is appid 570. Its LaunchOptions sit inside that block; the
        // file is large, so this looks at the window after the id rather
        // than parsing the whole VDF.
        if let Some(at) = text.find("\"570\"") {
            let window = &text[at..text.len().min(at + 4000)];
            if let Some(opts_at) = window.find("\"LaunchOptions\"") {
                let tail = &window[opts_at..];
                if let Some(value) = tail.split('"').nth(3) {
                    if value.contains("-gamestateintegration") {
                        return Some(true);
                    }
                }
            }
        }
    }
    saw_any.then_some(false)
}

pub fn status() -> GsiStatus {
    let dirs = dota_cfg_dirs();
    let wanted = config_text();

    let mut installed = false;
    let mut stale = false;
    let mut installed_at: Option<u64> = None;
    for dir in &dirs {
        let path = dir.join("gamestate_integration").join(CFG_NAME);
        match std::fs::read_to_string(&path) {
            Ok(found) if found.trim() == wanted.trim() => {
                installed = true;
                // The earliest install wins if Dota is in two libraries.
                let written = std::fs::metadata(&path)
                    .and_then(|m| m.modified())
                    .ok()
                    .and_then(|t| t.duration_since(std::time::UNIX_EPOCH).ok())
                    .map(|d| d.as_secs());
                installed_at = match (installed_at, written) {
                    (Some(a), Some(b)) => Some(a.min(b)),
                    (a, b) => a.or(b),
                };
            }
            Ok(_) => stale = true,
            Err(_) => {}
        }
    }

    GsiStatus {
        cfg_dirs: dirs.iter().map(|d| d.display().to_string()).collect(),
        installed,
        stale,
        launch_option: launch_option_set(),
        port: GSI_PORT,
        installed_at,
    }
}

/// Writes the config into every Dota install found. Returns the paths
/// written, so the UI can say exactly what was touched rather than claiming
/// something vague happened.
pub fn install() -> Result<Vec<String>, String> {
    let dirs = dota_cfg_dirs();
    if dirs.is_empty() {
        return Err("Couldn't find Dota 2. Is it installed through Steam?".to_string());
    }

    let text = config_text();
    let mut written = Vec::new();
    let mut failures = Vec::new();

    for dir in dirs {
        let folder = dir.join("gamestate_integration");
        if let Err(e) = std::fs::create_dir_all(&folder) {
            failures.push(format!("{}: {e}", folder.display()));
            continue;
        }
        let path = folder.join(CFG_NAME);
        match std::fs::write(&path, &text) {
            Ok(()) => written.push(path.display().to_string()),
            Err(e) => failures.push(format!("{}: {e}", path.display())),
        }
    }

    if written.is_empty() {
        return Err(format!("Couldn't write the config. {}", failures.join("; ")));
    }
    Ok(written)
}

/// Removes it again, so enabling this is not a one-way door.
pub fn remove() -> Result<Vec<String>, String> {
    let mut removed = Vec::new();
    for dir in dota_cfg_dirs() {
        let path = dir.join("gamestate_integration").join(CFG_NAME);
        if path.exists() && std::fs::remove_file(&path).is_ok() {
            removed.push(path.display().to_string());
        }
    }
    Ok(removed)
}

/// Called at startup. Installs only when something is actually missing or
/// stale, so a normal launch does no file writing at all.
pub fn ensure_installed() -> Option<Vec<String>> {
    let s = status();
    if s.cfg_dirs.is_empty() || (s.installed && !s.stale) {
        return None;
    }
    install().ok()
}

/// The original Electron version of this tracker had players install its
/// config under this name — its README said to. It points at the same local
/// port, so every Dota update has been arriving twice, and if this app ever
/// moves port it would go on posting into whatever else listens there.
const LEGACY_CFG_NAME: &str = "gamestate_integration_lasthits.cfg";
const LEGACY_PORT: u16 = 3000;

/// Recognised by where it sends data, not by name alone: a file of the same
/// name aimed anywhere other than the old tracker's local listener belongs
/// to something else and is left where it is.
fn is_our_legacy_config(text: &str) -> bool {
    let uri = text.lines().find_map(|line| {
        let line = line.trim();
        if line.starts_with("\"uri\"") {
            line.split('"').nth(3).map(str::to_string)
        } else {
            None
        }
    });
    let Some(uri) = uri else { return false };
    let uri = uri.trim_end_matches('/');
    uri == format!("http://localhost:{LEGACY_PORT}") || uri == format!("http://127.0.0.1:{LEGACY_PORT}")
}

/// Removes the old tracker's config from every Dota install, if present.
/// Returns what was removed so the caller can say so.
pub fn remove_legacy_configs() -> Vec<String> {
    let mut removed = Vec::new();
    for dir in dota_cfg_dirs() {
        let path = dir.join("gamestate_integration").join(LEGACY_CFG_NAME);
        let Ok(text) = std::fs::read_to_string(&path) else { continue };
        if is_our_legacy_config(&text) && std::fs::remove_file(&path).is_ok() {
            removed.push(path.display().to_string());
        }
    }
    removed
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn only_the_old_trackers_own_config_is_recognised() {
        // Byte-for-byte what sits in the Dota folder on the machine this was
        // built on, installed by the Electron version's README.
        let ours = r#""Dota 2 Integration Configuration"
{
    "uri"           "http://localhost:3000/"
    "timeout"       "5.0"
    "data"
    {
        "player"        "1"
    }
}"#;
        assert!(is_our_legacy_config(ours));

        // Same name, different destination: someone else's, left alone.
        assert!(!is_our_legacy_config(&ours.replace("localhost:3000", "127.0.0.1:3002")));
        assert!(!is_our_legacy_config(&ours.replace("localhost:3000", "localhost:30001")));
        assert!(!is_our_legacy_config("not a config at all"));
    }

    #[test]
    fn the_config_names_the_port_we_actually_listen_on() {
        // These drifting apart is silent: Dota posts into the void and the
        // Live tab just never fills in.
        assert!(config_text().contains(&format!("http://localhost:{GSI_PORT}/")));
    }

    #[test]
    fn the_config_asks_for_what_the_tracker_reads() {
        // state.rs reads map, player, hero and items. Requesting less would
        // break tracking; the abilities block is off because nothing uses it
        // and it makes every payload bigger.
        let text = config_text();
        for key in ["\"map\"", "\"player\"", "\"hero\"", "\"items\""] {
            assert!(text.contains(key), "{key} must be requested");
        }
        // Matched without depending on the padding between the two tokens.
        let abilities_off = text
            .lines()
            .any(|l| l.contains("\"abilities\"") && l.trim_end().ends_with("\"0\""));
        assert!(abilities_off, "abilities stays off — nothing reads it and it inflates every payload");
    }
}

/// Dota's Steam app id.
const DOTA_APPID: &str = "570";

/// Starts Dota through Steam with the GSI flag attached.
///
/// This is the way around the one thing that cannot be automated. Steam
/// accepts `-applaunch <appid> <args>` and forwards the arguments to the
/// game for that session, which gets `-gamestateintegration` in front of
/// Dota without touching `localconfig.vdf` at all — no fighting Steam over a
/// file it rewrites from memory, and nothing persisted that the player did
/// not ask for.
///
/// The trade is that it only applies to launches started from here. Someone
/// who presses Play in Steam still needs the flag set the ordinary way,
/// which is why the instructions stay on screen either way.
pub fn launch_dota() -> Result<(), String> {
    let Some(steam) = crate::steam::steam_path() else {
        return Err("Couldn't find Steam on this PC.".to_string());
    };

    let exe = steam.join(if cfg!(windows) { "steam.exe" } else { "steam" });
    if !exe.exists() {
        return Err(format!("Steam isn't where it said it was ({}).", exe.display()));
    }

    std::process::Command::new(&exe)
        .arg("-applaunch")
        .arg(DOTA_APPID)
        .arg("-gamestateintegration")
        .spawn()
        .map(|_| ())
        .map_err(|e| format!("Couldn't start Dota through Steam: {e}"))
}
