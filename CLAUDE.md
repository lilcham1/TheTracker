# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working in this repository.

## What this is

A Dota 2 live match tracker built on Valve's official Game State Integration
(GSI) feed. This folder holds two independent implementations of the same
tracker — pick one, they don't depend on each other:

- **`rust-tracker/`** — the current version. A native desktop app (Rust +
  egui/eframe), one compiled binary, no Node/Electron/browser tab required.
  See `rust-tracker/README.md` for build/run instructions and
  `rust-tracker/CLAUDE.md`-equivalent notes are in that folder's own README.
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
