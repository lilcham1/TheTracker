# TheTracker

A desktop match tracker for **Dota 2** and **Deadlock**.

Two different kinds of data feed it, and the app is explicit about which is
which rather than blurring them:

- **Live Dota tracking** comes from Valve's official **Game State
  Integration (GSI)** feed — the same system pro broadcast overlays use.
  Nothing reads game memory or touches the game process.
- **Dota match history** comes from the public **OpenDota** API. GSI only
  ever reports your own state and never says who won, so results, game
  modes and full scoreboards come from there.
- **Deadlock** has no Valve feed at all, so it uses the community-run
  **Deadlock API**. See the Deadlock section below for what that means.

Built with **Tauri**: a Rust backend does the GSI listening, match tracking
and API work, and the interface is HTML/CSS/JS rendered in the OS's own
WebView. One binary — no Electron, no browser tab, no bundled Node runtime.

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

That produces `src-tauri/target/release/thetracker.exe` — the frontend is
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
3. Copy `gamestate_integration_thetracker.cfg` (in this folder) into that
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

## Dota match history

The Live tab and the Match History tab are fed by different sources, on
purpose.

GSI is a live feed of *your own* state — it is excellent for tracking a
game as it happens, but Valve deliberately exposes nothing else. It never
reports the result, the other nine players, or the lobby type. That is why
matches recorded purely from GSI show up under **Tracked Sessions** with no
win/loss and an unspecified game type.

**Match History** fills that in from [OpenDota](https://www.opendota.com),
the same public dataset Dotabuff and friends use. Link your Steam account
once (Match History → search your name) and you get:

- real **win/loss**, including abandons
- authoritative **game mode and lobby type**, so Ranked / All Pick / Turbo
  stop being a manual guess
- **KDA, GPM, XPM, last hits, hero damage**, party size and duration
- the full **ten-player scoreboard** with items, expanded per match

Your row is highlighted in the scoreboard. Game-type classification is
verified against OpenDota's own constants: lobby type 7 is Ranked, game
mode 23 is Turbo, 22 and 1 are All Pick. Turbo is checked first, since a
ranked turbo game is still turbo and comparing its last-hit counts against
normal ranked games would be meaningless.

If nothing shows up, your Dota profile is probably private: in Dota 2 go to
Settings → Options → Advanced Options → **Expose Public Match Data**.

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

### Accounts

Publishing to the leaderboard requires an account (email + password, via
Convex Auth). **Reading is open to everyone** — you can track matches and
browse the global board signed out; an account is only needed to publish
your own results.

This is what stops leaderboard spam: every mutation reads the caller's
identity from their auth token server-side and ignores any user id sent by
the client, so knowing the deployment URL buys an attacker nothing. An
unauthenticated `matches:upsert` is rejected outright.

Password was chosen over OAuth deliberately: an OAuth provider in a desktop
app needs an external browser round trip and a deep link back into the
Tauri window, whereas this flow stays inside the app. The Rust backend
drives it by calling the `auth:signIn` action directly — there is no
JavaScript auth client bundled into the frontend.

Two tokens are involved: a short-lived JWT that authenticates calls, and a
refresh token that mints new ones. Only the refresh token is persisted (in
`auth.json` beside the history), so a session survives a restart; if a JWT
lapses mid-sync the worker refreshes once and replays the job rather than
dropping a match.

Matches synced by an install before it had an account are claimed by the
first account that signs in there (`matches:claimDevice`), so upgrading
doesn't orphan anything. `matches:removeMine` deletes everything your
account has published.

### The Convex side

```
convex/
  schema.ts       — auth tables + matches/profiles and their indexes
  auth.ts         — Convex Auth setup (password provider)
  http.ts         — auth HTTP routes
  matches.ts      — upsert / listMine / claimDevice / removeMine
  profiles.ts     — upsert / mine / whoami
  leaderboard.ts  — globalTop, the cross-player ranking (readable signed out)
```

Deploy changes to those with `npx convex deploy` (production) or
`npx convex dev` (dev deployment, watches for changes). The app points at
the production deployment baked into `convex_sync.rs`; override it at
runtime with `DOTA_TRACKER_CONVEX_URL` to aim at the dev one instead.

## Where your data lives

`history.json` and `profile.json` in `TheTracker/logs` inside your OS's
standard app-data folder (`%APPDATA%` on Windows,
`~/Library/Application Support` on Mac, `~/.local/share` on Linux).
Override the location entirely by setting `DOTA_TRACKER_LOG_DIR` before
launching.

The on-disk format is unchanged from the older Electron/egui versions, so an
existing `history.json` can be copied straight in.

## Deadlock

The Deadlock side works **fundamentally differently** from the Dota side, and
it's worth understanding why before relying on it.

Dota 2 ships Valve's official Game State Integration: the game itself pushes
live state to this app several times a second. **Deadlock has no equivalent.**
Valve publishes no GSI for it, so nothing can read a live Deadlock match
locally — not this app, not any other.

What exists instead is [deadlock-api.com](https://deadlock-api.com), an
independent, community-run service that aggregates match data from Valve's
own client APIs. So the Deadlock tab is:

- **post-match, not live.** Matches appear once the API has ingested them,
  shortly after a game ends. There are no in-match numbers.
- **keyed to a Steam account.** You link yours once, under Deadlock →
  Account. Nothing is read from your machine.
- **dependent on a third party.** Valve has been tightening rate limits on
  the underlying APIs, so matches can be delayed or missing. The app says so
  plainly rather than showing a misleading empty state.

Deliberately not done: nothing reads Deadlock's memory, injects into the
game, or surfaces information a player couldn't already see. The only live
signal is "are you currently in a match", from the public active-match
list, and the overlay shows the hero lineup the game already shows you —
never opponent identities, ranks or stats.

## Overlay

Both games get an optional in-game overlay: a separate transparent,
always-on-top, click-through window that floats over the game. It is an
ordinary desktop window — nothing is injected into either game.

Toggle it from the header. Click-through is on by default so it never eats
mouse input; turn it off temporarily to drag the overlay somewhere else,
then turn it back on.

For Dota it mirrors the live GSI data (clock, last hits, deaths and gold
lost, Roshan timer, checkpoints). For Deadlock — which has no live feed — it
shows the current match and lineup only.

## Why not CS2 or Valorant

CS2 has an official Valve GSI feed (same mechanism as Dota), so it's the
natural next game to add. Valorant has no live-data feed at all under Riot's
official policy, and post-match stats need an approved Riot developer app.
