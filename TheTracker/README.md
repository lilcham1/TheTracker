# TheTracker

A desktop match tracker for Dota 2, Deadlock, Counter-Strike 2 and Overwatch. One Windows executable,
written in Go, with its interface drawn in the system's WebView2.

## What it does

**Dota 2**

- **Live** – follows your match as you play, from Valve's own Game State
  Integration feed: last hits at 5/10/15/20/25 minutes against your own
  averages, each death and the gold it cost, timings of key items, and
  progress against goals you set.
- **Overlay** – a small transparent window over the game that counts down to
  rune spawns, lotuses and the stack pull, and shows nothing otherwise.
- **Sessions** – every match recorded live, with comparisons against your
  other games of the same type, your own notes, and plain-language insights
  ("most of your deaths come between 10 and 20 minutes").
- **Matches, Heroes, Overview** – your match history, lifetime hero records,
  medal, and the people you play with most, from OpenDota. Recorded sessions
  OpenDota lacks are merged into the same list.
- **Draft** – add the enemy's heroes as they pick and see which heroes have
  done well against that lineup, with your own record on each.
- **Meta** – which heroes are winning, rising and falling, by estimated role.
- **Builds** – your saved item plans, beside what players buy most.
- **Leaderboard** – your best recorded games, and (with an account) a shared
  board.

**Deadlock** – overview, matches with scoreboards, heroes, meta (heroes and
items) and builds, from the community Deadlock API. Deadlock has no live
feed, so there is no live page or overlay for it.

**Counter-Strike 2** – live tracking of your own matches from Valve's Game
State Integration (score, kills, deaths, headshots), saved as you play, with
per-map records. Valve publishes no CS2 match history or leaderboard for apps,
so only matches played while the app runs are shown.

**Overwatch** – career stats by mode, role and hero from your public profile,
via the community OverFast API. Blizzard publishes no match list.

**Valorant is not supported.** Riot only allows match data through a
production API key it grants per application after review, with each player
signing in through Riot. Until this project holds such a key there is no
legitimate way to read Valorant matches, and the app does not pretend to.

Each game can be switched off under Settings; a first-run screen asks which
ones you play.

## Signing in

One method: **Sign in with Steam**. Steam's own login page opens in the
browser (OpenID); the app receives a signed statement of which account
logged in, and the cloud function in `convex/auth.ts` verifies it with Steam
before opening a session. That one sign-in links Dota 2 and Deadlock, names
the leaderboard profile, and never involves a password or API key. If the
cloud service is unreachable the app verifies the statement itself and
everything except the shared leaderboard works. Overwatch is connected
separately by BattleTag, since it is not a Steam account.

After changing anything in `convex/`, deploy it with `npx convex deploy`.

## Where the data comes from

| Source | Used for | Notes |
| --- | --- | --- |
| Valve Game State Integration | Live Dota tracking | Dota posts your own state to `127.0.0.1` on this PC. Nothing reads game memory. |
| [OpenDota](https://www.opendota.com) API | Dota history, heroes, meta, matchups | Public, no key. Keyed on your Steam account id. |
| [Deadlock API](https://deadlock-api.com) | Everything Deadlock | Community-run, rate-limited by Valve; can be late or incomplete. |
| Valve Game State Integration (CS2) | Live CS2 tracking | Same local feed as Dota; needs no launch option. |
| Valve's Dota leaderboard web API | Dota top players by region | Names and ranks only. |
| [OverFast API](https://overfast-api.tekrop.fr) | Overwatch career stats | Community-run; reads Blizzard's public career pages. Profile must be public. |
| Convex (optional account) | Sync and the shared leaderboard | Only if you sign in. `convex/` holds the server functions. |

Nothing is scraped from any other tracker's site, and nothing is shown about
another player that the game does not already show you.

## Layout

```
main.go              desktop shell: windows, overlay, tray, start-with-Windows
internal/core/       everything the app does: tracker, storage, API clients,
                     sync, updater, insights. No window code; fully testable.
internal/api/        one HTTP handler; every UI action is POST /api/<command>
frontend/            the interface: plain HTML, CSS and JS, embedded in the exe
build/windows/       installer script and Windows resources
convex/              cloud functions (TypeScript, run on Convex's servers)
```

The UI talks to the backend only through `/api/*`, served inside the app's
own window (never on a network port). That is what lets every function be
tested without opening a window.

## Building

Needs Go 1.24+, [NSIS](https://nsis.sourceforge.io) and
[go-winres](https://github.com/tc-hib/go-winres). No C compiler.

```bash
./build.sh 1.0.0            # tests, then build/bin/thetracker.exe and the installer
./build.sh 1.0.0 --sign     # also signs the installer and writes latest.json
```

Signing uses the release key at `~/.tauri/thetracker.key` through the Tauri
CLI's signer (`npm install` provides it). The key and signature format are
the ones every release has used, so any installed version accepts the update.

To release: create a GitHub release tagged `v<version>` with the installer,
its `.sig`, and `latest.json` from `build/bin`. Installed copies check
`releases/latest/download/latest.json`.

## Testing

```bash
go test ./...
```

covers the tracker, the Dota config setup, every API client (against local
fake servers), sync, the updater's signature check, backup and restore, and
every `/api` command.

Two ways to run the app without touching real data or showing a window:

```bash
# The UI and API in a browser, backed by a throwaway data folder:
THETRACKER_LOG_DIR=/tmp/tt ./build/bin/thetracker.exe --serve 127.0.0.1:8765

# The real app checking itself (every page, the overlay, the tray), then quitting:
THETRACKER_LOG_DIR=/tmp/tt THETRACKER_NO_DOTA_SETUP=1 \
THETRACKER_SELFTEST=/tmp/selftest.txt ./build/bin/thetracker.exe --minimized
```

`THETRACKER_LOG_DIR` moves all app data; `THETRACKER_NO_DOTA_SETUP` stops the
app writing its config into Dota's folder.

## Data on disk

`%APPDATA%\TheTracker\logs` holds `history.json` (sessions), `prefs.json`,
`profile.json`, the linked accounts, and a `cache` folder. Uninstalling
leaves it in place. Settings → General can back it up to
`Documents\TheTracker` and restore it.
