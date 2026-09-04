> **Note:** `node_modules/` and `dist/` were removed during a cleanup to save space (they were ~570MB of regenerable build output). Run `npm install` before `npm start`, and `npm run dist` will recreate `dist/` when you need the packaged installer again.

# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A standalone Dota 2 live match tracker built on Valve's Game State Integration (GSI) feed. It's an Electron desktop app that wraps a dependency-free Node HTTP server; the same server also runs standalone in plain Node for browser-tab use. There is no frontend build step — the dashboard is a single HTML/CSS/JS string served directly by the server.

## Commands

```bash
npm install      # one-time; installs Electron + electron-builder (needs internet)
npm start         # runs the Electron app (electron .)
npm run dist       # builds a Windows installer into dist/ (electron-builder)
npm run release    # builds AND publishes the installer to GitHub Releases (needs GH_TOKEN env var set, and build.publish.owner/repo in package.json filled in)
node server.js     # runs the tracker as plain Node, no Electron — serves the dashboard at http://localhost:3000
```

There is no test suite and no lint config in this repo — don't invent npm scripts for `test`/`lint` that don't exist in [package.json](package.json).

To actually exercise the tracker locally, Dota 2 needs to be sending GSI data to `http://localhost:3000` (a `gamestate_integration_*.cfg` file dropped into the Dota 2 `cfg` folder, plus the `-gamestateintegration` launch option — see [README.md](README.md) for the exact steps). Without that, you can still hit the server directly with a synthetic POST to `/` (see "GSI payload shape" below) to drive state for manual testing.

## Architecture

Everything lives in two files: [server.js](server.js) (all tracking logic + the dashboard UI) and [main.js](main.js) (the Electron shell around it).

**Request flow:** Dota 2's client POSTs JSON GSI updates to `/` roughly every ~0.1–1s while a match is running. [`handleUpdate()`](server.js) is the single entry point that mutates the one in-memory `current` match object from each payload. The dashboard (served as one big inlined HTML string, `DASHBOARD_HTML`, from `GET /`) is a plain polling client — it hits `GET /stats` every 2s and re-renders from scratch client-side; there's no websocket/push and no client-side framework.

**State model:** Only one match is tracked at a time (`current`, a module-level variable — not per-session). `newMatch()` initializes it when a new `matchid` appears; `handleUpdate()` updates it in place per GSI tick; `finalizeMatch()` runs once when GSI reports `DOTA_GAMERULES_STATE_POST_GAME`, which snapshots a `summary` (via `buildSummary()`), computes historical comparisons, and appends it to `logs/history.json`. After that point `current.ended` is true and further GSI ticks for that matchid are ignored until a new `matchid` shows up.

**Persistence:** Two flat JSON files under `LOG_DIR` — `history.json` (every finished match, append-only) and `profile.json` (local username/rank/role). `LOG_DIR` defaults to `./logs` next to `server.js`, but main.js overrides it via `DOTA_TRACKER_LOG_DIR` to point at Electron's per-user data dir, since the packaged app runs from a read-only `app.asar` and can't write next to itself. Any change to where/how state is persisted needs to account for both run modes.

**Comparisons ("Better/Worse/Average" badges, personal bests):** `finalizeMatch()` filters `history.json` down to peer matches with the same `gameType` (ranked/unranked/turbo/other are never compared against each other) and computes averages via `compareMetric()`/`bestValue()`. `gameType` itself is not derivable from GSI — it's set manually per-match through `POST /set-game-type` and defaults to `'unspecified'`.

**Key items / Roshan / checkpoints** are all tracked by diffing GSI snapshots rather than reading discrete events:
- Key items: `KEY_ITEMS` is a whitelist of item internal names; ownership is tracked by *total count across inventory+backpack+neutral slots* each tick, so moving an item between slots doesn't re-trigger a log entry — only a count increase does (`current.ownedItemCounts`).
- Deaths: detected as an `alive: true → false` transition on `hero.alive`, with gold-lost inferred from the gold delta since the previous tick (not from a GSI death event, since GSI doesn't expose one).
- Roshan: `map.roshan_state` drives auto-detection, but it's explicitly called out (in code comments and the README) as unreliable, hence the manual `POST /roshan-death` fallback. Respawn window math (8–11 min) lives in the dashboard's `renderRoshan()`.
- Last hits: snapshotted at fixed `CHECKPOINT_MINUTES` ([5, 10, 15, 20, 25]) the first tick each threshold is crossed.

**Routing:** `http.createServer`'s callback is a hand-rolled if/else chain matching `req.method + req.url` (see bottom of [server.js](server.js)) — there's no router library and no path params, so a new endpoint means adding another branch there, plus a matching `fetch()` call in the dashboard's inline `<script>`.

**Dashboard UI:** Entirely inside the `DASHBOARD_HTML` template literal in server.js — CSS, HTML, and client JS all in one string, no separate static assets except hero/item icons which load live from Valve's public CDN (`HERO_CDN`/`ITEM_CDN` constants). Editing the UI means editing that string in place; there's no template engine or JSX.

**Electron wrapper ([main.js](main.js)):** Just requires `server.js` (which starts listening as a side effect of being required) and points a `BrowserWindow` at `http://localhost:3000`. Auto-update (via `electron-updater`, GitHub Releases) only activates in packaged builds (`app.isPackaged`), never during `npm start`.
