# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working in this repository.

## What this is

**TheTracker** — a desktop match tracker for Dota 2 and Deadlock, in
`rust-tracker/`. Built with **Tauri**: a Rust backend (`src-tauri/`) holding
the GSI listener, match-tracking logic and all API clients, plus an
HTML/CSS/JS frontend (`ui/`) rendered in the OS WebView. One compiled
binary — no Electron, no bundled Node runtime. See `rust-tracker/README.md`
for build and run instructions.

The folder is still named `rust-tracker/` for historical reasons; it is the
only implementation. An earlier Electron/Node version was removed once the
Tauri app superseded it — recover it from git history (commit `c8c1192`) if
it is ever needed.

## Where the data comes from

Three distinct sources, deliberately not blurred together:

- **Live Dota** — Valve's official Game State Integration feed. A
  `gamestate_integration_thetracker.cfg` dropped into Dota 2's `cfg` folder
  plus the `-gamestateintegration` launch option. Local HTTP only; nothing
  reads game memory. GSI reports *only the local player's own state* and
  never says who won.
- **Dota match history** — the public OpenDota API
  (`src-tauri/src/dota_api.rs`), keyed on a linked Steam32 account. This is
  where win/loss, game modes, GPM/XPM and scoreboards come from, precisely
  because GSI cannot supply them.
- **Deadlock** — the community-run Deadlock API
  (`src-tauri/src/deadlock.rs`). Valve ships no GSI for Deadlock, so this
  side is post-match only. Do not add anything here that surfaces
  information a player could not already see in-game.

`convex/` holds TypeScript functions for cloud sync and the shared
leaderboard; they run on Convex's servers, not in the app. Writes require an
authenticated account — the server takes the user id from the auth token,
never from client arguments.

## Windows build environment (important)

This machine builds with the **GNU** Rust toolchain, not MSVC — there are no
Visual Studio Build Tools installed. Two consequences worth knowing before
touching the build:

- A `rustup override` pins `rust-tracker/` to
  `stable-x86_64-pc-windows-gnu`, and MinGW-w64
  (`%USERPROFILE%\mingw64-winlibs\mingw64\bin`) must be on `PATH` when
  building — it supplies the `as`/`dlltool` binaries that rustup's own
  bundled linker lacks.
- **The repo path must not contain spaces.** MinGW's `cc1.exe` splits paths
  at the space, which breaks Tauri's build script. The project was moved
  from `F:\dota tracker` to `F:\dota-tracker` for exactly this reason —
  don't move it back under a path with a space in it.

Release builds must keep
`#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]` at the
top of `main.rs`, or Windows attaches a stray console window to the app.
