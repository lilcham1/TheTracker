# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working in this repository.

## What this is

**TheTracker** — a desktop match tracker for Dota 2, Deadlock, CS2 and
Overwatch, in
`TheTracker/`. Written in **Go**: the backend is plain Go, the window shell is
Wails v3 (WebView2, no CGO), and the interface is plain HTML/CSS/JS embedded
in the exe. See `TheTracker/README.md` for the feature list, layout, build
and test commands.

Versions before 1.0 were Rust/Tauri (and before that Electron). That code is
gone from the tree; recover it from git history (tag `v0.17.0`) if needed.
Data files, the install location, the updater feed and the signing key are
unchanged from those versions, so old installs update into the Go app.

## Architecture rules worth keeping

- `internal/core` has no window code. Anything that needs the desktop goes
  through the `Shell` interface, implemented in `main.go` and faked by
  `NoShell`. Keep it that way: it is why `go test ./...` covers nearly
  everything.
- The UI reaches the backend only via `POST /api/<command>`
  (`internal/api/api.go`). New feature = one command there + a test in
  `api_test.go`.
- All files are written through `Store` (atomic temp-file + rename; history
  and prefs updates go through `UpdateHistory` / `UpdatePrefs` under a lock).
- JSON field names in `history.json` / `prefs.json` must not change: existing
  installs carry files written by every earlier version.

## Where the data comes from

Three sources, deliberately not blurred together:

- **Live Dota** — Valve's Game State Integration. The app writes
  `gamestate_integration_thetracker.cfg` into Dota's cfg folder and listens
  on `127.0.0.1` only. GSI reports only the local player's own state.
- **Dota history, heroes, meta, matchups** — the public OpenDota API
  (`internal/core/opendota.go`). Always pass `significant=0` on player
  endpoints: without it OpenDota silently hides every Turbo game.
- **Deadlock** — the community Deadlock API (`internal/core/deadlock.go`).
  Post-match only. Do not add anything that surfaces information a player
  could not already see in-game.

- **CS2** — Game State Integration again (`internal/core/cs2.go`), on the
  same listener, routed by `provider.appid`. There is no public CS2 match
  history or leaderboard; do not invent one.
  Rounds, damage, weapon kills, the buy and how a round ended are derived
  from state changes between posts. `cs2_sim_start` plays a scripted match
  that is never saved. There is deliberately no Steam Web API use: the user
  does not want an API key to set up.
- **Overwatch** — meta and per-hero career in `overwatch_extra.go`; progress
  (snapshots of the totals, diffed into sessions) in `overwatch_progress.go`; the community OverFast API (`internal/core/overwatch.go`):
  career totals only, public profiles only.
- **Valorant** — deliberately absent. It needs a Riot production API key
  granted to this project; without one there is no legitimate source.
- **Sign-in** — Steam OpenID only (`steamlogin.go`, verified server-side in
  `convex/auth.ts`). No email/password, no manual account linking.

Do not scrape other trackers' sites, work around their sign-ins, or copy
their ratings. `convex/` holds the cloud functions for optional sync and the
shared leaderboard; the server takes the user id from the auth token, never
from client arguments.

## Build environment (Windows)

- Go, NSIS and go-winres are installed; none of Go, git, gh or node is
  reliably on PATH inside the Bash tool — `build.sh` prepends what it needs,
  and other commands must do the same.
- Build from Git Bash with `./build.sh <version> [--sign]`. Plain
  `go build -tags production -ldflags "-H windowsgui"` also works for a quick
  check; without `-H windowsgui` Windows attaches a console window.
- When passing Windows paths to native tools from Git Bash, use absolute
  paths from `cygpath -w` and set `MSYS2_ARG_CONV_EXCL="*"`; Git Bash
  rewrites arguments that look like POSIX paths.

## Verifying without the user's screen

Do not drive the user's desktop. Use `--serve` (UI in the built-in browser
pane) and `THETRACKER_SELFTEST` (the real app checks itself hidden and
quits), both with `THETRACKER_LOG_DIR` pointed at a throwaway folder and
`THETRACKER_NO_DOTA_SETUP=1`. See the README's Testing section.
