# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working in this repository.

## What this is

A Dota 2 live match tracker built on Valve's official Game State Integration
(GSI) feed. This folder holds two independent implementations of the same
tracker — pick one, they don't depend on each other:

- **`rust-tracker/`** — the current version. A native desktop app built with
  **Tauri**: a Rust backend (`src-tauri/`) holding all the GSI/tracking
  logic, and an HTML/CSS/JS frontend (`ui/`) for the interface, rendered in
  the OS WebView. One compiled binary, no browser tab, no Electron. See
  `rust-tracker/README.md` for build/run instructions.
- **`electron-tracker/`** — the original version. Electron + a
  dependency-free Node HTTP server serving an inlined HTML dashboard. See
  `electron-tracker/README.md` and `electron-tracker/CLAUDE.md` for details.
  Its `node_modules/` and `dist/` build output were removed to save space
  (they're fully regenerable — see that folder's CLAUDE.md).

Both talk to Dota 2 the same official way: a `gamestate_integration_*.cfg`
file dropped into Dota 2's `cfg` folder, plus the `-gamestateintegration`
launch option. Each subfolder ships its own copy of that `.cfg` file.

## Working in this repo

Treat `rust-tracker/` and `electron-tracker/` as separate projects — run
their commands from inside their own folder, not from here. Each has its
own README with the exact setup steps.

## Windows build environment (important)

This machine builds `rust-tracker/` with the **GNU** Rust toolchain, not
MSVC — there are no Visual Studio Build Tools installed. Two consequences
worth knowing before touching the build:

- A `rustup override` pins `rust-tracker/` to
  `stable-x86_64-pc-windows-gnu`, and MinGW-w64 (`%USERPROFILE%\mingw64-winlibs\mingw64\bin`)
  must be on `PATH` when building — it supplies the `as`/`dlltool` binaries
  that rustup's own bundled linker lacks.
- **The repo path must not contain spaces.** MinGW's `cc1.exe` splits paths
  at the space, which breaks Tauri's build script. The project was moved
  from `F:\dota tracker` to `F:\dota-tracker` for exactly this reason —
  don't move it back under a path with a space in it.
