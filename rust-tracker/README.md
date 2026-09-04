# Dota Tracker

A native desktop Dota 2 match tracker, built on Valve's official **Game
State Integration (GSI)** feed — the same system pro broadcast overlays
use. Nothing here reads game memory, calls a third-party API, or touches
anything outside GSI's official local HTTP feed.

Built with **Tauri**: the Rust backend does all the GSI listening and match
tracking, and the interface is HTML/CSS/JS rendered in the OS's own WebView.
One binary, no Electron, no browser tab, no Node runtime shipped.

## Project layout

```
ui/                    — the interface (plain HTML/CSS/JS, no bundler)
  index.html
  style.css
  app.js               — rendering + all `invoke` calls to the backend
src-tauri/
  src/
    main.rs            — Tauri entry point + the commands the UI calls
    gsi.rs             — the GSI HTTP listener (one route: POST /)
    state.rs           — match tracking + historical comparison logic
    model.rs           — data types (JSON-compatible with history.json)
    heroes.rs          — key-item whitelist, Roshan drop table
    storage.rs         — history.json / profile.json persistence
    convex_sync.rs     — background cloud sync + global leaderboard queries
    device_id.rs       — stable per-install id used by the leaderboard
  tauri.conf.json      — window, bundle and CSP config
  icons/               — generated app icons
```

The frontend never touches the filesystem or the network directly — it
calls Rust commands (`get_live_state`, `get_history`,
`set_history_game_type`, …) and renders what comes back.

## Build & run

Requires the Rust toolchain (`rustup.rs`) and Node (only for the Tauri
CLI — nothing from npm ships inside the app).

```
npm install
npm run tauri dev
```

For a release binary:

```
cd src-tauri
cargo build --release
```

That produces `src-tauri/target/release/dota-tracker.exe` — the frontend is
compiled into the binary, so the `.exe` is self-contained and can be copied
anywhere.

### Windows note: this repo builds with the GNU toolchain

There are no Visual Studio Build Tools on the machine this was developed
on, so `rust-tracker/` is pinned by a `rustup override` to
`stable-x86_64-pc-windows-gnu`, and a MinGW-w64 `bin` directory must be on
`PATH` at build time (it provides `as.exe`/`dlltool.exe`, which rustup's
bundled self-contained linker does not include).

**The project path must not contain spaces** — MinGW's `cc1.exe` splits
paths at the space, which breaks Tauri's build script with a confusing
"No such file or directory" error. (If you'd rather not deal with any of
this, installing the MSVC "C++ build tools" and switching the override to
`stable-x86_64-pc-windows-msvc` avoids both constraints.)

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
5. Launch the tracker, then start a Dota 2 match. The Live tab updates
   automatically once GSI data starts arriving.

## What it tracks

- **Key items** (boots tier, Blink, BKB, Aghs, refresher, etc.) — tracked by
  total owned count, so moving items between slots never re-logs them.
- **Deaths** — time and gold lost per death.
- **Last hits** — snapshotted at 5/10/15/20/25 minutes.
- **Roshan** — auto-detected when GSI reports it reliably, plus a manual
  "Mark Death" button (Valve's own Roshan state data is known to be
  inconsistent, so the manual button is the reliable fallback). Shows the
  respawn window and the drop table for that Roshan number.
- **End-of-match summary** comparing this game's deaths/gold-lost/last-hit
  checkpoints against your historical average for the same game type, with
  Better/Worse/Average badges and 🏆 personal-best flags.
- **History tab** — every finished match, expandable for full detail. Each
  match's game type can be **re-tagged after the fact** (Ranked / All Pick /
  Turbo / Other) straight from its badge — GSI doesn't reliably report lobby
  type, so this is how you correct it. Re-tagging recomputes every match's
  comparison so the peer-group stats stay consistent.
- **Leaderboard tab** — your own top 10 games by a stat you pick (most last
  hits @25m, fewest deaths, least gold lost, most kills), filterable by game
  type. Personal only, not a real multiplayer leaderboard.
- **Profile tab** — a local username/rank/main-role label, stored only on
  your PC.

## Game types

Matches are tagged as **Ranked**, **All Pick**, **Turbo**, or **Other**, and
comparisons only ever run within the same tag (a Turbo game is never
compared against a Ranked one). Set it live during a match on the Live tab,
or correct it later from the History tab.

## Cloud sync (Convex)

Finished matches and your profile sync to a [Convex](https://convex.dev)
deployment, which is also what powers the **Global** leaderboard — a
cross-player ranking, not just your own games.

**Local files stay the source of truth.** Every match is written to
`history.json` first and only then queued for upload, on a background task.
If the network is down, Convex is unreachable, or you're mid-game when it
hiccups, nothing is lost — the sync pill in the header turns red, and
**Sync Everything** on the Profile tab pushes the backlog whenever you're
back online. Uploads upsert on `(deviceId, matchid)`, so re-syncing the
whole history never creates duplicates.

### Identity

There's no sign-in. A random `deviceId` is generated on first run and
stored in `device_id.txt` next to your history; the leaderboard shows the
username from the Profile tab as the display name. That means:

- Anyone who pulls the deployment URL out of the binary could write junk
  rows — fine for a tool shared with friends, not hardened for public use.
  Moving to real accounts would mean adding Convex Auth.
- `matches:removeForDevice` deletes everything one install has synced, if
  you want your data off the shared leaderboard.

### The Convex side

```
convex/
  schema.ts       — matches + profiles tables and their indexes
  matches.ts      — upsert / listForDevice / countForDevice / removeForDevice
  profiles.ts     — upsert / forDevice
  leaderboard.ts  — globalTop, the cross-player ranking
```

Deploy changes to those with `npx convex deploy` (production) or
`npx convex dev` (dev deployment, watches for changes). The app points at
the production deployment baked into `convex_sync.rs`; override it at
runtime with `DOTA_TRACKER_CONVEX_URL` to aim at the dev one instead.

## Where your data lives

`history.json` and `profile.json` in `DotaTracker/logs` inside your OS's
standard app-data folder (`%APPDATA%` on Windows,
`~/Library/Application Support` on Mac, `~/.local/share` on Linux).
Override the location entirely by setting `DOTA_TRACKER_LOG_DIR` before
launching.

The on-disk format is unchanged from the older Electron/egui versions, so an
existing `history.json` can be copied straight in.

## Why only Dota 2, for now

CS2 has an official Valve GSI feed too (same mechanism), so it's the
natural next game to add. Valorant has no live-data feed at all under Riot's
official policy. Deadlock has no official Valve API yet. All were
deliberately left out so what's here stays 100% built on official, allowed
data access.
