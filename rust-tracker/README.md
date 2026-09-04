# Dota Tracker (Rust)

A native desktop rewrite of the Dota 2 match tracker, in Rust. Built entirely
on Valve's official **Game State Integration (GSI)** feed — the same system
pro broadcast overlays use. Nothing here reads game memory, calls a
third-party API, or touches anything outside GSI's official local HTTP feed.

Compared to the old Electron/Node version, this is one native binary — no
Node, no Electron, no browser tab. The window you open *is* the app.

## ⚠️ First build happens on your machine, not here

This was written and reviewed carefully, but it was **not compiled** in the
sandbox that generated it — that environment's network access doesn't reach
crates.io (Rust's package registry), so `cargo build` couldn't run there.
That means the first build needs to happen in a normal terminal on your own
PC, where you have your usual internet access. If it doesn't compile
cleanly, paste me the error and I'll fix it immediately — but there's a real
chance of a small mismatch on the first try, since I couldn't verify it
end-to-end myself.

## Build & run

Requires the Rust toolchain (`rustup.rs` if you don't have it — `cargo` and
`rustc` come with it).

```
cargo run --release
```

The first build will take a couple of minutes (it's fetching and compiling
`egui`/`eframe` and friends). After that, `cargo build --release` produces
`target/release/dota-tracker.exe` — copy that anywhere and double-click it
like any other app; it doesn't need `cargo` installed to run, only to build.

## Setting up Dota 2 to send it data

1. Find your Dota 2 `cfg` folder:
   - Windows: `...\Steam\steamapps\common\dota 2 beta\game\dota\cfg`
   - Mac: `~/Library/Application Support/Steam/steamapps/common/dota 2 beta/game/dota/cfg`
   - Linux: `~/.steam/steam/steamapps/common/dota 2 beta/game/dota/cfg`
2. Inside `cfg`, create a folder named `gamestate_integration` (if it
   doesn't already exist).
3. Copy `gamestate_integration_dota_tracker.cfg` (in this folder) into that
   `gamestate_integration` folder.
4. In Steam: right-click **Dota 2 → Properties → Launch Options**, add:
   ```
   -gamestateintegration
   ```
5. Launch the tracker (`cargo run --release`, or the built `.exe`), then
   start a Dota 2 match. The Live tab updates automatically once GSI data
   starts arriving.

## What it tracks

Same feature set as the original tracker:

- **Key items** (boots tier, Blink, BKB, Aghs, refresher, etc.) — tracked by
  total owned count, so moving items between slots never re-logs them.
- **Deaths** — time and gold lost per death.
- **Last hits** — snapshotted at 5/10/15/20/25 minutes.
- **Roshan** — auto-detected when GSI reports it reliably, plus a manual
  "Mark Roshan Death" button (Valve's own Roshan state data is known to be
  inconsistent, so the manual button is the reliable fallback).
- **End-of-match summary** comparing this game's deaths/gold-lost/last-hit
  checkpoints against your historical average for the same game type
  (Ranked/Unranked/Turbo/Other are never compared against each other), with
  Better/Worse/Average badges and 🏆 personal-best flags.
- **History tab** — every finished match, expandable for full detail.
- **Leaderboard tab** — your own top 10 games by a stat you pick (most last
  hits @25m, fewest deaths, least gold lost, most kills), filterable by game
  type. Personal only, not a real multiplayer leaderboard.
- **Profile tab** — a local username/rank/main-role label, stored only on
  your PC.

## Bringing over your old match history

If you used the old Electron/Node tracker, its `logs/history.json` uses the
same shape this app reads. To carry your history forward:

1. Find the old app's log directory (Electron stores it in its per-user app
   data folder — check the old app's `main.js` / README for the exact
   path on your OS).
2. Find this app's log directory: it defaults to `DotaTracker/logs` inside
   your OS's standard app-data folder (`%APPDATA%` on Windows, `~/Library/Application Support` on
   Mac, `~/.local/share` on Linux). You can override it entirely by setting
   the `DOTA_TRACKER_LOG_DIR` environment variable before launching, e.g. to
   point straight at the old app's folder.
3. Copy `history.json` (and `profile.json`, if you want your old profile
   too) into the new location.

## Why only Dota 2, for now

CS2 has an official Valve GSI feed too (same mechanism), so it's the
natural next game to add to this same app. Valorant has no live-data feed
at all under Riot's official policy, and getting even post-match stats
requires an approved Riot developer app plus players logging in through
Riot's own sign-on — that's a separate, heavier piece of work. Deadlock has
no official Valve API yet. All were deliberately left out of this build so
what's here stays 100% built on official, allowed data access.

## Project layout

```
src/
  main.rs     — entry point: spawns the GSI listener, launches the window
  gsi.rs      — the GSI HTTP listener (the one and only HTTP route: POST /)
  state.rs    — match tracking logic, ported 1:1 from the original tracker
  model.rs    — data types (kept JSON-compatible with the old history.json)
  heroes.rs   — hero/item display names, CDN icon URLs, key-item whitelist
  storage.rs  — history.json / profile.json persistence
  app.rs      — the egui UI: Live / History / Leaderboard / Profile tabs
```
